package openaiapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"openlimit/internal/auth"
	"openlimit/internal/audit"
	"openlimit/internal/billing"
	"openlimit/internal/cache"
	"openlimit/internal/circuit"
	"openlimit/internal/config"
	"openlimit/internal/errtypes"
	"openlimit/internal/guardrails"
	"openlimit/internal/health"
	"openlimit/internal/mcp"
	"openlimit/internal/metrics"
	"openlimit/internal/providers"
	"openlimit/internal/ratelimit"
	rediscli "openlimit/internal/redis"
	"openlimit/internal/requestid"
	"openlimit/internal/routing"
	openaischema "openlimit/internal/schema/openai"
	"openlimit/internal/tracing"
	usageapi "openlimit/internal/usage"
)

type Handler struct {
	cfg           config.Config
	logger        *slog.Logger
	router        *routing.Router
	cache         cache.Cache
	adapters      map[string]providers.Adapter
	keys          map[string]*providers.KeyRing
	limitersMu    sync.Mutex
	limiters      map[string]*ratelimit.Limiter
	redisClient   *rediscli.Client
	breakersMu    sync.Mutex
	breakers      map[string]*circuit.Breaker
	prices        *billing.PriceTable
	usageW        *usageapi.Writer
	metrics       *metrics.Collector
	tracer        *tracing.Tracer
	guardrails    *guardrails.Pipeline
	mcpRegistry   *mcp.Registry
	mcpExecutor   *mcp.Executor
	healthTracker *health.Tracker
	auditLog      *audit.Logger
	logBodies     bool
}

