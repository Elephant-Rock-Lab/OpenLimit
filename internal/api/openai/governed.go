package openaiapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"openlimit/internal/auth"
	"openlimit/internal/audit"
	"openlimit/internal/cache"
	"openlimit/internal/errtypes"
	"openlimit/internal/guardrails"
	"openlimit/internal/health"
	"openlimit/internal/mcp"
	"openlimit/internal/providers"
	"openlimit/internal/requestid"
	"openlimit/internal/routing"
	openaischema "openlimit/internal/schema/openai"
	usageapi "openlimit/internal/usage"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// GovernanceIdentity provides a unified identity for all three entry points
// (direct API via virtual key, MCP server tool call, A2A). It carries the
// per-key governance parameters that ExecuteGoverned enforces before every
// provider call.
//
// A nil *GovernanceIdentity means "no per-key identity" — used for A2A.
// Rate limiting and budget checks are skipped; guardrails are always enforced.
type GovernanceIdentity struct {
	ProjectID        string
	VirtualKeyID     string
	KeyPrefix        string
	Name             string
	AllowedModels    []string
	AllowedProviders []string
	RPMLimit         int
	TPMLimit         int
	BudgetLimitUSD   float64
	BudgetPeriod     string
	Source           string // "virtual_key", "mcp_tool", "a2a"
	SkipRateLimit    bool
	SkipBudget       bool
	SkipUsageLog     bool
}

// GovernanceError is returned by ExecuteGoverned when a governance control
// blocks a request. Callers type-assert errors to GovernanceError to extract
// structured status codes, headers, and stage information.
type GovernanceError struct {
	StatusCode int               // HTTP status code (400, 403, 429)
	Type       string            // Machine-readable error type
	Message    string            // Human-readable error message
	Stage      string            // Governance stage that triggered the block (e.g. "input", "output")
	Headers    map[string]string // Response headers (Retry-After, X-RateLimit-*)
}

// Error implements the error interface.
func (e *GovernanceError) Error() string {
	if e.Stage != "" {
		return fmt.Sprintf("governance error: %s: %s (stage=%s)", e.Type, e.Message, e.Stage)
	}
	return fmt.Sprintf("governance error: %s: %s", e.Type, e.Message)
}

// GovernanceBlocked returns true, satisfying the GovernanceBlockedError
// interface used by the A2A handler to detect governance rejections.
func (e *GovernanceError) GovernanceBlocked() bool { return true }

// GovernedResult carries the provider response together with operational
// metadata useful for observability and HTTP response headers.
type GovernedResult struct {
	Response   *openaischema.ChatCompletionResponse
	Target     providers.Target
	Attempts   int
	CacheHit   bool
	DurationMS int64
	CostUSD    float64
	Headers    map[string]string // X-Provider, X-Cache, X-Cost-USD, etc.
}

// ---------------------------------------------------------------------------
// Conversion functions
// ---------------------------------------------------------------------------

// governanceIdentityFromAuth maps an auth.Context to a GovernanceIdentity.
// Source is set to "virtual_key" and all skip flags are false — the direct
// API path always enforces every governance control.
func governanceIdentityFromAuth(ac *auth.Context) *GovernanceIdentity {
	if ac == nil {
		return nil
	}
	return &GovernanceIdentity{
		ProjectID:        ac.ProjectID,
		VirtualKeyID:     ac.VirtualKeyID,
		KeyPrefix:        ac.KeyPrefix,
		Name:             ac.Name,
		AllowedModels:    ac.AllowedModels,
		AllowedProviders: ac.AllowedProviders,
		RPMLimit:         ac.RPMLimit,
		TPMLimit:         ac.TPMLimit,
		BudgetLimitUSD:   ac.BudgetLimitUSD,
		BudgetPeriod:     ac.BudgetPeriod,
		Source:           "virtual_key",
		SkipRateLimit:    false,
		SkipBudget:       false,
		SkipUsageLog:     false,
	}
}

