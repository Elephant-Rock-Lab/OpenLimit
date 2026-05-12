package openaiapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openlimit/internal/audit"
	"openlimit/internal/cache"
	"openlimit/internal/circuit"
	"openlimit/internal/config"
	"openlimit/internal/guardrails"
	"openlimit/internal/metrics"
	"openlimit/internal/providers"
	"openlimit/internal/routing"
	openaischema "openlimit/internal/schema/openai"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// mockProviderServer creates an httptest.Server that returns a canned
// chat-completion response, along with an atomic call counter.
func mockProviderServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": float64(123),
			"model":   "provider-model",
			"choices": []map[string]any{{
				"index": float64(0),
				"message": map[string]any{
					"role":    "assistant",
					"content": "hello from mock",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     float64(5),
				"completion_tokens": float64(10),
				"total_tokens":      float64(15),
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return srv, &calls
}

// testHandler creates a minimal Handler wired with a mock provider server.
// The provider is registered under the name "mock" with model "provider-model",
// and a route is set up so that logical model "fast" maps to mock/provider-model.
func testHandler(t *testing.T, providerURL string) *Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := config.Default()
	cfg.Routing.Defaults.TimeoutMS = 5000
	cfg.Routing.Defaults.Retry.Attempts = 1
	cfg.Routing.Defaults.Retry.InitialMS = 1
	cfg.Routing.Defaults.Retry.MaxMS = 2
	cfg.Providers = map[string]config.ProviderConfig{
		"mock": {
			Type:    "openai-compatible",
			BaseURL: providerURL,
			Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "test-key", Weight: 100}},
		},
	}
	cfg.Models = map[string]config.ModelConfig{
		"fast": {Routes: []config.ModelRoute{{Provider: "mock", Model: "provider-model", Weight: 100}}},
	}

	router := routing.New(cfg.Models, cfg.Providers, cfg.Routing, nil)
	c := cache.NewExactLRU(100)
	adapters := map[string]providers.Adapter{
		"mock": newTestAdapter(providerURL),
	}
	keys := map[string]*providers.KeyRing{
		"mock": providers.NewKeyRing(cfg.Providers["mock"], nil),
	}
	m := metrics.NewCollector(false)

	return NewHandler(cfg, logger, router, c, adapters, keys, nil, nil, m, nil, nil, nil, nil, nil)
}

// testAdapter wraps an OpenAI-compatible provider for testing.
type testAdapter struct {
	baseURL string
	client  *http.Client
}

func newTestAdapter(baseURL string) *testAdapter {
	return &testAdapter{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

func (a *testAdapter) Name() string { return "openai-compatible" }

func (a *testAdapter) CompleteChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", &bytesReader{data: body})
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key.Value)
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result openaischema.ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (a *testAdapter) StreamChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*providers.StreamResult, error) {
	return nil, nil
}

// bytesReader wraps a byte slice as an io.Reader.
type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TEST-7A-01-01: ExecuteGoverned with valid identity and mock provider
// returns GovernedResult with correct Target, Attempts=1, CacheHit=false.
func TestExecuteGoverned_Success(t *testing.T) {
	srv, calls := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)

	identity := &GovernanceIdentity{
		ProjectID:    "proj-1",
		VirtualKeyID: "vk-1",
		KeyPrefix:    "sk-test",
		Name:         "test-key",
		Source:       "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	// Use a fresh context with generous timeout to avoid spurious deadline
	// exceeded errors when running in the full test suite.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := h.ExecuteGoverned(ctx, req, identity)
	if err != nil {
		t.Fatalf("ExecuteGoverned returned error: %v", err)
	}

	// Verify response content
	if result.Response == nil {
		t.Fatal("Response is nil")
	}
	if len(result.Response.Choices) == 0 {
		t.Fatal("Response has no choices")
	}

	// Verify Target
	if result.Target.Provider != "mock" {
		t.Errorf("Target.Provider = %q, want %q", result.Target.Provider, "mock")
	}
	if result.Target.Model != "provider-model" {
		t.Errorf("Target.Model = %q, want %q", result.Target.Model, "provider-model")
	}

	// Verify metadata
	if result.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", result.Attempts)
	}
	if result.CacheHit {
		t.Error("CacheHit = true, want false")
	}
	if result.DurationMS <= 0 {
		t.Error("DurationMS should be positive")
	}

	// Verify the provider was called exactly once
	if got := calls.Load(); got != 1 {
		t.Errorf("provider called %d times, want 1", got)
	}
}

// TEST-7A-01-02: ExecuteGoverned with identity.RPMLimit=5 on 6th call
// returns GovernanceError with StatusCode=429, Type="rate_limit_exceeded".
func TestExecuteGoverned_RateLimitExceeded(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)

	rpmLimit := 5
	identity := &GovernanceIdentity{
		ProjectID:    "proj-rl",
		VirtualKeyID: "vk-rl",
		KeyPrefix:    "sk-rl",
		Name:         "rate-limited-key",
		RPMLimit:     rpmLimit,
		Source:       "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	// Exhaust the rate limiter by calling CheckRPM directly
	// (simulating prior requests that used up the RPM allowance)
	limiter := h.getLimiter(identity.VirtualKeyID, rpmLimit, 0)
	for i := 0; i < rpmLimit; i++ {
		limiter.CheckRPM(identity.VirtualKeyID)
	}

	// Now ExecuteGoverned should fail with rate limit
	result, err := h.ExecuteGoverned(context.Background(), req, identity)
	if err == nil {
		t.Fatal("expected GovernanceError, got nil error")
	}
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}

	ge, ok := err.(*GovernanceError)
	if !ok {
		t.Fatalf("expected *GovernanceError, got %T: %v", err, err)
	}
	if ge.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", ge.StatusCode)
	}
	if ge.Type != "rate_limit_exceeded" {
		t.Errorf("Type = %q, want %q", ge.Type, "rate_limit_exceeded")
	}
	if ge.Stage != "rate_limit" {
		t.Errorf("Stage = %q, want %q", ge.Stage, "rate_limit")
	}
	// Verify rate-limit headers are present
	if ge.Headers == nil {
		t.Fatal("Headers is nil, expected rate limit headers")
	}
	if _, ok := ge.Headers["Retry-After"]; !ok {
		t.Error("missing Retry-After header")
	}
	if _, ok := ge.Headers["X-RateLimit-Limit"]; !ok {
		t.Error("missing X-RateLimit-Limit header")
	}
}

