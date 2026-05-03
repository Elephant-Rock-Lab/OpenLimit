//go:build integration

package openaiapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// NOTE: These tests call h.Embeddings() directly. The embeddings proxy flow
// currently hangs when routing through executeEmbeddingsPlan in the test
// environment. They require -tags=integration to run.
// Run with: go test -tags=integration ./internal/api/openai/...

// TEST-20-02-01: POST /v1/embeddings returns valid response from mock.
func TestEmbeddings_ValidResponse(t *testing.T) {
	srv, calls, _ := mockEmbeddingsServer(t)
	defer srv.Close()

	h := embeddingsTestHandler(t, srv.URL)

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "Hello, world!",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytesReader{data: body})
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

			req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytesReader{data: body})
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

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytesReader{data: body})
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

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	h.Embeddings(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	if got := calls.Load(); got < 1 {
		t.Errorf("provider called %d times, want at least 1", got)
	}
}