// governanceIdentityFromResolvedKey maps an mcp.ResolvedKey to a
// GovernanceIdentity. Source is set to "mcp_tool" and all skip flags are
// false — MCP tool calls enforce every governance control.
func governanceIdentityFromResolvedKey(key *mcp.ResolvedKey) *GovernanceIdentity {
	if key == nil {
		return nil
	}
	return &GovernanceIdentity{
		ProjectID:        key.ProjectID,
		VirtualKeyID:     key.KeyID,
		KeyPrefix:        key.KeyPrefix,
		Name:             key.KeyName,
		AllowedModels:    key.AllowedModels,
		AllowedProviders: key.AllowedProviders,
		RPMLimit:         key.RPMLimit,
		TPMLimit:         key.TPMLimit,
		BudgetLimitUSD:   key.BudgetLimitUSD,
		BudgetPeriod:     key.BudgetPeriod,
		Source:           "mcp_tool",
		SkipRateLimit:    false,
		SkipBudget:       false,
		SkipUsageLog:     false,
	}
}

// governanceIdentityFromProvider converts an IdentityProvider from the mcp
// package into a GovernanceIdentity. This avoids circular imports: the mcp
// package defines MCPIdentity with getter methods (IdentityProvider interface),
// and this package extracts the fields without importing MCPIdentity directly.
func governanceIdentityFromProvider(p mcp.IdentityProvider) *GovernanceIdentity {
	if p == nil {
		return nil
	}
	return &GovernanceIdentity{
		ProjectID:        p.GetProjectID(),
		VirtualKeyID:     p.GetVirtualKeyID(),
		KeyPrefix:        p.GetKeyPrefix(),
		Name:             p.GetName(),
		AllowedModels:    p.GetAllowedModels(),
		AllowedProviders: p.GetAllowedProviders(),
		RPMLimit:         p.GetRPMLimit(),
		TPMLimit:         p.GetTPMLimit(),
		BudgetLimitUSD:   p.GetBudgetLimitUSD(),
		BudgetPeriod:     p.GetBudgetPeriod(),
		Source:           p.GetSource(),
		SkipRateLimit:    p.GetSkipRateLimit(),
		SkipBudget:       p.GetSkipBudget(),
		SkipUsageLog:     p.GetSkipUsageLog(),
	}
}

// ---------------------------------------------------------------------------
// ExecuteGoverned — the unified governance pipeline
// ---------------------------------------------------------------------------

