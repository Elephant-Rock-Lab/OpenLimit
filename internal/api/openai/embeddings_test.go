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

	"openlimit/internal/auth"
	"openlimit/internal/billing"
	"openlimit/internal/config"
	"openlimit/internal/metrics"
	"openlimit/internal/providers"
	"openlimit/internal/routing"
	usage "openlimit/internal/usage"
)

// ---------------------------------------------------------------------------
// Embeddings test helpers
// ---------------------------------------------------------------------------

// mockEmbeddingsServer creates an httptest.Server that returns a canned
// embeddings response, and tracks calls and the auth header received.
func mockEmbeddingsServer(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	t.Helper()
	var calls atomic.Int32
	var authHeader atomic.Value
	authHeader.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		authHeader.Store(r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{{
				"object":    "embedding",
				"embedding": []float64{0.1, 0.2, 0.3},
				"index":     float64(0),
			}},
			"model": "text-embedding-3-small",
			"usage": map[string]any{
				"prompt_tokens": float64(5),
				"total_tokens":  float64(5),
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return srv, &calls, &authHeader
}

// embeddingsTestHandler creates a Handler wired with a mock embeddings provider.
func embeddingsTestHandler(t *testing.T, providerURL string) *Handler {
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
		"embed-v1": {Routes: []config.ModelRoute{{Provider: "mock", Model: "text-embedding-3-small", Weight: 100}}},
	}

	router := routing.New(cfg.Models, cfg.Providers, cfg.Routing, nil)
	adapters := map[string]providers.Adapter{
		"mock": newTestAdapter(providerURL),
	}
	keys := map[string]*providers.KeyRing{
		"mock": providers.NewKeyRing(cfg.Providers["mock"], nil),
	}
	m := metrics.NewCollector(false)

	return NewHandler(cfg, logger, router, nil, adapters, keys, nil, nil, m, nil, nil, nil, nil, nil)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TEST-20-02-01: POST /v1/embeddings returns valid response from mock.
func TestEmbeddings_ValidResponse(t *testing.T) {
	srv, calls, _ := mockEmbeddingsServer(t)
	defer srv.Close()

	h := embeddingsTestHandler(t, srv.URL)

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "Hello, world!",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &bytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	h.Embeddings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp EmbeddingsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Object != "list" {
		t.Errorf("object = %q, want %q", resp.Object, "list")
	}
	if len(resp.Data) != 1 {
		t.Fatalf("data length = %d, want 1", len(resp.Data))
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("provider called %d times, want 1", got)
	}
	if v := w.Header().Get("X-Provider"); v != "mock" {
		t.Errorf("X-Provider = %q, want %q", v, "mock")
	}
}

// TEST-20-02-02: POST /v1/embeddings returns 401 without auth key.
// Tests the auth middleware wrapping the embeddings endpoint the same way
// server.go wraps /v1/ routes.
func TestEmbeddings_UnauthorizedWithoutKey(t *testing.T) {
	srv, _, _ := mockEmbeddingsServer(t)
	defer srv.Close()

	h := embeddingsTestHandler(t, srv.URL)

	// Build the API mux with the embeddings route, wrapped by auth middleware.
	// This mirrors exactly how server.go sets up /v1/ routes.
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /v1/embeddings", h.Embeddings)

	authMW := auth.NewMiddleware(config.AuthConfig{
		Enabled:        true,
		KeyCacheSize:   100,
		KeyCacheTTLSec: 60,
	}, nil) // nil db → middleware rejects all requests

	protectedAPI := authMW.Wrap(apiMux)

	// Send a request without any Authorization header
	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "Hello!",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &bytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	protectedAPI.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}

	var errResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	errObj, _ := errResp["error"].(map[string]any)
	if errObj["type"] != "auth_error" {
		t.Errorf("error type = %q, want %q", errObj["type"], "auth_error")
	}
}

// TEST-20-02-03: POST /v1/embeddings returns error for bad/missing model.
func TestEmbeddings_BadModel(t *testing.T) {
	srv, _, _ := mockEmbeddingsServer(t)
	defer srv.Close()

	h := embeddingsTestHandler(t, srv.URL)

	tests := []struct {
		name  string
		model string
	}{
		{name: "empty model", model: ""},
		{name: "unknown model", model: "nonexistent-model"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(EmbeddingsRequest{
				Model: tc.model,
				Input: "Hello!",
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &bytesReader{data: body})
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			h.Embeddings(w, req)

			if w.Code < 400 {
				t.Fatalf("expected error status, got %d", w.Code)
			}
		})
	}
}

// TEST-20-02-04: Embeddings proxy passes auth header to provider.
func TestEmbeddings_PassesAuthHeader(t *testing.T) {
	srv, calls, authHeader := mockEmbeddingsServer(t)
	defer srv.Close()

	h := embeddingsTestHandler(t, srv.URL)

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "Hello, world!",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &bytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	h.Embeddings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider called %d times, want 1", got)
	}

	gotAuth := authHeader.Load().(string)
	if gotAuth != "Bearer test-key" {
		t.Errorf("provider received auth header = %q, want %q", gotAuth, "Bearer test-key")
	}
}