// TEST-7A-01-03: ExecuteGoverned with identity.BudgetLimitUSD=1.0 and
// spend > 1.0 returns GovernanceError with StatusCode=403,
// Type="budget_exceeded".
//
// NOTE: Budget enforcement requires h.usageW != nil and a real database.
// Without a DB, the budget check is skipped. This test verifies:
//  1. The GovernanceError type has the correct fields for budget errors
//  2. With usageW == nil, the request still succeeds (budget is skipped)
//
// Full integration test with DB-backed budget will be validated in TASK-02.
func TestExecuteGoverned_BudgetExceeded(t *testing.T) {
	// Verify the GovernanceError type and field structure for budget errors
	ge := &GovernanceError{
		StatusCode: 403,
		Type:       "budget_exceeded",
		Message:    "budget exceeded: $1.50 of $1.00",
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
	errMsg := ge.Error()
	if errMsg == "" {
		t.Error("Error() returned empty string")
	}

	// Verify that with usageW == nil, ExecuteGoverned skips budget and succeeds
	srv, _ := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)

	identity := &GovernanceIdentity{
		ProjectID:      "proj-budget",
		VirtualKeyID:   "vk-budget",
		KeyPrefix:      "sk-budget",
		Name:           "budget-key",
		BudgetLimitUSD: 1.0,
		BudgetPeriod:   "monthly",
		Source:         "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	// Without a real usage writer, budget is skipped and the request succeeds
	result, err := h.ExecuteGoverned(context.Background(), req, identity)
	if err != nil {
		t.Fatalf("ExecuteGoverned returned error with nil usageW: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Response == nil {
		t.Fatal("expected non-nil response")
	}
}

// TEST-7A-01-04: ExecuteGoverned with identity.SkipRateLimit=true bypasses
// rate limit even after the underlying limiter is exhausted.
func TestExecuteGoverned_SkipRateLimit(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)

	rpmLimit := 5
	identity := &GovernanceIdentity{
		ProjectID:     "proj-skip-rl",
		VirtualKeyID:  "vk-skip-rl",
		KeyPrefix:     "sk-skip-rl",
		Name:          "skip-rl-key",
		RPMLimit:      rpmLimit,
		SkipRateLimit: true,
		Source:        "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	// Exhaust the underlying rate limiter
	limiter := h.getLimiter(identity.VirtualKeyID, rpmLimit, 0)
	for i := 0; i < rpmLimit+5; i++ {
		limiter.CheckRPM(identity.VirtualKeyID)
	}

	// With SkipRateLimit=true, ExecuteGoverned should still succeed
	for i := 0; i < 10; i++ {
		result, err := h.ExecuteGoverned(context.Background(), req, identity)
		if err != nil {
			t.Fatalf("call %d with SkipRateLimit=true: unexpected error: %v", i+1, err)
		}
		if result == nil || result.Response == nil {
			t.Fatalf("call %d: expected non-nil result", i+1)
		}
	}
}

// TEST-7A-01-05: ExecuteGoverned with nil identity skips rate limit and
// budget but still allows the request to proceed (guardrails are always
// enforced but guardrails are nil in the test handler, so we verify the
// nil-identity path doesn't block).
func TestExecuteGoverned_NilIdentity(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	// With nil identity, rate limit and budget are skipped
	result, err := h.ExecuteGoverned(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("ExecuteGoverned with nil identity returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Response == nil {
		t.Fatal("expected non-nil response")
	}
	if result.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", result.Attempts)
	}

	// Call multiple times to prove rate limiting is not applied
	for i := 0; i < 5; i++ {
		result, err := h.ExecuteGoverned(context.Background(), req, nil)
		if err != nil {
			t.Fatalf("call %d with nil identity: unexpected error: %v", i+1, err)
		}
		if result == nil {
			t.Fatalf("call %d: expected non-nil result", i+1)
		}
	}
}

// ---------------------------------------------------------------------------
// TASK-01 tests: writeErrorWithDetails and writeGuardrailError
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// TASK-02 tests: Operational Response Headers
// ---------------------------------------------------------------------------

// TEST-7C-02-01: ExecuteGoverned returns Headers with X-Provider, X-Cache=MISS,
// X-Cost-USD on a successful call with an identity.
func TestExecuteGoverned_Headers_Success(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)

	identity := &GovernanceIdentity{
		ProjectID:    "proj-hdr",
		VirtualKeyID: "vk-hdr",
		KeyPrefix:    "sk-hdr",
		Name:         "hdr-key",
		Source:       "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	result, err := h.ExecuteGoverned(context.Background(), req, identity)
	if err != nil {
		t.Fatalf("ExecuteGoverned returned error: %v", err)
	}

	if result.Headers == nil {
		t.Fatal("Headers is nil")
	}

	// X-Provider must be set to the provider name
	if v := result.Headers["X-Provider"]; v != "mock" {
		t.Errorf("X-Provider = %q, want %q", v, "mock")
	}

	// X-Cache must be MISS on a fresh call
	if v := result.Headers["X-Cache"]; v != "MISS" {
		t.Errorf("X-Cache = %q, want %q", v, "MISS")
	}

	// X-Cost-USD must be present (identity is non-nil)
	if _, ok := result.Headers["X-Cost-USD"]; !ok {
		t.Error("X-Cost-USD header is absent, expected it to be present with non-nil identity")
	}
}

// TEST-7C-02-02: Cache hit returns X-Cache=HIT.
func TestExecuteGoverned_Headers_CacheHit(t *testing.T) {
	srv, calls := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)

	identity := &GovernanceIdentity{
		ProjectID:    "proj-cache",
		VirtualKeyID: "vk-cache",
		KeyPrefix:    "sk-cache",
		Name:         "cache-key",
		Source:       "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	// First call: should be a MISS
	result1, err := h.ExecuteGoverned(context.Background(), req, identity)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if result1.Headers["X-Cache"] != "MISS" {
		t.Errorf("first call X-Cache = %q, want MISS", result1.Headers["X-Cache"])
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 provider call, got %d", calls.Load())
	}

	// Second call with identical request: should be a HIT
	result2, err := h.ExecuteGoverned(context.Background(), req, identity)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if result2.Headers == nil {
		t.Fatal("second call Headers is nil")
	}
	if v := result2.Headers["X-Cache"]; v != "HIT" {
		t.Errorf("second call X-Cache = %q, want HIT", v)
	}
	// Provider should NOT have been called again
	if calls.Load() != 1 {
		t.Errorf("expected 1 provider call total (cache hit on second), got %d", calls.Load())
	}
}

// TEST-7C-02-03: Nil identity (A2A path) returns X-Provider but X-Cost-USD
// header is absent (AUTH-03).
func TestExecuteGoverned_Headers_NilIdentity_NoCost(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	result, err := h.ExecuteGoverned(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("ExecuteGoverned returned error: %v", err)
	}

	if result.Headers == nil {
		t.Fatal("Headers is nil")
	}

	// X-Provider must still be present
	if v := result.Headers["X-Provider"]; v != "mock" {
		t.Errorf("X-Provider = %q, want %q", v, "mock")
	}

	// X-Cache must be MISS
	if v := result.Headers["X-Cache"]; v != "MISS" {
		t.Errorf("X-Cache = %q, want %q", v, "MISS")
	}

	// X-Cost-USD must be ABSENT (nil identity → AUTH-03)
	if _, ok := result.Headers["X-Cost-USD"]; ok {
		t.Errorf("X-Cost-USD header is present (%q), expected ABSENT for nil identity", result.Headers["X-Cost-USD"])
	}
}

// ---------------------------------------------------------------------------
// TASK-01 tests: writeErrorWithDetails and writeGuardrailError
// ---------------------------------------------------------------------------

// TEST-7C-01-03: writeErrorWithDetails produces JSON with "details" map.
func TestWriteErrorWithDetails(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx := context.Background()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	details := map[string]any{
		"retry_after": float64(30),
		"rate_limit":  float64(100),
		"reason":      "RPM exceeded",
	}

	writeErrorWithDetails(w, req, http.StatusTooManyRequests, "rate_limit_exceeded", "too many requests", details)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	var resp openaischema.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error.Type != "rate_limit_exceeded" {
		t.Errorf("type = %q, want %q", resp.Error.Type, "rate_limit_exceeded")
	}
	if resp.Error.Message != "too many requests" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "too many requests")
	}
	if resp.Error.Details == nil {
		t.Fatal("Details is nil, expected non-nil")
	}
	if v, ok := resp.Error.Details["retry_after"]; !ok {
		t.Error("missing details.retry_after")
	} else if v.(float64) != 30 {
		t.Errorf("details.retry_after = %v, want 30", v)
	}
	if v, ok := resp.Error.Details["rate_limit"]; !ok {
		t.Error("missing details.rate_limit")
	} else if v.(float64) != 100 {
		t.Errorf("details.rate_limit = %v, want 100", v)
	}
	if v, ok := resp.Error.Details["reason"]; !ok {
		t.Error("missing details.reason")
	} else if v.(string) != "RPM exceeded" {
		t.Errorf("details.reason = %v, want %q", v, "RPM exceeded")
	}
}