// ExecuteGoverned enforces all governance controls in order and, if none
// block the request, routes it to a provider. It returns a GovernedResult
// on success or a GovernanceError (which implements error) when a
// governance control rejects the request.
//
// Governance steps (in order):
//  1. Model validation
//  2. Rate limiting (with skip flag)
//  3. Budget check (with skip flag)
//  4. Input guardrails
//  5. Cache lookup
//  6. Routing + circuit breaker + retry
//  7. Provider call
//  8. Output guardrails
//  9. Cache store
//
// 10. Usage logging (with skip flag)
// 11. Metrics recording
func (h *Handler) ExecuteGoverned(ctx context.Context, req openaischema.ChatCompletionRequest, identity *GovernanceIdentity) (*GovernedResult, error) {
	start := time.Now()

	// ------ Step 1: Model validation ------
	if identity != nil && len(identity.AllowedModels) > 0 {
		allowed := false
		for _, m := range identity.AllowedModels {
			if strings.EqualFold(m, req.Model) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, &GovernanceError{
				StatusCode: 403,
				Type:       "model_not_allowed",
				Message:    fmt.Sprintf("model %q is not allowed for this key", req.Model),
				Stage:      "model_validation",
			}
		}
	}

	// Track rate limit headers for inclusion in GovernedResult (even on success)
	rateLimitHeaders := make(map[string]string)

	// ------ Step 2: Rate limiting ------
	if identity != nil && !identity.SkipRateLimit && identity.RPMLimit > 0 {
		limiter := h.getLimiter(identity.VirtualKeyID, identity.RPMLimit, identity.TPMLimit)
		allowed, limit, remaining, resetAt := limiter.CheckRPM(identity.VirtualKeyID)
		rateLimitHeaders["X-RateLimit-Limit"] = fmt.Sprintf("%d", limit)
		rateLimitHeaders["X-RateLimit-Remaining"] = fmt.Sprintf("%d", remaining)
		rateLimitHeaders["X-RateLimit-Reset"] = fmt.Sprintf("%d", resetAt.Unix())
		if !allowed {
			h.metrics.RecordRateLimitRejection(identity.KeyPrefix, identity.ProjectID)
			rateLimitHeaders["Retry-After"] = fmt.Sprintf("%d", int(time.Until(resetAt).Seconds())+1)
			return nil, &GovernanceError{
				StatusCode: 429,
				Type:       "rate_limit_exceeded",
				Message:    fmt.Sprintf("rate limit exceeded: %d RPM limit", identity.RPMLimit),
				Stage:      "rate_limit",
				Headers:    rateLimitHeaders,
			}
		}
	}

	// ------ Step 3: Budget check ------
	if identity != nil && !identity.SkipBudget && identity.BudgetLimitUSD > 0 && h.usageW != nil {
		spent, err := usageapi.GetSpendForCurrentPeriod(ctx, h.usageW.DB(), identity.VirtualKeyID, identity.BudgetPeriod)
		if err != nil {
			h.logger.Warn("budget check failed, allowing request",
				"virtual_key_id", identity.VirtualKeyID,
				"error", err,
			)
		} else if spent >= identity.BudgetLimitUSD {
			h.metrics.RecordBudgetRejection(identity.KeyPrefix, identity.ProjectID)
			return nil, &GovernanceError{
				StatusCode: 403,
				Type:       "budget_exceeded",
				Message:    fmt.Sprintf("budget exceeded: $%.2f of $%.2f", spent, identity.BudgetLimitUSD),
				Stage:      "budget",
			}
		}
	}

	// ------ Step 4: Input guardrails ------
	if h.shouldRunInputGuardrails(req.Model) {
		messages := toGuardrailMessages(req.Messages)
		result, err := h.guardrails.CheckInput(ctx, messages)
		if err != nil {
			h.logger.Error("guardrail input check failed", "error", err)
			return nil, &GovernanceError{
				StatusCode: 500,
				Type:       "guardrail_error",
				Message:    "guardrail check failed",
				Stage:      "input",
			}
		}
		h.metrics.RecordGuardrailDuration("pipeline", "input", time.Since(start))
		switch result.Action {
		case guardrails.Block:
			h.metrics.RecordGuardrailBlock(result.StageName, "input", req.Model)
			return nil, &GovernanceError{
				StatusCode: 400,
				Type:       "guardrail_block",
				Message:    result.Message,
				Stage:      "input",
			}
		case guardrails.Redact:
			h.metrics.RecordGuardrailRedaction(result.StageName, "input", req.Model)
			for i, gm := range result.RedactedMessages {
				if i < len(req.Messages) {
					req.Messages[i].Content = json.RawMessage(`"` + gm.Content + `"`)
				}
			}
		}
	}

	// ------ Step 5: Cache lookup ------
	cacheKey := ""
	if h.cache != nil && h.cfg.Cache.Exact.Enabled {
		if key, err := cache.HashRequest(req); err == nil {
			cacheKey = key
			if cached, ok, err := h.cache.Get(ctx, key); err == nil && ok {
				h.metrics.RecordCacheHit(req.Model)
				return &GovernedResult{
					Response:   cached,
					CacheHit:   true,
					DurationMS: time.Since(start).Milliseconds(),
					Headers: map[string]string{
						"X-Cache": "HIT",
					},
				}, nil
			}
		}
	}
	if cacheKey != "" {
		h.metrics.RecordCacheMiss(req.Model)
	}

	// ------ Step 6: Routing + circuit breaker + retry ------
	// Check for pre-computed plan in context (set by ChatCompletions for residency filtering)
	plan := planFromContext(ctx)
	if plan == nil {
		var planErr error
		plan, planErr = h.router.Plan(req.Model)
		if planErr != nil {
			return nil, &GovernanceError{
				StatusCode: 400,
				Type:       "model_not_found",
				Message:    planErr.Error(),
				Stage:      "routing",
			}
		}
	}

	// ------ Step 7: Provider call (via executePlan which handles circuit breaker + retry) ------
	resp, target, attempts, execErr := h.executePlan(ctx, req, plan)

	// Record health after provider call (AC-03-03).
	if h.healthTracker != nil {
		if execErr != nil {
			h.healthTracker.RecordFailure(target.Provider, target.Model, target.Region)
		} else {
			h.healthTracker.RecordSuccess(target.Provider, target.Model, target.Region)
		}
	}

	if execErr != nil {
		h.metrics.RecordProviderCall(target.Provider, target.Model, time.Since(start), classifyError(execErr))

		// Extract HTTP status and body from provider error for enrichment.
		statusCode := 0
		rawBody := ""
		var httpErr *providers.HTTPError
		if errors.As(execErr, &httpErr) {
			statusCode = httpErr.StatusCode
			rawBody = httpErr.Body
		}
		enrichedMsg := errtypes.EnrichProviderError(target.Provider, statusCode, rawBody)

		return nil, &GovernanceError{
			StatusCode: 502,
			Type:       "provider_error",
			Message:    enrichedMsg,
			Stage:      "provider",
		}
	}

	// ------ Step 8: Output guardrails ------
	if h.shouldRunOutputGuardrails(req.Model) && resp != nil && len(resp.Choices) > 0 {
		outputContent := extractChoiceContent(resp)
		result, err := h.guardrails.CheckOutput(ctx, outputContent)
		if err != nil {
			h.logger.Error("guardrail output check failed", "error", err)
		} else {
			h.metrics.RecordGuardrailDuration("pipeline", "output", time.Since(start))
			switch result.Action {
			case guardrails.Block:
				h.metrics.RecordGuardrailBlock(result.StageName, "output", req.Model)
				return nil, &GovernanceError{
					StatusCode: 400,
					Type:       "guardrail_block",
					Message:    result.Message,
					Stage:      "output",
				}
			case guardrails.Redact:
				h.metrics.RecordGuardrailRedaction(result.StageName, "output", req.Model)
				if len(resp.Choices) > 0 {
					resp.Choices[0].Message.Content = json.RawMessage(`"` + result.Message + `"`)
				}
			}
		}
	}

	// ------ Step 9: Cache store ------
	if cacheKey != "" && h.cache != nil && h.cfg.Cache.Exact.Enabled {
		ttl := time.Duration(h.cfg.Cache.Exact.TTLSeconds) * time.Second
		_ = h.cache.Set(ctx, cacheKey, resp, ttl)
	}

	// Calculate cost for result and usage logging
	cost := h.calculateCost(target, resp.Usage)

	// ------ Step 10: Usage logging ------
	if identity != nil && !identity.SkipUsageLog && h.usageW != nil {
		promptTokens := 0
		completionTokens := 0
		totalTokens := 0
		if resp.Usage != nil {
			promptTokens = resp.Usage.PromptTokens
			completionTokens = resp.Usage.CompletionTokens
			totalTokens = resp.Usage.TotalTokens
		}

		h.usageW.Record(usageapi.Entry{
			ProjectID:        identity.ProjectID,
			VirtualKeyID:     identity.VirtualKeyID,
			Model:            req.Model,
			Provider:         target.Provider,
			ProviderModel:    target.Model,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			CostUSD:          cost,
			CacheHit:         false,
			Stream:           false,
			Attempts:         attempts,
			DurationMS:       time.Since(start).Milliseconds(),
		})
	}

	// ------ Step 10b: Audit body logging (opt-in) ------
	if h.logBodies && h.auditLog != nil {
		authCtx := auth.FromContext(ctx)
		actor := "anonymous"
		resource := req.Model
		if authCtx != nil {
			actor = authCtx.VirtualKeyID
			resource = authCtx.ProjectID + "/" + req.Model
		}

		// Capture request messages
		reqMessages := make([]map[string]string, 0, len(req.Messages))
		for _, m := range req.Messages {
			reqMessages = append(reqMessages, map[string]string{
				"role":    string(m.Role),
				"content": string(m.Content),
			})
		}

		meta := map[string]any{
			"model":    req.Model,
			"provider": target.Provider,
			"stream":   false,
			"attempts": attempts,
		}
		if resp.Usage != nil {
			meta["prompt_tokens"] = resp.Usage.PromptTokens
			meta["completion_tokens"] = resp.Usage.CompletionTokens
		}
		meta["cost_usd"] = cost
		meta["duration_ms"] = time.Since(start).Milliseconds()
		meta["request_messages"] = reqMessages

		// Capture response content
		if len(resp.Choices) > 0 {
			meta["response_content"] = string(resp.Choices[0].Message.Content)
			meta["finish_reason"] = resp.Choices[0].FinishReason
		}

		h.auditLog.Record(audit.Event{
			EventType: audit.EventChatCompletion,
			Actor:     actor,
			Action:    "complete",
			Resource:  resource,
			Outcome:   "success",
			RequestID: requestid.FromContext(ctx),
			Metadata:  meta,
		})
	}

	// ------ Step 11: Metrics ------
	h.metrics.RecordRequest(req.Model, target.Provider, "200", false, time.Since(start))
	h.metrics.RecordTokens(req.Model, target.Provider, usageValue(resp, "prompt"), usageValue(resp, "completion"))
	if cost > 0 {
		h.metrics.RecordCost(req.Model, target.Provider, cost)
	}

	durationMS := time.Since(start).Milliseconds()

	// Build result headers
	resultHeaders := map[string]string{
		"X-Provider": target.Provider,
		"X-Cache":    "MISS",
	}
	// AUTH-03: X-Cost-USD is only meaningful when tracked per-key.
	// When identity is nil (A2A path), cost tracking is unavailable.
	if identity != nil {
		resultHeaders["X-Cost-USD"] = fmt.Sprintf("%.6f", cost)
	}
	for k, v := range rateLimitHeaders {
		resultHeaders[k] = v
	}

	return &GovernedResult{
		Response:   resp,
		Target:     target,
		Attempts:   attempts,
		CacheHit:   false,
		DurationMS: durationMS,
		CostUSD:    cost,
		Headers:    resultHeaders,
	}, nil
}