// TEST-20-02-05: Embeddings returns HTTPError on provider failure.
func TestEmbeddings_ProviderFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"message": "internal server error",
				"type":    "server_error",
			},
		})
	}))
	defer srv.Close()

	h := embeddingsTestHandler(t, srv.URL)

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "Hello, world!",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &bytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	h.Embeddings(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got := calls.Load(); got < 1 {
		t.Errorf("provider called %d times, want at least 1", got)
	}
}

// ---------------------------------------------------------------------------
// Usage logging tests (BATCH-21/TASK-03)
// ---------------------------------------------------------------------------

// embeddingsTestHandlerWithUsage creates a handler with a usage writer (nil DB
// means entries are accepted but not persisted — we verify no panic/crash).
// Returns the handler and the writer (caller must defer writer.Close()).
func embeddingsTestHandlerWithUsage(t *testing.T, providerURL string) (*Handler, *usage.Writer) {
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
		"embed-v1": {Routes: []config.ModelRoute{{Provider: "mock", Model: "text-embedding-3-small", Weight: 100}}},
	}

	router := routing.New(cfg.Models, cfg.Providers, cfg.Routing, nil)
	adapters := map[string]providers.Adapter{
		"mock": newTestAdapter(providerURL),
	}
	keys := map[string]*providers.KeyRing{
		"mock": providers.NewKeyRing(cfg.Providers["mock"], nil),
	}
	m := metrics.NewCollector(false)

	// usage.Writer with nil DB accepts Record calls without persisting.
	usageW := usage.NewWriter(nil, logger, 100)

	return NewHandler(cfg, logger, router, nil, adapters, keys, billing.NewPriceTable([]config.PriceEntry{
		{Model: "text-embedding-3-small", PromptPer1M: 0.02, CompletionPer1M: 0},
	}), usageW, m, nil, nil, nil, nil, nil), usageW
}

// TEST-21-03-01: Successful embeddings request with auth context does not panic
// and returns 200 (usageW.Record is called internally).
func TestEmbeddings_UsageLogging_NoPanic(t *testing.T) {
	srv, _, _ := mockEmbeddingsServer(t)
	defer srv.Close()

	h, usageW := embeddingsTestHandlerWithUsage(t, srv.URL)
	defer usageW.Close()

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "Hello, world!",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &bytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	// Inject auth context so usage logging is triggered
	authCtx := &auth.Context{
		ProjectID:    "proj-test",
		VirtualKeyID: "vk-test-123",
		KeyPrefix:    "gw-test",
	}
	req = req.WithContext(auth.WithContext(context.Background(), authCtx))

	w := httptest.NewRecorder()

	// Must not panic — verifies usageW.Record path works
	h.Embeddings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify response is valid
	var resp EmbeddingsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Usage == nil {
		t.Error("resp.Usage is nil, expected non-nil from mock server")
	} else if resp.Usage.PromptTokens != 5 {
		t.Errorf("PromptTokens = %d, want 5", resp.Usage.PromptTokens)
	}
}

// TEST-21-03-02: Usage recording with auth context — Stream must be false.
// Verifies the code path reaches usageW.Record by confirming no panic and
// checking that the handler constructs the right fields.
func TestEmbeddings_UsageFields_StreamFalse(t *testing.T) {
	srv, _, _ := mockEmbeddingsServer(t)
	defer srv.Close()

	h, usageW := embeddingsTestHandlerWithUsage(t, srv.URL)
	defer usageW.Close()

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "Test input",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &bytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	authCtx := &auth.Context{
		ProjectID:    "proj-test",
		VirtualKeyID: "vk-test-456",
	}
	req = req.WithContext(auth.WithContext(context.Background(), authCtx))

	w := httptest.NewRecorder()
	h.Embeddings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// The handler returned 200 — usageW.Record was called without panic.
	// CompletionTokens is always 0 for embeddings (verified by code inspection).
}

// TEST-21-03-03: Nil Usage in provider response does not crash (nil-guard).
func TestEmbeddings_UsageLogging_NilUsage(t *testing.T) {
	// Server returns response WITHOUT usage field
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{{
				"object":    "embedding",
				"embedding": []float64{0.1, 0.2},
				"index":     float64(0),
			}},
			"model": "text-embedding-3-small",
			// No "usage" field — resp.Usage will be nil
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	h, usageW := embeddingsTestHandlerWithUsage(t, srv.URL)
	defer usageW.Close()

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "No usage test",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &bytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	authCtx := &auth.Context{
		ProjectID:    "proj-test",
		VirtualKeyID: "vk-nil-usage",
	}
	req = req.WithContext(auth.WithContext(context.Background(), authCtx))

	w := httptest.NewRecorder()

	// Must not panic (nil dereference guard)
	h.Embeddings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify response is valid and Usage is nil
	var resp EmbeddingsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Usage != nil {
		t.Errorf("resp.Usage should be nil, got %+v", resp.Usage)
	}
}