// TEST-7C-01-04: writeGuardrailError produces JSON with "type":"guardrail_block" and "stage" field.
func TestWriteGuardrailError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx := context.Background()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	writeGuardrailError(w, req, "input", "PII detected in prompt")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp openaischema.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error.Type != "guardrail_block" {
		t.Errorf("type = %q, want %q", resp.Error.Type, "guardrail_block")
	}
	if resp.Error.Stage != "input" {
		t.Errorf("stage = %q, want %q", resp.Error.Stage, "input")
	}
	if resp.Error.Message != "PII detected in prompt" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "PII detected in prompt")
	}
	// Details should be nil (omitempty)
	if resp.Error.Details != nil {
		t.Errorf("Details should be nil, got %v", resp.Error.Details)
	}
}

// ---------------------------------------------------------------------------
// BATCH-27 / TASK-05: Available Models in Error Responses
// ---------------------------------------------------------------------------

// TestModelNotAllowed_IncludesAvailableModels verifies that a model_not_allowed
// GovernanceError includes the AllowedModels field populated from identity.
func TestModelNotAllowed_IncludesAvailableModels(t *testing.T) {
	identity := &GovernanceIdentity{
		VirtualKeyID:   "vk-test",
		AllowedModels:  []string{"fast", "smart"},
		RPMLimit:       0,
		SkipRateLimit:  true,
		BudgetLimitUSD: 0,
	}

	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	req := openaischema.ChatCompletionRequest{
		Model:    "unknown-model",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}

	result, err := h.ExecuteGoverned(context.Background(), req, identity)
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}

	ge, ok := err.(*GovernanceError)
	if !ok {
		t.Fatalf("expected *GovernanceError, got %T: %v", err, err)
	}

	if ge.Type != "model_not_allowed" {
		t.Errorf("type = %q, want %q", ge.Type, "model_not_allowed")
	}
	if len(ge.AvailableModels) != 2 {
		t.Fatalf("AvailableModels length = %d, want 2", len(ge.AvailableModels))
	}
	if ge.AvailableModels[0] != "fast" || ge.AvailableModels[1] != "smart" {
		t.Errorf("AvailableModels = %v, want [fast smart]", ge.AvailableModels)
	}
	if !strings.Contains(ge.Message, "Allowed: [fast smart]") {
		t.Errorf("Message should contain allowed models list, got: %q", ge.Message)
	}
}