// modelAllowed checks if the model is in the allowed list. An empty list
// means all models are permitted.
func modelAllowed(model string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, m := range allowed {
		if strings.EqualFold(m, model) {
			return true
		}
	}
	return false
}

// isGovernanceError checks if an error is a GovernanceError.
func isGovernanceError(err error) bool {
	var ge *GovernanceError
	return errors.As(err, &ge)
}

// AsGovernanceError extracts a *GovernanceError from an error chain, or
// returns nil if the error is not a GovernanceError.
func AsGovernanceError(err error) *GovernanceError {
	var ge *GovernanceError
	if errors.As(err, &ge) {
		return ge
	}
	return nil
}

// newGovernanceIdentity creates a GovernanceIdentity with skip flags for
// MCP re-invocations. This prevents double-counting of rate limits and
// budgets when the executor calls the handler again for tool-call rounds.
func newGovernanceIdentity(projectID, virtualKeyID, keyPrefix, name string, allowedModels, allowedProviders []string, rpmLimit, tpmLimit int, budgetLimitUSD float64, budgetPeriod, source string, skipRateLimit, skipBudget, skipUsageLog bool) *GovernanceIdentity {
	return &GovernanceIdentity{
		ProjectID:        projectID,
		VirtualKeyID:     virtualKeyID,
		KeyPrefix:        keyPrefix,
		Name:             name,
		AllowedModels:    allowedModels,
		AllowedProviders: allowedProviders,
		RPMLimit:         rpmLimit,
		TPMLimit:         tpmLimit,
		BudgetLimitUSD:   budgetLimitUSD,
		BudgetPeriod:     budgetPeriod,
		Source:           source,
		SkipRateLimit:    skipRateLimit,
		SkipBudget:       skipBudget,
		SkipUsageLog:     skipUsageLog,
	}
}