func NewHandler(cfg config.Config, logger *slog.Logger, router *routing.Router, exactCache cache.Cache, adapters map[string]providers.Adapter, keys map[string]*providers.KeyRing, prices *billing.PriceTable, usageW *usageapi.Writer, m *metrics.Collector, t *tracing.Tracer, g *guardrails.Pipeline, mcpReg *mcp.Registry, mcpExec *mcp.Executor, redisClient *rediscli.Client) *Handler {
	return &Handler{
		cfg:         cfg,
		logger:      logger,
		router:      router,
		cache:       exactCache,
		adapters:    adapters,
		keys:        keys,
		limiters:    make(map[string]*ratelimit.Limiter),
		breakers:    make(map[string]*circuit.Breaker),
		redisClient: redisClient,
		prices:      prices,
		usageW:      usageW,
		metrics:     m,
		tracer:      t,
		guardrails:  g,
		mcpRegistry: mcpReg,
		mcpExecutor: mcpExec,
	}
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	var req openaischema.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON request body")
		return
	}
	if req.Model == "" {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "messages are required")
		return
	}

	start := time.Now()
	requestID := requestid.FromContext(r.Context())

	// Start chat span for tracing
	ctx, endChatSpan := h.tracer.StartChatSpan(r.Context(), req.Model, req.Stream)
	r = r.WithContext(ctx)
	defer endChatSpan()

	// Build governance identity from auth context
	authCtx := auth.FromContext(r.Context())
	identity := governanceIdentityFromAuth(authCtx)

	// Data residency enforcement (request-level concern — filtered plan stored in context
	// for ExecuteGoverned to reuse, avoiding double routing).
	plan, err := h.router.Plan(req.Model)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "model_not_found", err.Error())
		return
	}
	residency := strings.TrimSpace(r.Header.Get("X-Data-Residency"))
	if residency != "" {
		filtered := routing.FilterByResidency(plan.Targets, residency)
		if len(filtered) == 0 {
			h.metrics.RecordResidencyFilter("denied")
			writeError(w, r, http.StatusForbidden, "residency_denied", fmt.Sprintf("no providers available in data residency region %q", residency))
			return
		}
		h.metrics.RecordResidencyFilter("allowed")
		plan.Targets = filtered
	}

	// MCP: Reject streaming + tool_choice=required + MCP tools present
	if req.Stream && h.mcpRegistry != nil && mcp.IsToolChoiceRequired(req.ToolChoice) {
		if h.mcpRegistry.ToolCount() > 0 {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "streaming with tool_choice=required and MCP tools is not supported")
			return
		}
	}

	// MCP: Merge permitted MCP tools into the request
	var mcpMergeResult *mcp.MergeResult
	if h.mcpRegistry != nil && h.mcpRegistry.ToolCount() > 0 {
		authCtx := auth.FromContext(r.Context())
		var permittedTools []mcp.Tool
		for _, tool := range h.mcpRegistry.AllTools() {
			if authCtx == nil || authCtx.ToolAllowed(tool.Name) {
				permittedTools = append(permittedTools, tool)
			}
		}

		if len(permittedTools) > 0 {
			mergeCfg := mcp.MergeConfig{
				AutoInjectTools:      h.cfg.MCP.AutoInjectTools,
				ToolConflictStrategy: h.cfg.MCP.ToolConflictStrategy,
			}
			mcpMergeResult, err = mcp.MergeTools(req.Tools, req.ToolChoice, permittedTools, mergeCfg, h.logger)
			if err != nil {
				writeError(w, r, http.StatusBadRequest, "tool_merge_error", err.Error())
				return
			}
			if mcpMergeResult.Tools != nil {
				mergedJSON, _ := json.Marshal(mcpMergeResult.Tools)
				req.Tools = mergedJSON
			}
		}
	}

	if req.Stream {
		h.streamChatCompletions(w, r, req, plan, identity, start, requestID)
		return
	}

	// Store filtered plan in context for ExecuteGoverned (avoids double routing)
	govCtx := planWithContext(r.Context(), plan)

	// Execute through unified governance pipeline
	result, err := h.ExecuteGoverned(govCtx, req, identity)
	if err != nil {
		if ge, ok := err.(*GovernanceError); ok {
			writeGovernanceError(w, r, ge)
			return
		}
		// Fallback for non-governance errors
		writeError(w, r, http.StatusBadGateway, "provider_error", err.Error())
		return
	}

	resp := result.Response

	// MCP: Execute tool calls if present and MCP executor is available
	// (MCP tool execution happens AFTER ExecuteGoverned)
	if h.mcpExecutor != nil && mcpMergeResult != nil && len(mcpMergeResult.MCPToolNames) > 0 {
		execResult, err := h.mcpExecutor.Execute(r.Context(), &req, resp, func(ctx context.Context, r2 openaischema.ChatCompletionRequest) (*openaischema.ChatCompletionResponse, error) {
			// Re-invocations skip rate limit, budget, and usage logging
			// to avoid double-counting (already counted at entry)
			skipIdentity := &GovernanceIdentity{
				ProjectID:        identity.ProjectID,
				VirtualKeyID:     identity.VirtualKeyID,
				KeyPrefix:        identity.KeyPrefix,
				Name:             identity.Name,
				AllowedModels:    identity.AllowedModels,
				AllowedProviders: identity.AllowedProviders,
				RPMLimit:         identity.RPMLimit,
				TPMLimit:         identity.TPMLimit,
				BudgetLimitUSD:   identity.BudgetLimitUSD,
				BudgetPeriod:     identity.BudgetPeriod,
				Source:           identity.Source,
				SkipRateLimit:    true,
				SkipBudget:       true,
				SkipUsageLog:     true,
			}
			mcpResult, err := h.ExecuteGoverned(ctx, r2, skipIdentity)
			if err != nil {
				return nil, err
			}
			return mcpResult.Response, nil
		})
		if err != nil {
			if ge, ok := err.(*GovernanceError); ok {
				writeGovernanceError(w, r, ge)
				return
			}
			h.logger.Error("MCP tool execution failed", "error", err, "request_id", requestID)
			writeError(w, r, http.StatusInternalServerError, "mcp_error", err.Error())
			return
		}
		resp = execResult.Response
		if execResult.Timeout {
			w.Header().Set("X-MCP-Timeout", "true")
		}
		if execResult.MaxReached {
			w.Header().Set("X-MCP-Max-Rounds-Reached", "true")
			h.metrics.RecordMCPMaxRoundsExceeded(req.Model)
		}
	}

	// Write response headers from GovernedResult
	for k, v := range result.Headers {
		w.Header().Set(k, v)
	}

	h.logger.Info("chat completion proxied",
		"request_id", requestID,
		"model", req.Model,
		"provider", result.Target.Provider,
		"provider_model", result.Target.Model,
		"attempts", result.Attempts,
		"duration_ms", result.DurationMS,
		"prompt_tokens", usageValue(resp, "prompt"),
		"completion_tokens", usageValue(resp, "completion"),
		"total_tokens", usageValue(resp, "total"),
	)

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) streamChatCompletions(w http.ResponseWriter, r *http.Request, req openaischema.ChatCompletionRequest, plan *routing.Plan, identity *GovernanceIdentity, start time.Time, requestID string) {
	// Pre-stream governance: steps 1-4 (model validation, rate limit, budget, input guardrails)
	rateLimitHeaders, err := h.preStreamGovernance(r.Context(), req, identity)
	if err != nil {
		if ge, ok := err.(*GovernanceError); ok {
			writeGovernanceError(w, r, ge)
			return
		}
		writeError(w, r, http.StatusBadGateway, "provider_error", err.Error())
		return
	}

	stream, target, attempts, err := h.openStream(r.Context(), req, plan)
	if err != nil {
		h.logger.Error("chat completion stream failed before headers",
			"request_id", requestID,
			"model", req.Model,
			"attempts", attempts,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
		writeError(w, r, http.StatusBadGateway, "provider_error", err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "streaming_unsupported", "response writer does not support streaming")
		return
	}

	// Set governance and operational headers BEFORE WriteHeader(200)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Provider", target.Provider)
	w.Header().Set("X-Model", target.Model)
	for k, v := range rateLimitHeaders {
		w.Header().Set(k, v)
	}
	w.WriteHeader(http.StatusOK)

	// Accumulate tokens from SSE chunks (final chunk may carry usage)
	var promptTokens, completionTokens int
	chunkCount := 0
	var streamErr error
	for {
		select {
		case chunk, ok := <-stream.Chunks:
			if !ok {
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				flusher.Flush()

				durationMS := time.Since(start).Milliseconds()

				// Post-stream finalization: steps 10-11 (usage logging + metrics)
				h.postStreamGovernance(r, req, target, attempts, promptTokens, completionTokens, identity, durationMS)

				h.logger.Info("chat completion stream proxied",
					"request_id", requestID,
					"model", req.Model,
					"provider", target.Provider,
					"provider_model", target.Model,
					"attempts", attempts,
					"chunks", chunkCount,
					"duration_ms", durationMS,
					"prompt_tokens", promptTokens,
					"completion_tokens", completionTokens,
				)
				return
			}

			// Accumulate usage from chunks (OpenAI convention: final chunk carries usage)
			if chunk.Usage != nil {
				promptTokens = chunk.Usage.PromptTokens
				completionTokens = chunk.Usage.CompletionTokens
			}

			data, err := json.Marshal(chunk)
			if err != nil {
				streamErr = err
				break
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			chunkCount++

		case err, ok := <-stream.Errors:
			if ok && err != nil {
				streamErr = err
				break
			}
		case <-r.Context().Done():
			streamErr = r.Context().Err()
			break
		}

		if streamErr != nil {
			// Send SSE error event to client
			errMsg := errtypes.EnrichProviderError(target.Provider, extractHTTPStatus(streamErr), "")
			errJSON, _ := json.Marshal(map[string]any{
				"error": map[string]string{
					"type":    "provider_error",
					"message": errMsg,
				},
			})
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", errJSON)
			flusher.Flush()

			h.logger.Error("chat completion stream interrupted",
				"request_id", requestID,
				"model", req.Model,
				"provider", target.Provider,
				"provider_model", target.Model,
				"chunks", chunkCount,
				"duration_ms", time.Since(start).Milliseconds(),
				"error", streamErr,
			)
			return
		}
	}
}

func (h *Handler) executePlan(ctx context.Context, req openaischema.ChatCompletionRequest, plan *routing.Plan) (*openaischema.ChatCompletionResponse, providers.Target, int, error) {
	attemptsPerTarget := h.cfg.Routing.Defaults.Retry.Attempts
	if attemptsPerTarget <= 0 {
		attemptsPerTarget = 1
	}

	totalAttempts := 0
	var lastErr error
	for i, target := range plan.Targets {
		if i > 0 {
			h.metrics.RecordFallback(plan.Targets[i-1].Provider, target.Provider)
		}

		// Circuit breaker check
		breaker := h.getBreaker(target.Provider, target.Model, target.Region)
		if !breaker.Allow() {
			h.metrics.RecordCircuitBreakerRejection(target.Provider, target.Model)
			lastErr = errors.New("circuit breaker open for provider: " + target.Provider)
			continue
		}

		adapter, ok := h.adapters[target.Provider]
		if !ok {
			lastErr = errors.New("provider adapter is not configured: " + target.Provider)
			continue
		}
		keyRing := h.keys[target.Provider]

		for attempt := 1; attempt <= attemptsPerTarget; attempt++ {
			key, err := h.nextProviderKey(target.Provider, keyRing)
			if err != nil {
				lastErr = err
				break
			}
			totalAttempts++

			callCtx := ctx
			cancel := func() {}
			if h.cfg.Routing.Defaults.TimeoutMS > 0 {
				callCtx, cancel = context.WithTimeout(ctx, time.Duration(h.cfg.Routing.Defaults.TimeoutMS)*time.Millisecond)
			}
			callStart := time.Now()
			resp, err := adapter.CompleteChat(callCtx, req, target, key)
			cancel()
			elapsed := time.Since(callStart)
			h.metrics.RecordProviderCall(target.Provider, target.Model, elapsed, "")
			h.metrics.RecordProviderRegionCall(target.Provider, target.Model, target.Region, elapsed)
			if err == nil {
				breaker.RecordSuccess()
				return resp, target, totalAttempts, nil
			}

			lastErr = err
			breaker.RecordFailure()
			h.metrics.RecordProviderCall(target.Provider, target.Model, 0, classifyError(err))
			if !providers.IsRetryable(err) || attempt == attemptsPerTarget {
				break
			}
			h.metrics.RecordRetry(target.Provider, target.Model)
			time.Sleep(backoff(attempt, h.cfg.Routing.Defaults.Retry))
		}
	}

	if lastErr == nil {
		lastErr = errors.New("no provider targets available")
	}
	return nil, providers.Target{}, totalAttempts, lastErr
}

// ExecuteForMCP executes a chat completion request for MCP server mode.
// It routes the request through the governance pipeline and returns the response.
// This implements the mcp.ChatHandler interface.
// The identity parameter is typed as any to avoid circular imports;
// callers pass *GovernanceIdentity which is type-asserted internally.
func (h *Handler) ExecuteForMCP(ctx context.Context, req openaischema.ChatCompletionRequest, identity any) (*openaischema.ChatCompletionResponse, error) {
	var govIdentity *GovernanceIdentity
	if identity != nil {
		// Support direct *GovernanceIdentity (from tests and re-invocations)
		if gi, ok := identity.(*GovernanceIdentity); ok {
			govIdentity = gi
		} else if p, ok := identity.(mcp.IdentityProvider); ok {
			// Support IdentityProvider (from MCPIdentity in mcp package)
			govIdentity = governanceIdentityFromProvider(p)
		}
	}
	result, err := h.ExecuteGoverned(ctx, req, govIdentity)
	if err != nil {
		return nil, err
	}
	return result.Response, nil
}

func (h *Handler) openStream(ctx context.Context, req openaischema.ChatCompletionRequest, plan *routing.Plan) (*providers.StreamResult, providers.Target, int, error) {
	attemptsPerTarget := h.cfg.Routing.Defaults.Retry.Attempts
	if attemptsPerTarget <= 0 {
		attemptsPerTarget = 1
	}

	totalAttempts := 0
	var lastErr error
	for i, target := range plan.Targets {
		if i > 0 {
			h.metrics.RecordFallback(plan.Targets[i-1].Provider, target.Provider)
		}
		adapter, ok := h.adapters[target.Provider]
		if !ok {
			lastErr = errors.New("provider adapter is not configured: " + target.Provider)
			continue
		}
		keyRing := h.keys[target.Provider]

		for attempt := 1; attempt <= attemptsPerTarget; attempt++ {
			key, err := h.nextProviderKey(target.Provider, keyRing)
			if err != nil {
				lastErr = err
				break
			}
			totalAttempts++
			callStart := time.Now()
			stream, err := adapter.StreamChat(ctx, req, target, key)
			h.metrics.RecordProviderCall(target.Provider, target.Model, time.Since(callStart), "")
			if err == nil {
				return stream, target, totalAttempts, nil
			}

			lastErr = err
			h.metrics.RecordProviderCall(target.Provider, target.Model, 0, classifyError(err))
			if !providers.IsRetryable(err) || attempt == attemptsPerTarget {
				break
			}
			h.metrics.RecordRetry(target.Provider, target.Model)
			time.Sleep(backoff(attempt, h.cfg.Routing.Defaults.Retry))
		}
	}

	if lastErr == nil {
		lastErr = errors.New("no provider targets available")
	}
	return nil, providers.Target{}, totalAttempts, lastErr
}

func (h *Handler) nextProviderKey(providerName string, keyRing *providers.KeyRing) (providers.ProviderKey, error) {
	if keyRing != nil {
		if next, ok := keyRing.Next(); ok {
			return next, nil
		}
	}

	providerCfg := h.cfg.Providers[providerName]
	if providerRequiresAuth(providerCfg) {
		return providers.ProviderKey{}, fmt.Errorf("provider %q requires auth but has no active keys", providerName)
	}

	return providers.ProviderKey{}, nil
}

func providerRequiresAuth(provider config.ProviderConfig) bool {
	switch provider.Type {
	case "openai", "anthropic", "gemini", "azure-openai", "bedrock", "vertex", "groq", "cohere", "mistral", "":
		return true
	default:
		return false
	}
}

func backoff(attempt int, cfg config.RetryConfig) time.Duration {
	initial := time.Duration(cfg.InitialMS) * time.Millisecond
	if initial <= 0 {
		initial = 250 * time.Millisecond
	}
	maxDelay := time.Duration(cfg.MaxMS) * time.Millisecond
	if maxDelay <= 0 {
		maxDelay = 4 * time.Second
	}
	delay := initial
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > maxDelay {
			return maxDelay
		}
	}
	return delay
}

func usageValue(resp *openaischema.ChatCompletionResponse, field string) int {
	if resp == nil || resp.Usage == nil {
		return 0
	}
	switch field {
	case "prompt":
		return resp.Usage.PromptTokens
	case "completion":
		return resp.Usage.CompletionTokens
	case "total":
		return resp.Usage.TotalTokens
	default:
		return 0
	}
}

func (h *Handler) calculateCost(target providers.Target, usage *openaischema.Usage) float64 {
	if h.prices == nil || usage == nil {
		return 0
	}
	return h.prices.CalculateCost(target.Provider, target.Model, usage.PromptTokens, usage.CompletionTokens)
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "rate") || strings.Contains(msg, "429"):
		return "rate_limit"
	default:
		return "server_error"
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) getLimiter(keyID string, rpm, tpm int) ratelimit.RateLimiter {
	// Use Redis-backed limiter when available
	if h.redisClient != nil && h.redisClient.Healthy() {
		return ratelimit.NewRedisLimiter(h.redisClient, keyID, rpm, tpm)
	}

	// Fallback to local limiter
	h.limitersMu.Lock()
	defer h.limitersMu.Unlock()

	l, ok := h.limiters[keyID]
	if !ok {
		l = ratelimit.NewLimiter(rpm, tpm)
		h.limiters[keyID] = l
	}
	return l
}