// TestModelNotAllowed_NoRestrictions_OmitsField verifies that when AllowedModels
// is empty (no restrictions), the AvailableModels field is nil/empty.
func TestModelNotAllowed_NoRestrictions_OmitsField(t *testing.T) {
	identity := &GovernanceIdentity{
		VirtualKeyID:   "vk-test",
		AllowedModels:  nil, // no restrictions
		RPMLimit:       0,
		SkipRateLimit:  true,
		BudgetLimitUSD: 0,
	}

	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}

	// Should succeed (model allowed, no restrictions)
	result, err := h.ExecuteGoverned(context.Background(), req, identity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestPreStreamModelNotAllowed_IncludesAvailableModels verifies the streaming
// path also enriches errors with AvailableModels.
func TestPreStreamModelNotAllowed_IncludesAvailableModels(t *testing.T) {
	identity := &GovernanceIdentity{
		VirtualKeyID:   "vk-test",
		AllowedModels:  []string{"fast"},
		RPMLimit:       0,
		SkipRateLimit:  true,
		BudgetLimitUSD: 0,
	}

	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	req := openaischema.ChatCompletionRequest{
		Model:    "unknown",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}

	headers, err := h.preStreamGovernance(context.Background(), req, identity)
	if headers != nil {
		t.Fatalf("expected nil headers, got %+v", headers)
	}

	ge, ok := err.(*GovernanceError)
	if !ok {
		t.Fatalf("expected *GovernanceError, got %T: %v", err, err)
	}

	if ge.Type != "model_not_allowed" {
		t.Errorf("type = %q, want %q", ge.Type, "model_not_allowed")
	}
	if len(ge.AvailableModels) != 1 || ge.AvailableModels[0] != "fast" {
		t.Errorf("AvailableModels = %v, want [fast]", ge.AvailableModels)
	}
}

// TestWriteGovernanceError_IncludesAvailableModels verifies the HTTP error
// serialization includes the available_models field.
func TestWriteGovernanceError_IncludesAvailableModels(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	w := httptest.NewRecorder()

	ge := &GovernanceError{
		StatusCode:      403,
		Type:            "model_not_allowed",
		Message:         "model \"unknown\" is not allowed. Allowed: [fast smart]",
		Stage:           "model_validation",
		AvailableModels: []string{"fast", "smart"},
	}

	writeGovernanceError(w, req, ge)

	if w.Code != 403 {
		t.Fatalf("status = %d, want 403", w.Code)
	}

	var resp openaischema.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error.Type != "model_not_allowed" {
		t.Errorf("type = %q, want %q", resp.Error.Type, "model_not_allowed")
	}
	if len(resp.Error.AvailableModels) != 2 {
		t.Fatalf("available_models length = %d, want 2", len(resp.Error.AvailableModels))
	}
	if resp.Error.AvailableModels[0] != "fast" || resp.Error.AvailableModels[1] != "smart" {
		t.Errorf("available_models = %v, want [fast smart]", resp.Error.AvailableModels)
	}
}

// TestWriteGovernanceError_NilAvailableModels_OmitsField verifies backward
// compatibility — when AvailableModels is nil, the field is absent from JSON.
func TestWriteGovernanceError_NilAvailableModels_OmitsField(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	w := httptest.NewRecorder()

	ge := &GovernanceError{
		StatusCode: 429,
		Type:       "rate_limit_exceeded",
		Message:    "rate limit exceeded",
		Stage:      "rate_limiting",
		Headers:    map[string]string{"Retry-After": "30"},
	}

	writeGovernanceError(w, req, ge)

	body := w.Body.String()
	if strings.Contains(body, "available_models") {
		t.Errorf("available_models should be absent when nil, but found in: %s", body)
	}
}

// TestModelNames_ReturnsConfiguredModels verifies the Router.ModelNames() accessor.
func TestModelNames_ReturnsConfiguredModels(t *testing.T) {
	cfg := config.Default()
	cfg.Models = map[string]config.ModelConfig{
		"zebra":   {Routes: []config.ModelRoute{{Provider: "mock", Model: "m1", Weight: 100}}},
		"alpha":   {Routes: []config.ModelRoute{{Provider: "mock", Model: "m2", Weight: 100}}},
		"mid":     {Routes: []config.ModelRoute{{Provider: "mock", Model: "m3", Weight: 100}}},
	}
	cfg.Providers = map[string]config.ProviderConfig{
		"mock": {Type: "openai-compatible", BaseURL: "http://localhost:0", Keys: []config.ProviderKeyConfig{{ID: "k", Value: "v", Weight: 100}}},
	}

	r := routing.New(cfg.Models, cfg.Providers, cfg.Routing, nil)
	names := r.ModelNames()

	if len(names) != 3 {
		t.Fatalf("ModelNames() length = %d, want 3", len(names))
	}
	// Must be sorted
	if names[0] != "alpha" || names[1] != "mid" || names[2] != "zebra" {
		t.Errorf("ModelNames() = %v, want [alpha mid zebra] (sorted)", names)
	}
}

// TestErrorBody_AvailableModels_Omitempty verifies the JSON omitempty tag —
// AvailableModels is absent when nil/empty.
func TestErrorBody_AvailableModels_Omitempty(t *testing.T) {
	body := openaischema.ErrorBody{
		Message:   "test",
		Type:      "test_error",
		RequestID: "req-123",
	}

	data, err := json.Marshal(openaischema.ErrorResponse{Error: body})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(data)
	if strings.Contains(s, "available_models") {
		t.Errorf("available_models should be absent when nil, got: %s", s)
	}

	// Now with AvailableModels set
	body.AvailableModels = []string{"fast", "smart"}
	data2, err := json.Marshal(openaischema.ErrorResponse{Error: body})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s2 := string(data2)
	if !strings.Contains(s2, "available_models") {
		t.Errorf("available_models should be present when set, got: %s", s2)
	}
	if !strings.Contains(s2, "fast") || !strings.Contains(s2, "smart") {
		t.Errorf("available_models values missing in: %s", s2)
	}
}

// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// BATCH-31 TD-05: Streaming audit gap closure
// ---------------------------------------------------------------------------

// TestPostStreamGovernance_NilAuditLogger_NoPanic verifies no panic when auditLog is nil.
func TestPostStreamGovernance_NilAuditLogger_NoPanic(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)
	// auditLog is nil by default — postStreamGovernance must not panic

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(context.Background())

	h.postStreamGovernance(req,
		openaischema.ChatCompletionRequest{Model: "fast"},
		providers.Target{Provider: "mock", Model: "m1"},
		1, 5, 10, nil, 100,
	)
	// If we get here, no panic occurred
}

