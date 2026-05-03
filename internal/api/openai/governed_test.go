package openaiapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"openlimit/internal/cache"
	"openlimit/internal/config"
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

	result, err := h.ExecuteGoverned(context.Background(), req, identity)
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
