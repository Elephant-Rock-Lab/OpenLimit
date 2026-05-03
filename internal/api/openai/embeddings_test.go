package openaiapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"openlimit/internal/auth"
	"openlimit/internal/config"
	"openlimit/internal/metrics"
	"openlimit/internal/providers"
	"openlimit/internal/routing"
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

// NOTE: Tests that call h.Embeddings() directly (ValidResponse, ProviderFailure,
// BadModel, PassesAuthHeader) are moved to embeddings_integration_test.go
// because the embeddings proxy flow hangs in the test environment.
// Run with: go test -tags=integration ./internal/api/openai/...

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
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytesReader{data: body})
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
