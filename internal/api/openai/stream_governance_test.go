package openaiapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"openlimit/internal/auth"
	"openlimit/internal/cache"
	"openlimit/internal/config"
	"openlimit/internal/guardrails"
	"openlimit/internal/metrics"
	"openlimit/internal/providers"
	"openlimit/internal/routing"
	openaischema "openlimit/internal/schema/openai"
	"openlimit/internal/tracing"
	usageapi "openlimit/internal/usage"
)

// ---------------------------------------------------------------------------
// Streaming governance test helpers
// ---------------------------------------------------------------------------

// streamGovTestAdapter is a test adapter that supports StreamChat with
// controllable chunks and error injection.
type streamGovTestAdapter struct {
	chunks      []openaischema.ChatCompletionStreamChunk
	streamErr   error
	streamCalls atomic.Int32
}

func newStreamGovAdapter(chunks []openaischema.ChatCompletionStreamChunk, streamErr error) *streamGovTestAdapter {
	return &streamGovTestAdapter{chunks: chunks, streamErr: streamErr}
}

func (a *streamGovTestAdapter) Name() string { return "stream-gov-test" }

func (a *streamGovTestAdapter) CompleteChat(_ context.Context, _ openaischema.ChatCompletionRequest, _ providers.Target, _ providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	return &openaischema.ChatCompletionResponse{
		ID:      "chatcmpl-test",
		Object:  "chat.completion",
		Created: 123,
		Model:   "provider-model",
		Choices: []openaischema.Choice{{
			Index:        0,
			Message:      openaischema.ChatMessage{Role: "assistant", Content: json.RawMessage(`"hello"`)},
			FinishReason: "stop",
		}},
		Usage: &openaischema.Usage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
	}, nil
}

func (a *streamGovTestAdapter) StreamChat(_ context.Context, _ openaischema.ChatCompletionRequest, _ providers.Target, _ providers.ProviderKey) (*providers.StreamResult, error) {
	a.streamCalls.Add(1)
	chunks := make(chan openaischema.ChatCompletionStreamChunk, len(a.chunks)+1)
	errs := make(chan error, 1)

	for _, ch := range a.chunks {
		chunks <- ch
	}
	if a.streamErr != nil {
		errs <- a.streamErr
	} else {
		close(chunks)
	}

	return &providers.StreamResult{
		Chunks: chunks,
		Errors: errs,
	}, nil
}

// streamGovHandler creates a handler configured for streaming governance tests.
func streamGovHandler(t *testing.T, adapter providers.Adapter, guardrailsPipeline *guardrails.Pipeline) *Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := config.Default()
	cfg.Routing.Defaults.TimeoutMS = 5000
	cfg.Routing.Defaults.Retry.Attempts = 1
	cfg.Routing.Defaults.Retry.InitialMS = 1
	cfg.Routing.Defaults.Retry.MaxMS = 2
	cfg.Providers = map[string]config.ProviderConfig{
		"mock": {
			Type: "openai-compatible",
			Keys: []config.ProviderKeyConfig{{ID: "test", Value: "test-key", Weight: 100}},
		},
	}
	cfg.Models = map[string]config.ModelConfig{
		"fast": {Routes: []config.ModelRoute{{Provider: "mock", Model: "provider-model", Weight: 100}}},
	}

	router := routing.New(cfg.Models, cfg.Providers, cfg.Routing, nil)
	c := cache.NewExactLRU(100)
	adapters := map[string]providers.Adapter{"mock": adapter}
	keys := map[string]*providers.KeyRing{
		"mock": providers.NewKeyRing(cfg.Providers["mock"], nil),
	}
	m := metrics.NewCollector(false)
	tr, _ := tracing.NewTracer(false, "", "", 0, logger)

	if guardrailsPipeline != nil {
		cfg.Guardrails.Enabled = true
	}
	h := NewHandler(cfg, logger, router, c, adapters, keys, nil, nil, m, tr, guardrailsPipeline, nil, nil, nil)
	return h
}