// ---------------------------------------------------------------------------
// Plan context helpers — allow callers to pre-compute a routing plan and
// pass it through the context so ExecuteGoverned reuses it instead of
// creating a new one. This is used by ChatCompletions() which needs to
// apply data residency filtering before the governance pipeline runs.
// ---------------------------------------------------------------------------

type planKey struct{}

// planWithContext stores a pre-computed routing plan in the context.
func planWithContext(ctx context.Context, plan *routing.Plan) context.Context {
	return context.WithValue(ctx, planKey{}, plan)
}

// planFromContext retrieves a pre-computed routing plan from the context.
// Returns nil if no plan was stored.
func planFromContext(ctx context.Context) *routing.Plan {
	p, _ := ctx.Value(planKey{}).(*routing.Plan)
	return p
}

// SetHealthTracker sets the health tracker used to record provider call outcomes.
// TASK-04 wires the same *health.Tracker instance into both Handler and Router.
func (h *Handler) SetHealthTracker(t *health.Tracker) {
	h.healthTracker = t
}

// Ensure GovernanceError satisfies the error interface at compile time.
var _ error = (*GovernanceError)(nil)

// ---------------------------------------------------------------------------
// Pre-stream governance (steps 1-4)
// ---------------------------------------------------------------------------

// preStreamGovernance runs governance steps 1-4 (model validation, rate limiting,
// budget check, input guardrails) before a streaming request is forwarded to the
// provider. It returns rate limit headers on success, or a GovernanceError if any
// check blocks the request.
//
// If identity is nil (A2A path), all identity-based checks are skipped but
// guardrails are still enforced (when configured).
func (h *Handler) preStreamGovernance(ctx context.Context, req openaischema.ChatCompletionRequest, identity *GovernanceIdentity) (map[string]string, error) {
	rateLimitHeaders := make(map[string]string)

	// ------ Step 1: Model validation ------
	if identity != nil && len(identity.AllowedModels) > 0 {
		allowed := false
		for _, m := range identity.AllowedModels {
			if strings.EqualFold(m, req.Model) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, &GovernanceError{
				StatusCode: 403,
				Type:       "model_not_allowed",
				Message:    fmt.Sprintf("model %q is not allowed for this key", req.Model),
				Stage:      "model_validation",
			}
		}
	}

	// ------ Step 2: Rate limiting ------
	if identity != nil && !identity.SkipRateLimit && identity.RPMLimit > 0 {
		limiter := h.getLimiter(identity.VirtualKeyID, identity.RPMLimit, identity.TPMLimit)
		allowed, limit, remaining, resetAt := limiter.CheckRPM(identity.VirtualKeyID)
		rateLimitHeaders["X-RateLimit-Limit"] = fmt.Sprintf("%d", limit)
		rateLimitHeaders["X-RateLimit-Remaining"] = fmt.Sprintf("%d", remaining)
		rateLimitHeaders["X-RateLimit-Reset"] = fmt.Sprintf("%d", resetAt.Unix())
		if !allowed {
			h.metrics.RecordRateLimitRejection(identity.KeyPrefix, identity.ProjectID)
			rateLimitHeaders["Retry-After"] = fmt.Sprintf("%d", int(time.Until(resetAt).Seconds())+1)
			return nil, &GovernanceError{
				StatusCode: 429,
				Type:       "rate_limit_exceeded",
				Message:    fmt.Sprintf("rate limit exceeded: %d RPM limit", identity.RPMLimit),
				Stage:      "rate_limit",
				Headers:    rateLimitHeaders,
			}
		}
	}

	// ------ Step 3: Budget check ------
	if identity != nil && !identity.SkipBudget && identity.BudgetLimitUSD > 0 && h.usageW != nil {
		spent, err := usageapi.GetSpendForCurrentPeriod(ctx, h.usageW.DB(), identity.VirtualKeyID, identity.BudgetPeriod)
		if err != nil {
			h.logger.Warn("budget check failed, allowing request",
				"virtual_key_id", identity.VirtualKeyID,
				"error", err,
			)
		} else if spent >= identity.BudgetLimitUSD {
			h.metrics.RecordBudgetRejection(identity.KeyPrefix, identity.ProjectID)
			return nil, &GovernanceError{
				StatusCode: 403,
				Type:       "budget_exceeded",
				Message:    fmt.Sprintf("budget exceeded: $%.2f of $%.2f", spent, identity.BudgetLimitUSD),
				Stage:      "budget",
			}
		}
	}

	// ------ Step 4: Input guardrails ------
	if h.shouldRunInputGuardrails(req.Model) {
		messages := toGuardrailMessages(req.Messages)
		result, err := h.guardrails.CheckInput(ctx, messages)
		if err != nil {
			h.logger.Error("guardrail input check failed", "error", err)
			return nil, &GovernanceError{
				StatusCode: 500,
				Type:       "guardrail_error",
				Message:    "guardrail check failed",
				Stage:      "input",
			}
		}
		h.metrics.RecordGuardrailDuration("pipeline", "input", time.Since(time.Now()))
		switch result.Action {
		case guardrails.Block:
			h.metrics.RecordGuardrailBlock(result.StageName, "input", req.Model)
			return nil, &GovernanceError{
				StatusCode: 400,
				Type:       "guardrail_block",
				Message:    result.Message,
				Stage:      "input",
			}
		case guardrails.Redact:
			h.metrics.RecordGuardrailRedaction(result.StageName, "input", req.Model)
			for i, gm := range result.RedactedMessages {
				if i < len(req.Messages) {
					req.Messages[i].Content = json.RawMessage(`"` + gm.Content + `"`)
				}
			}
		}
	}

	return rateLimitHeaders, nil
}