func (h *Handler) getBreaker(provider, model, region string) *circuit.Breaker {
	h.breakersMu.Lock()
	defer h.breakersMu.Unlock()

	key := provider + ":" + model
	if region != "" {
		key = provider + ":" + region + ":" + model
	}
	b, ok := h.breakers[key]
	if !ok {
		b = circuit.NewBreaker(h.redisClient, provider, model, h.logger)
		h.breakers[key] = b
	}
	return b
}

func writeError(w http.ResponseWriter, r *http.Request, status int, typ string, message string) {
	writeJSON(w, status, openaischema.ErrorResponse{Error: openaischema.ErrorBody{
		Message:   message,
		Type:      typ,
		RequestID: requestid.FromContext(r.Context()),
	}})
}

// writeGovernanceError translates a GovernanceError into an HTTP error response.
func writeGovernanceError(w http.ResponseWriter, r *http.Request, ge *GovernanceError) {
	// Write governance headers first
	for k, v := range ge.Headers {
		w.Header().Set(k, v)
	}
	writeJSON(w, ge.StatusCode, openaischema.ErrorResponse{Error: openaischema.ErrorBody{
		Message:   ge.Message,
		Type:      ge.Type,
		RequestID: requestid.FromContext(r.Context()),
	}})
}

// writeErrorWithDetails writes an OpenAI-compatible error response with a details map.
func writeErrorWithDetails(w http.ResponseWriter, r *http.Request, status int, typ string, message string, details map[string]any) {
	requestID := ""
	if r != nil {
		requestID = requestid.FromContext(r.Context())
	}
	writeJSON(w, status, openaischema.ErrorResponse{Error: openaischema.ErrorBody{
		Message:   message,
		Type:      typ,
		Details:   details,
		RequestID: requestID,
	}})
}