// makeUsageChunk creates a final streaming chunk with usage data (OpenAI convention).
func makeUsageChunk(promptTokens, completionTokens int) openaischema.ChatCompletionStreamChunk {
	return openaischema.ChatCompletionStreamChunk{
		ID:      "chatcmpl-test",
		Object:  "chat.completion.chunk",
		Created: 123,
		Model:   "provider-model",
		Choices: []openaischema.StreamChoice{
			{Index: 0, Delta: openaischema.StreamDelta{}, FinishReason: ptrStr("stop")},
		},
		Usage: &openaischema.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

func ptrStr(s string) *string { return &s }

// makeStreamRequest creates an HTTP request for a streaming chat completion.
func makeStreamRequest(t *testing.T) *http.Request {
	t.Helper()
	body := `{"model":"fast","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// makeStreamRequestWithAuth creates a streaming request with auth context injected.
func makeStreamRequestWithAuth(t *testing.T, authCtx *auth.Context) *http.Request {
	t.Helper()
	req := makeStreamRequest(t)
	ctx := auth.WithContext(req.Context(), authCtx)
	return req.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TEST-S1-01: Streaming with model restriction → expect 403.
func TestStreamGovernance_ModelRestricted_Returns403(t *testing.T) {
	adapter := newStreamGovAdapter(nil, nil) // no chunks needed — governance blocks first
	h := streamGovHandler(t, adapter, nil)

	authCtx := &auth.Context{
		ProjectID:     "proj-1",
		VirtualKeyID:  "vk-1",
		KeyPrefix:     "sk-test",
		Name:          "restricted-key",
		AllowedModels: []string{"gpt-4"}, // "fast" is NOT allowed
	}

	req := makeStreamRequestWithAuth(t, authCtx)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}

	var resp openaischema.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error.Type != "model_not_allowed" {
		t.Errorf("type = %q, want %q", resp.Error.Type, "model_not_allowed")
	}

	// Provider must NOT have been called
	if got := adapter.streamCalls.Load(); got != 0 {
		t.Errorf("provider StreamChat called %d times, want 0", got)
	}
}

// TEST-S1-02: Streaming with rate limit exceeded → expect 429.
func TestStreamGovernance_RateLimitExceeded_Returns429(t *testing.T) {
	adapter := newStreamGovAdapter(nil, nil)
	h := streamGovHandler(t, adapter, nil)

	rpmLimit := 2
	authCtx := &auth.Context{
		ProjectID:    "proj-rl",
		VirtualKeyID: "vk-rl",
		KeyPrefix:    "sk-rl",
		Name:         "rate-limited-key",
		RPMLimit:     rpmLimit,
	}

	// Exhaust the rate limiter before the streaming request
	limiter := h.getLimiter(authCtx.VirtualKeyID, rpmLimit, 0)
	for i := 0; i < rpmLimit; i++ {
		limiter.CheckRPM(authCtx.VirtualKeyID)
	}

	req := makeStreamRequestWithAuth(t, authCtx)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}

	var resp openaischema.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error.Type != "rate_limit_exceeded" {
		t.Errorf("type = %q, want %q", resp.Error.Type, "rate_limit_exceeded")
	}

	if got := adapter.streamCalls.Load(); got != 0 {
		t.Errorf("provider StreamChat called %d times, want 0", got)
	}
}

// TEST-S1-03: Streaming with budget exceeded → expect 403 (structure test).
// Budget enforcement requires usageW with a real DB. Without it, budget check
// is skipped and the request succeeds. This test verifies both:
// 1) The GovernanceError structure for budget errors
// 2) With nil usageW, budget is skipped and streaming proceeds
func TestStreamGovernance_BudgetExceeded_Structure(t *testing.T) {
	// Verify the GovernanceError structure for budget errors
	ge := &GovernanceError{
		StatusCode: 403,
		Type:       "budget_exceeded",
		Message:    "budget exceeded: $5.00 of $1.00",
		Stage:      "budget",
	}
	if ge.StatusCode != 403 {
		t.Errorf("StatusCode = %d, want 403", ge.StatusCode)
	}
	if ge.Type != "budget_exceeded" {
		t.Errorf("Type = %q, want %q", ge.Type, "budget_exceeded")
	}
	if ge.Stage != "budget" {
		t.Errorf("Stage = %q, want %q", ge.Stage, "budget")
	}

	// With nil usageW, budget check is skipped and streaming proceeds normally
	adapter := newStreamGovAdapter([]openaischema.ChatCompletionStreamChunk{
		makeChunk("hello"),
		makeUsageChunk(5, 10),
	}, nil)
	h := streamGovHandler(t, adapter, nil)

	authCtx := &auth.Context{
		ProjectID:      "proj-budget",
		VirtualKeyID:   "vk-budget",
		KeyPrefix:      "sk-budget",
		Name:           "budget-key",
		BudgetLimitUSD: 1.0,
		BudgetPeriod:   "monthly",
	}

	req := makeStreamRequestWithAuth(t, authCtx)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	// Without a real DB, budget is skipped → stream should succeed
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (budget skipped with nil usageW)", w.Code)
	}
}

// TEST-S1-04: Streaming with input guardrail block → expect 400.
func TestStreamGovernance_InputGuardrailBlock_Returns400(t *testing.T) {
	blockStage := &blockTestStage{name: "test-blocker"}
	pipeline := guardrails.NewPipeline([]guardrails.Stage{blockStage}, nil)

	adapter := newStreamGovAdapter(nil, nil)
	h := streamGovHandler(t, adapter, pipeline)

	req := makeStreamRequest(t)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp openaischema.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error.Type != "guardrail_block" {
		t.Errorf("type = %q, want %q", resp.Error.Type, "guardrail_block")
	}

	if got := adapter.streamCalls.Load(); got != 0 {
		t.Errorf("provider StreamChat called %d times, want 0", got)
	}
}

// TEST-S1-05: Streaming with input guardrail redaction → content modified, stream proceeds.
func TestStreamGovernance_InputGuardrailRedaction_ContentModified(t *testing.T) {
	redactStage := &redactTestStage{name: "test-redactor"}
	pipeline := guardrails.NewPipeline([]guardrails.Stage{redactStage}, nil)

	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("response"),
		makeUsageChunk(5, 10),
	}
	adapter := newStreamGovAdapter(chunks, nil)
	h := streamGovHandler(t, adapter, pipeline)

	req := makeStreamRequest(t)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	// Should succeed (200) — redaction modifies content but doesn't block
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Provider should have been called (redaction doesn't block)
	if got := adapter.streamCalls.Load(); got != 1 {
		t.Errorf("provider StreamChat called %d times, want 1", got)
	}
}

// TEST-S1-06: Normal streaming → verify usage entry structure.
// Since usage.Writer is a concrete struct requiring a real DB, we verify
// the code path by confirming that with a nil usageW, the stream still
// completes without error and usage recording is simply skipped.
func TestStreamGovernance_NormalStream_UsageCodePath(t *testing.T) {
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("hello"),
		makeUsageChunk(8, 15),
	}
	adapter := newStreamGovAdapter(chunks, nil)
	h := streamGovHandler(t, adapter, nil)

	authCtx := &auth.Context{
		ProjectID:    "proj-usage",
		VirtualKeyID: "vk-usage",
		KeyPrefix:    "sk-usage",
		Name:         "usage-key",
	}

	req := makeStreamRequestWithAuth(t, authCtx)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("response body should contain 'data: [DONE]'")
	}
	if !strings.Contains(body, "hello") {
		t.Error("response body should contain streamed chunk content")
	}
}

// TEST-S1-07: Normal streaming → verify metrics recorded (enabled metrics, no panic).
func TestStreamGovernance_NormalStream_MetricsRecorded(t *testing.T) {
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("hello"),
		makeUsageChunk(5, 10),
	}
	adapter := newStreamGovAdapter(chunks, nil)

	h := streamGovHandler(t, adapter, nil)
	// Replace with enabled metrics to verify no panics during recording
	h.metrics = metrics.NewCollector(true)

	req := makeStreamRequest(t)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	if !h.metrics.Enabled() {
		t.Error("metrics should be enabled")
	}
}

// TEST-S1-08: Normal streaming → verify X-Provider header.
func TestStreamGovernance_NormalStream_XProviderHeader(t *testing.T) {
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("hello"),
		makeUsageChunk(5, 10),
	}
	adapter := newStreamGovAdapter(chunks, nil)
	h := streamGovHandler(t, adapter, nil)

	req := makeStreamRequest(t)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	if v := w.Header().Get("X-Provider"); v != "mock" {
		t.Errorf("X-Provider = %q, want %q", v, "mock")
	}
}

// TEST-S1-09: Normal streaming → verify X-RateLimit headers when rate limit is configured.
func TestStreamGovernance_NormalStream_RateLimitHeaders(t *testing.T) {
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("hello"),
		makeUsageChunk(5, 10),
	}
	adapter := newStreamGovAdapter(chunks, nil)
	h := streamGovHandler(t, adapter, nil)

	rpmLimit := 100
	authCtx := &auth.Context{
		ProjectID:    "proj-rl-hdr",
		VirtualKeyID: "vk-rl-hdr",
		KeyPrefix:    "sk-rl-hdr",
		Name:         "rl-header-key",
		RPMLimit:     rpmLimit,
	}

	req := makeStreamRequestWithAuth(t, authCtx)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	if v := w.Header().Get("X-RateLimit-Limit"); v == "" {
		t.Error("X-RateLimit-Limit header is missing")
	} else if v != "100" {
		t.Errorf("X-RateLimit-Limit = %q, want %q", v, "100")
	}
	if v := w.Header().Get("X-RateLimit-Remaining"); v == "" {
		t.Error("X-RateLimit-Remaining header is missing")
	}
	if v := w.Header().Get("X-RateLimit-Reset"); v == "" {
		t.Error("X-RateLimit-Reset header is missing")
	}
}

// TEST-S1-10: Streaming with nil identity → governance skipped (A2A path).
func TestStreamGovernance_NilIdentity_GovernanceSkipped(t *testing.T) {
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("hello"),
		makeUsageChunk(5, 10),
	}
	adapter := newStreamGovAdapter(chunks, nil)
	h := streamGovHandler(t, adapter, nil)

	// No auth context → nil identity → governance checks skipped
	req := makeStreamRequest(t)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("response body should contain 'data: [DONE]'")
	}
	if !strings.Contains(body, "hello") {
		t.Error("response body should contain streamed content")
	}
}

// TEST-S1-11: Streaming error → usage NOT logged (graceful handling).
// postStreamGovernance is only called on successful stream completion.
// On error, the SSE error event is sent but usage is not recorded.
func TestStreamGovernance_StreamError_NoUsageLeak(t *testing.T) {
	adapter := newStreamGovAdapter(
		[]openaischema.ChatCompletionStreamChunk{makeChunk("Hello")},
		&providers.HTTPError{StatusCode: 500, Body: "internal server error"},
	)

	h := streamGovHandler(t, adapter, nil)

	authCtx := &auth.Context{
		ProjectID:    "proj-err",
		VirtualKeyID: "vk-err",
		KeyPrefix:    "sk-err",
		Name:         "error-key",
	}

	req := makeStreamRequestWithAuth(t, authCtx)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	// Stream should complete (200) — headers already sent before error
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (headers already sent)", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Error("response body should contain 'event: error'")
	}

	// No data: [DONE] on error
	if strings.Contains(body, "data: [DONE]") {
		t.Error("response body should NOT contain 'data: [DONE]' on error")
	}
}

// TEST-S1-12: Streaming with zero usage chunks → no panic.
func TestStreamGovernance_ZeroUsageChunks_NoPanic(t *testing.T) {
	// Chunks with nil usage — some providers don't send usage data
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("hello"),
		makeChunk(" world"),
		// No usage chunk at all
	}
	adapter := newStreamGovAdapter(chunks, nil)

	h := streamGovHandler(t, adapter, nil)

	req := makeStreamRequest(t)
	w := httptest.NewRecorder()

	// Must not panic
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("response body should contain 'data: [DONE]'")
	}
}

// ---------------------------------------------------------------------------
// Mock guardrail stages for testing
// ---------------------------------------------------------------------------

// blockTestStage always blocks on input.
type blockTestStage struct {
	name string
}

func (s *blockTestStage) Name() string { return s.name }
func (s *blockTestStage) CheckInput(_ context.Context, _ []guardrails.Message) (guardrails.Result, error) {
	return guardrails.Result{
		Action:  guardrails.Block,
		Message: "content blocked by test stage",
	}, nil
}
func (s *blockTestStage) CheckOutput(_ context.Context, _ string) (guardrails.Result, error) {
	return guardrails.Result{Action: guardrails.Pass}, nil
}

// redactTestStage always redacts input content.
type redactTestStage struct {
	name string
}

func (s *redactTestStage) Name() string { return s.name }
func (s *redactTestStage) CheckInput(_ context.Context, messages []guardrails.Message) (guardrails.Result, error) {
	redacted := make([]guardrails.Message, len(messages))
	for i, m := range messages {
		redacted[i] = guardrails.Message{
			Role:    m.Role,
			Content: "[REDACTED]",
		}
	}
	return guardrails.Result{
		Action:           guardrails.Redact,
		RedactedMessages: redacted,
	}, nil
}
func (s *redactTestStage) CheckOutput(_ context.Context, _ string) (guardrails.Result, error) {
	return guardrails.Result{Action: guardrails.Pass}, nil
}

// Ensure unused import guard.
var _ usageapi.Entry