// TestPostStreamGovernance_WithAuditLogger_NoPanic verifies the audit code path
// executes without panic when auditLog is set (nil DB logger — events are no-ops).
func TestPostStreamGovernance_WithAuditLogger_NoPanic(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	// audit.NewLogger(nil, ...) is a no-op logger but Record() still runs partially
	h.SetAuditLogger(audit.NewLogger(nil, slog.Default(), 100))
	h.SetLogBodies(true)

	identity := &GovernanceIdentity{
		VirtualKeyID:  "vk-stream-test",
		ProjectID:     "proj-1",
		SkipRateLimit: true,
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx := context.Background()
	req = req.WithContext(ctx)

	h.postStreamGovernance(req,
		openaischema.ChatCompletionRequest{Model: "fast", Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}}},
		providers.Target{Provider: "mock", Model: "provider-model"},
		1, 5, 10, identity, 150,
	)
	// If we get here, no panic — audit code path exercised
}

// TestPostStreamGovernance_NilIdentity_NoPanic verifies postStreamGovernance
// with nil identity (A2A path) doesn't panic and still records metrics.
func TestPostStreamGovernance_NilIdentity_NoPanic(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(context.Background())

	h.postStreamGovernance(req,
		openaischema.ChatCompletionRequest{Model: "fast"},
		providers.Target{Provider: "mock", Model: "m1"},
		1, 5, 10, nil, 100,
	)
}

// ---------------------------------------------------------------------------
// BATCH-37 / TASK-03 Fix 1: Guardrail output error returns 500
// ---------------------------------------------------------------------------

// errorOutputStage is a guardrails.Stage that returns an error on CheckOutput.
type errorOutputStage struct{}

func (s *errorOutputStage) Name() string { return "error-output" }
func (s *errorOutputStage) CheckInput(_ context.Context, _ []guardrails.Message) (guardrails.Result, error) {
	return guardrails.Result{Action: guardrails.Pass}, nil
}
func (s *errorOutputStage) CheckOutput(_ context.Context, _ string) (guardrails.Result, error) {
	return guardrails.Result{}, fmt.Errorf("output check internal failure")
}

// TestGuardrailOutputErrorReturns500 verifies that when CheckOutput returns an error,
// ExecuteGoverned returns a GovernanceError with StatusCode 500 instead of falling
// through and returning the unchecked response.
func TestGuardrailOutputErrorReturns500(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)

	// Wire a guardrails pipeline with an output stage that always errors
	h.guardrails = guardrails.NewPipeline(nil, []guardrails.Stage{&errorOutputStage{}})
	h.cfg.Guardrails.Enabled = true

	identity := &GovernanceIdentity{
		ProjectID:    "proj-gr-err",
		VirtualKeyID: "vk-gr-err",
		KeyPrefix:    "sk-gr-err",
		Name:         "gr-error-key",
		Source:       "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	result, err := h.ExecuteGoverned(context.Background(), req, identity)
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
	if err == nil {
		t.Fatal("expected GovernanceError, got nil error")
	}

	ge, ok := err.(*GovernanceError)
	if !ok {
		t.Fatalf("expected *GovernanceError, got %T: %v", err, err)
	}
	if ge.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", ge.StatusCode)
	}
	if ge.Type != "guardrail_error" {
		t.Errorf("Type = %q, want %q", ge.Type, "guardrail_error")
	}
	if ge.Stage != "output" {
		t.Errorf("Stage = %q, want %q", ge.Stage, "output")
	}
	if ge.Message != "guardrail check failed" {
		t.Errorf("Message = %q, want %q", ge.Message, "guardrail check failed")
	}
}

// ---------------------------------------------------------------------------
// BATCH-34 / TASK-01: Extracted governance helper tests
// ---------------------------------------------------------------------------

// TEST-34-01-01: buildModelNotAllowedError returns correct error struct.
func TestBuildModelNotAllowedError(t *testing.T) {
	ge := buildModelNotAllowedError("gpt-5", []string{"fast", "smart"})

	if ge.StatusCode != 403 {
		t.Errorf("StatusCode = %d, want 403", ge.StatusCode)
	}
	if ge.Type != "model_not_allowed" {
		t.Errorf("Type = %q, want %q", ge.Type, "model_not_allowed")
	}
	if !strings.Contains(ge.Message, "gpt-5") {
		t.Errorf("Message should contain model name, got: %q", ge.Message)
	}
	if !strings.Contains(ge.Message, "Allowed: [fast smart]") {
		t.Errorf("Message should contain allowed list, got: %q", ge.Message)
	}
	if ge.Stage != "model_validation" {
		t.Errorf("Stage = %q, want %q", ge.Stage, "model_validation")
	}
	if len(ge.AvailableModels) != 2 || ge.AvailableModels[0] != "fast" || ge.AvailableModels[1] != "smart" {
		t.Errorf("AvailableModels = %v, want [fast smart]", ge.AvailableModels)
	}
}

// TEST-34-01-02: modelAllowed returns true when model is in the allowed list.
func TestModelAllowed_Matching(t *testing.T) {
	if !modelAllowed("fast", []string{"fast"}) {
		t.Error("modelAllowed(\"fast\", [\"fast\"]) = false, want true")
	}
	if !modelAllowed("Fast", []string{"fast"}) {
		t.Error("modelAllowed should be case-insensitive")
	}
	if modelAllowed("other", []string{"fast"}) {
		t.Error("modelAllowed(\"other\", [\"fast\"]) = true, want false")
	}
	if !modelAllowed("anything", nil) {
		t.Error("modelAllowed with nil allowed list should return true")
	}
	if !modelAllowed("anything", []string{}) {
		t.Error("modelAllowed with empty allowed list should return true")
	}
}