// writeGuardrailError writes an OpenAI-compatible error response for guardrail blocks.
func writeGuardrailError(w http.ResponseWriter, r *http.Request, stage string, message string) {
	requestID := ""
	if r != nil {
		requestID = requestid.FromContext(r.Context())
	}
	writeJSON(w, http.StatusBadRequest, openaischema.ErrorResponse{Error: openaischema.ErrorBody{
		Message:   message,
		Type:      "guardrail_block",
		Stage:     stage,
		RequestID: requestID,
	}})
}

// shouldRunInputGuardrails returns true if input guardrails should run for this model.
func (h *Handler) shouldRunInputGuardrails(model string) bool {
	if h.guardrails == nil || !h.guardrails.HasInputStages() {
		return false
	}
	if !h.cfg.Guardrails.Enabled {
		return false
	}
	// Check per-model override
	if mc, ok := h.cfg.Guardrails.Models[model]; ok {
		return mc.Input
	}
	// Default: run if enabled
	return true
}

// shouldRunOutputGuardrails returns true if output guardrails should run for this model.
func (h *Handler) shouldRunOutputGuardrails(model string) bool {
	if h.guardrails == nil || !h.guardrails.HasOutputStages() {
		return false
	}
	if !h.cfg.Guardrails.Enabled {
		return false
	}
	if mc, ok := h.cfg.Guardrails.Models[model]; ok {
		return mc.Output
	}
	return true
}

// toGuardrailMessages converts OpenAI schema messages to guardrail messages.
func toGuardrailMessages(msgs []openaischema.ChatMessage) []guardrails.Message {
	result := make([]guardrails.Message, len(msgs))
	for i, m := range msgs {
		content := string(m.Content)
		// Strip JSON quotes if present
		if len(content) >= 2 && content[0] == '"' && content[len(content)-1] == '"' {
			content = content[1 : len(content)-1]
		}
		result[i] = guardrails.Message{Role: m.Role, Content: content}
	}
	return result
}

// extractHTTPStatus extracts the HTTP status code from a provider error.
func extractHTTPStatus(err error) int {
	var httpErr *providers.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode
	}
	return 0
}

// extractChoiceContent extracts text content from a chat completion response.
func extractChoiceContent(resp *openaischema.ChatCompletionResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	content := string(resp.Choices[0].Message.Content)
	if len(content) >= 2 && content[0] == '"' && content[len(content)-1] == '"' {
		content = content[1 : len(content)-1]
	}
	return content
}