// ---------------------------------------------------------------------------
// Post-stream finalization (steps 10-11)
// ---------------------------------------------------------------------------

// postStreamGovernance runs governance steps 10-11 (usage logging and metrics)
// after a streaming request completes successfully. It is a no-op when identity
// is nil or SkipUsageLog is true.
func (h *Handler) postStreamGovernance(r *http.Request, req openaischema.ChatCompletionRequest, target providers.Target, attempts int, promptTokens int, completionTokens int, identity *GovernanceIdentity, durationMS int64) {
	// ------ Step 10: Usage logging ------
	if identity != nil && !identity.SkipUsageLog && h.usageW != nil {
		totalTokens := promptTokens + completionTokens
		cost := 0.0
		if h.prices != nil {
			cost = h.prices.CalculateCost(target.Provider, target.Model, promptTokens, completionTokens)
		}

		authCtx := auth.FromContext(r.Context())
		projectID := ""
		virtualKeyID := ""
		if authCtx != nil {
			projectID = authCtx.ProjectID
			virtualKeyID = authCtx.VirtualKeyID
		}

		h.usageW.Record(usageapi.Entry{
			ProjectID:        projectID,
			VirtualKeyID:     virtualKeyID,
			Model:            req.Model,
			Provider:         target.Provider,
			ProviderModel:    target.Model,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			CostUSD:          cost,
			CacheHit:         false,
			Stream:           true,
			Attempts:         attempts,
			DurationMS:       durationMS,
		})
	}

	// ------ Step 11: Metrics ------
	h.metrics.RecordRequest(req.Model, target.Provider, "200", true, time.Duration(durationMS)*time.Millisecond)
	h.metrics.RecordTokens(req.Model, target.Provider, promptTokens, completionTokens)
	if identity != nil {
		cost := 0.0
		if h.prices != nil {
			cost = h.prices.CalculateCost(target.Provider, target.Model, promptTokens, completionTokens)
		}
		if cost > 0 {
			h.metrics.RecordCost(req.Model, target.Provider, cost)
		}
	}
}

// SetAuditLogger sets the audit logger for request/response body logging.
func (h *Handler) SetAuditLogger(l *audit.Logger) {
	h.auditLog = l
}

// SetLogBodies enables or disables request/response body capture.
func (h *Handler) SetLogBodies(enabled bool) {
	h.logBodies = enabled
}

// Ensure unused-import guard for slog — used in ExecuteGoverned logging.
var _ = slog.Default