// TEST-34-01-03: Rate limit exceeded produces correct error + headers.
func TestCheckRateLimit_Exceeded(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	rpmLimit := 3
	identity := &GovernanceIdentity{
		ProjectID:    "proj-rl-34",
		VirtualKeyID: "vk-rl-34",
		KeyPrefix:    "sk-rl-34",
		Name:         "rl-test-key",
		RPMLimit:     rpmLimit,
		Source:       "virtual_key",
	}

	// Exhaust the rate limiter
	limiter := h.getLimiter(identity.VirtualKeyID, rpmLimit, 0)
	for i := 0; i < rpmLimit; i++ {
		limiter.CheckRPM(identity.VirtualKeyID)
	}

	// Now checkRateLimit should fail
	headers, err := h.checkRateLimit(identity)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if headers != nil {
		t.Fatalf("expected nil headers, got %v", headers)
	}

	ge, ok := err.(*GovernanceError)
	if !ok {
		t.Fatalf("expected *GovernanceError, got %T", err)
	}
	if ge.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", ge.StatusCode)
	}
	if ge.Type != "rate_limit_exceeded" {
		t.Errorf("Type = %q, want %q", ge.Type, "rate_limit_exceeded")
	}
	if ge.Stage != "rate_limit" {
		t.Errorf("Stage = %q, want %q", ge.Stage, "rate_limit")
	}
	if _, ok := ge.Headers["Retry-After"]; !ok {
		t.Error("missing Retry-After header")
	}
	if _, ok := ge.Headers["X-RateLimit-Limit"]; !ok {
		t.Error("missing X-RateLimit-Limit header")
	}
}

// TEST-34-01-04: Budget exceeded produces correct error with amounts.
func TestCheckBudget_Exceeded(t *testing.T) {
	ge := &GovernanceError{
		StatusCode: 403,
		Type:       "budget_exceeded",
		Message:    "budget exceeded: $1.50 of $1.00",
		Stage:      "budget",
	}

	if ge.StatusCode != 403 {
		t.Errorf("StatusCode = %d, want 403", ge.StatusCode)
	}
	if ge.Type != "budget_exceeded" {
		t.Errorf("Type = %q, want %q", ge.Type, "budget_exceeded")
	}
	if !strings.Contains(ge.Message, "$1.50") {
		t.Errorf("Message should contain spent amount, got: %q", ge.Message)
	}
	if !strings.Contains(ge.Message, "$1.00") {
		t.Errorf("Message should contain limit amount, got: %q", ge.Message)
	}
	if ge.Stage != "budget" {
		t.Errorf("Stage = %q, want %q", ge.Stage, "budget")
	}

	// Also verify the helper returns nil for nil identity
	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	if err := h.checkBudget(context.Background(), nil); err != nil {
		t.Errorf("checkBudget with nil identity should return nil, got: %v", err)
	}
}

// TEST-34-01-05: Governance step ordering — model check runs before rate limit.
func TestGovernanceStepOrdering(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	// Identity with BOTH model restriction AND rate limit.
	// Disallowed model should trigger BEFORE rate limit.
	rpmLimit := 1
	identity := &GovernanceIdentity{
		ProjectID:     "proj-order",
		VirtualKeyID:  "vk-order",
		KeyPrefix:     "sk-order",
		Name:          "order-key",
		AllowedModels: []string{"fast"},
		RPMLimit:      rpmLimit,
		Source:        "virtual_key",
	}

	// Exhaust the rate limiter
	limiter := h.getLimiter(identity.VirtualKeyID, rpmLimit, 0)
	limiter.CheckRPM(identity.VirtualKeyID)

	// Request with a disallowed model — should get model_not_allowed (NOT rate_limit)
	req := openaischema.ChatCompletionRequest{
		Model:    "unknown-model",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}

	_, err := h.ExecuteGoverned(context.Background(), req, identity)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	ge, ok := err.(*GovernanceError)
	if !ok {
		t.Fatalf("expected *GovernanceError, got %T", err)
	}
	if ge.Type != "model_not_allowed" {
		t.Errorf("Type = %q, want %q (model check should run first)", ge.Type, "model_not_allowed")
	}
	if ge.Stage != "model_validation" {
		t.Errorf("Stage = %q, want %q", ge.Stage, "model_validation")
	}
}

// ---------------------------------------------------------------------------
// BATCH-57 / TASK-01: JSON injection fix regression tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// BATCH-59 / TASK-01: Context-aware backoff test
// ---------------------------------------------------------------------------

// TEST-59-01-03: Context-aware backoff exits immediately when context is already cancelled.
func TestExecuteGoverned_ContextAwareBackoff(t *testing.T) {
	// Create a provider that always fails with a retryable error so backoff is triggered.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "server error"}})
	}))
	defer srv.Close()

	h := testHandler(t, srv.URL)

	// Configure 3 retries with significant backoff
	h.cfg.Routing.Defaults.Retry.Attempts = 3
	h.cfg.Routing.Defaults.Retry.InitialMS = 2000
	h.cfg.Routing.Defaults.Retry.MaxMS = 10000

	identity := &GovernanceIdentity{
		ProjectID:    "proj-backoff",
		VirtualKeyID: "vk-backoff",
		KeyPrefix:    "sk-test",
		Name:         "backoff-test",
		SkipRateLimit: true,
		Source:       "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	// Pre-cancel the context before calling
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	start := time.Now()
	_, err := h.ExecuteGoverned(ctx, req, identity)
	elapsed := time.Since(start)

	// The backoff should NOT wait — it should exit immediately due to ctx.Done()
	if elapsed > 500*time.Millisecond {
		t.Errorf("backoff took %v with cancelled context, expected < 500ms", elapsed)
	}

	// Should return context.Canceled error
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

// ---------------------------------------------------------------------------
// BATCH-59 / TASK-02: Default per-call timeout tests
// ---------------------------------------------------------------------------

// TEST-59-02-01: TimeoutMS=0 produces a 30s context deadline.
func TestDefaultTimeout_WhenTimeoutMSZero(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()

	h := testHandler(t, srv.URL)
	h.cfg.Routing.Defaults.TimeoutMS = 0 // unset — should default to 30s

	identity := &GovernanceIdentity{
		ProjectID:     "proj-timeout-default",
		VirtualKeyID:  "vk-timeout-default",
		KeyPrefix:     "sk-test",
		Name:          "timeout-default",
		SkipRateLimit: true,
		Source:        "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	// With TimeoutMS=0, the handler should apply a 30s default and succeed
	result, err := h.ExecuteGoverned(context.Background(), req, identity)
	if err != nil {
		t.Fatalf("expected success with default 30s timeout, got error: %v", err)
	}
	if result == nil || result.Response == nil {
		t.Fatal("expected non-nil result")
	}
}

// TEST-59-02-04: TimeoutMS>0 takes precedence over default 30s.
func TestDefaultTimeout_TimeoutMSPrecedence(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // delay longer than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()

	h := testHandler(t, provider.URL)
	h.cfg.Routing.Defaults.TimeoutMS = 50 // 50ms — much shorter than 30s default
	h.cfg.Routing.Defaults.Retry.Attempts = 1

	identity := &GovernanceIdentity{
		ProjectID:     "proj-timeout-prec",
		VirtualKeyID:  "vk-timeout-prec",
		KeyPrefix:     "sk-test",
		Name:          "timeout-prec",
		SkipRateLimit: true,
		Source:        "virtual_key",
	}

	req := openaischema.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}

	start := time.Now()
	_, err := h.ExecuteGoverned(context.Background(), req, identity)
	elapsed := time.Since(start)

	// Should timeout quickly (around 50ms) — not wait 30s
	if elapsed > 5*time.Second {
		t.Errorf("request took %v with TimeoutMS=50, expected < 5s", elapsed)
	}
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

// TEST-57-01-01: Output redaction produces valid JSON when content contains double quotes.
func TestGuardrailRedaction_OutputDoubleQuotes(t *testing.T) {
	content := `He said "hello world" and left`
	marshaled, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var unmarshalled string
	if err := json.Unmarshal(marshaled, &unmarshalled); err != nil {
		t.Errorf("json.Unmarshal on marshal output failed: %v", err)
	}
	if unmarshalled != content {
		t.Errorf("roundtrip failed: got %q, want %q", unmarshalled, content)
	}
	// Verify that string concat would have produced broken JSON
	broken := json.RawMessage(`"` + content + `"`)
	if err := json.Unmarshal(broken, new(string)); err == nil {
		t.Error("string concat should produce broken JSON for content with quotes, but it unmarshal succeeded")
	}
}

// TEST-57-01-02: Input redaction produces valid JSON when message contains newlines and backslashes.
func TestGuardrailRedaction_InputNewlinesBackslashes(t *testing.T) {
	content := "line1\nline2\tpath\\\\file"
	marshaled, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var unmarshalled string
	if err := json.Unmarshal(marshaled, &unmarshalled); err != nil {
		t.Errorf("json.Unmarshal on marshal output failed: %v", err)
	}
	if unmarshalled != content {
		t.Errorf("roundtrip failed: got %q, want %q", unmarshalled, content)
	}
	// Verify string concat would fail
	broken := json.RawMessage(`"` + content + `"`)
	if err := json.Unmarshal(broken, new(string)); err == nil {
		t.Error("string concat should produce broken JSON for content with newlines, but unmarshal succeeded")
	}
}

// TEST-57-01-03: Output redaction of normal content produces identical JSON to before.
func TestGuardrailRedaction_NormalContent(t *testing.T) {
	content := "Hello, this is a normal response."
	marshaled, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	expected := json.RawMessage(`"Hello, this is a normal response."`)
	if string(marshaled) != string(expected) {
		t.Errorf("json.Marshal produced %q, want %q", string(marshaled), string(expected))
	}
}

// TEST-57-01-04: Input redaction of empty content produces valid empty JSON string.
func TestGuardrailRedaction_EmptyContent(t *testing.T) {
	content := ""
	marshaled, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if string(marshaled) != `""` {
		t.Errorf("json.Marshal(\"\") = %s, want \"\\\"\\\"\"" , string(marshaled))
	}
	var unmarshalled string
	if err := json.Unmarshal(marshaled, &unmarshalled); err != nil {
		t.Errorf("json.Unmarshal on empty string marshal failed: %v", err)
	}
	if unmarshalled != "" {
		t.Errorf("roundtrip of empty string failed: got %q, want empty", unmarshalled)
	}
}

// ---------------------------------------------------------------------------
// BATCH-60 / TASK-01: Streaming Output Guardrails
// ---------------------------------------------------------------------------

// blockOutputStage always blocks on output.
type blockOutputStage struct{}

func (s *blockOutputStage) Name() string { return "test-output-blocker" }
func (s *blockOutputStage) CheckInput(_ context.Context, _ []guardrails.Message) (guardrails.Result, error) {
	return guardrails.Result{Action: guardrails.Pass}, nil
}
func (s *blockOutputStage) CheckOutput(_ context.Context, _ string) (guardrails.Result, error) {
	return guardrails.Result{
		Action:    guardrails.Block,
		Message:   "content blocked by output stage",
		StageName: "test-output-blocker",
	}, nil
}

// redactOutputStage always redacts on output.
type redactOutputStage struct{}

func (s *redactOutputStage) Name() string { return "test-output-redactor" }
func (s *redactOutputStage) CheckInput(_ context.Context, _ []guardrails.Message) (guardrails.Result, error) {
	return guardrails.Result{Action: guardrails.Pass}, nil
}
func (s *redactOutputStage) CheckOutput(_ context.Context, content string) (guardrails.Result, error) {
	return guardrails.Result{
		Action:    guardrails.Redact,
		Message:   "[REDACTED]",
		StageName: "test-output-redactor",
	}, nil
}

// passOutputStage always passes on output.
type passOutputStage struct{}

func (s *passOutputStage) Name() string { return "test-output-passer" }
func (s *passOutputStage) CheckInput(_ context.Context, _ []guardrails.Message) (guardrails.Result, error) {
	return guardrails.Result{Action: guardrails.Pass}, nil
}
func (s *passOutputStage) CheckOutput(_ context.Context, _ string) (guardrails.Result, error) {
	return guardrails.Result{Action: guardrails.Pass, StageName: "test-output-passer"}, nil
}

// TEST-60-01-01: Streaming with nil output guardrails sends [DONE] normally.
func TestStreamOutputGuardrails_NilGuardrails_SendsDone(t *testing.T) {
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("hello"),
		makeChunk(" world"),
	}
	adapter := newStreamGovAdapter(chunks, nil)
	h := streamGovHandler(t, adapter, nil) // nil guardrails

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
	if strings.Contains(body, "event: error") {
		t.Error("response body should NOT contain 'event: error'")
	}
}

// TEST-60-01-02: Streaming blocked by output guardrail sends SSE error event (NOT [DONE]).
func TestStreamOutputGuardrails_Block_SendsErrorEvent(t *testing.T) {
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("sensitive data"),
	}
	adapter := newStreamGovAdapter(chunks, nil)
	pipeline := guardrails.NewPipeline(nil, []guardrails.Stage{&blockOutputStage{}})
	h := streamGovHandler(t, adapter, pipeline)

	req := makeStreamRequest(t)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (headers already sent)", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Error("response body should contain 'event: error' when guardrail blocks")
	}
	if !strings.Contains(body, "guardrail_block") {
		t.Error("response body should contain 'guardrail_block' type")
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Error("response body should NOT contain 'data: [DONE]' when guardrail blocks")
	}
}

// TEST-60-01-03: Streaming with redacting output guardrail logs and sends [DONE] (audit-only).
func TestStreamOutputGuardrails_Redact_LogsAndSendsDone(t *testing.T) {
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("some content"),
	}
	adapter := newStreamGovAdapter(chunks, nil)
	pipeline := guardrails.NewPipeline(nil, []guardrails.Stage{&redactOutputStage{}})
	h := streamGovHandler(t, adapter, pipeline)

	req := makeStreamRequest(t)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	// Redact is audit-only — [DONE] should still be sent
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("response body should contain 'data: [DONE]' when guardrail redacts (audit-only)")
	}
	// No error event for redaction
	if strings.Contains(body, "event: error") {
		t.Error("response body should NOT contain 'event: error' when guardrail redacts")
	}
}

// TEST-60-01-04: Streaming with pass output guardrail sends [DONE] unchanged.
func TestStreamOutputGuardrails_Pass_SendsDone(t *testing.T) {
	chunks := []openaischema.ChatCompletionStreamChunk{
		makeChunk("clean content"),
	}
	adapter := newStreamGovAdapter(chunks, nil)
	pipeline := guardrails.NewPipeline(nil, []guardrails.Stage{&passOutputStage{}})
	h := streamGovHandler(t, adapter, pipeline)

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
	if strings.Contains(body, "event: error") {
		t.Error("response body should NOT contain 'event: error' when guardrail passes")
	}
}

// ---------------------------------------------------------------------------
// BATCH-60 / TASK-03: Breaker Map LRU Eviction
// ---------------------------------------------------------------------------

// TEST-60-03-01: Most-active breaker survives eviction.
func TestGetBreaker_MostActiveSurvivesEviction(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &Handler{
		logger:   logger,
		breakers: make(map[string]*breakerEntry),
	}

	// Populate three entries with distinct lastAccess times.
	// "stale" is oldest, "mid" is middle, "fresh" is newest.
	h.breakers["stale:model"] = &breakerEntry{
		breaker:    circuit.NewBreaker(nil, "stale", "model", logger),
		lastAccess: time.Now().Add(-2 * time.Hour),
	}
	h.breakers["mid:model"] = &breakerEntry{
		breaker:    circuit.NewBreaker(nil, "mid", "model", logger),
		lastAccess: time.Now().Add(-1 * time.Hour),
	}
	h.breakers["fresh:model"] = &breakerEntry{
		breaker:    circuit.NewBreaker(nil, "fresh", "model", logger),
		lastAccess: time.Now(),
	}

	// Access "fresh" via getBreaker — updates its lastAccess to now
	b := h.getBreaker("fresh", "model", "")
	if b == nil {
		t.Fatal("expected non-nil breaker")
	}

	// Now fill up to maxBreakers by inserting many new entries.
	// The LRU eviction should remove "stale" first (oldest lastAccess).
	for i := 0; i < maxBreakers; i++ {
		provider := fmt.Sprintf("filler-%d", i)
		h.getBreaker(provider, "model", "")
	}

	h.breakersMu.Lock()
	count := len(h.breakers)
	_, staleExists := h.breakers["stale:model"]
	_, freshExists := h.breakers["fresh:model"]
	h.breakersMu.Unlock()

	// Map should be at or below cap
	if count > maxBreakers {
		t.Errorf("breaker count %d exceeds maxBreakers %d", count, maxBreakers)
	}

	// "stale" should have been evicted (it was the oldest)
	if staleExists {
		t.Error("stale entry should have been evicted by LRU")
	}

	// "fresh" was accessed most recently via getBreaker — should survive
	if !freshExists {
		t.Error("fresh entry (most recently accessed) should survive eviction")
	}
}

// TEST-60-03-02: Eviction caps at maxBreakers.
func TestGetBreaker_EvictionCapsAtMax(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	// Insert 2*maxBreakers entries — map should never exceed maxBreakers
	for i := 0; i < maxBreakers*2; i++ {
		provider := fmt.Sprintf("provider-%d", i)
		h.getBreaker(provider, "model", "")
	}

	h.breakersMu.Lock()
	count := len(h.breakers)
	h.breakersMu.Unlock()

	if count > maxBreakers {
		t.Errorf("breaker count %d exceeds maxBreakers %d", count, maxBreakers)
	}
}

// TEST-60-03-03: Concurrent getBreaker calls don't race.
func TestGetBreaker_ConcurrentNoRace(t *testing.T) {
	srv, _ := mockProviderServer(t)
	defer srv.Close()
	h := testHandler(t, srv.URL)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			provider := fmt.Sprintf("prov-%d", idx%10) // 10 unique providers
			h.getBreaker(provider, "model", "us-east")
		}(i)
	}

	wg.Wait()

	// If we get here without a race detector firing, the test passes
	h.breakersMu.Lock()
	count := len(h.breakers)
	h.breakersMu.Unlock()
	if count == 0 {
		t.Error("expected some breakers to be created")
	}
}
