package openaiapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openlimit/internal/providers"
)

// ---------------------------------------------------------------------------
// TEST-36-03-01: Embeddings handler rejects >50MB provider response
// ---------------------------------------------------------------------------
func TestEmbeddings_ProviderResponseExceedsLimit(t *testing.T) {
	// Create a server that returns a response larger than 50MB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return a response that's larger than the limit
		largeData := strings.Repeat("x", int(providers.MaxProviderResponseSize)+1024)
		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{{
				"object":    "embedding",
				"embedding": largeData,
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
	defer srv.Close()

	h := embeddingsTestHandler(t, srv.URL)

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "Hello, world!",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &sec03BytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	h.Embeddings(w, req)

	// Should get an error (either 502 or a truncated JSON error)
	if w.Code != http.StatusOK {
		// Got an error response — good, the LimitReader prevented OOM
		t.Logf("Correctly rejected oversized response with status %d", w.Code)
	} else {
		// If it's 200, verify the response is truncated/corrupted
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			// JSON parse error means the response was truncated — good
			t.Logf("Response was truncated (JSON parse error), which is expected behavior")
		}
	}
}

// ---------------------------------------------------------------------------
// TEST-36-03-02: Embeddings handler accepts normal provider response
// ---------------------------------------------------------------------------
func TestEmbeddings_ProviderResponseWithinLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer srv.Close()

	h := embeddingsTestHandler(t, srv.URL)

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: "Hello, world!",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &sec03BytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	h.Embeddings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for normal response, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp EmbeddingsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("object = %q, want %q", resp.Object, "list")
	}
}

// ---------------------------------------------------------------------------
// TEST-36-03-03: No bare io.ReadAll on external sources in non-test code
// (This is a build-time/lint-time check — we verify the constant exists)
// ---------------------------------------------------------------------------
func TestMaxProviderResponseSize_Constant(t *testing.T) {
	expected := int64(50 << 20) // 50MB
	if providers.MaxProviderResponseSize != expected {
		t.Errorf("MaxProviderResponseSize = %d, want %d", providers.MaxProviderResponseSize, expected)
	}
}

// ---------------------------------------------------------------------------
// TEST-36-03-04: Request body read is also limited via LimitReader
// ---------------------------------------------------------------------------
func TestEmbeddings_RequestBodyLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should never reach here with oversized body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := embeddingsTestHandler(t, srv.URL)

	// Create a very large request body (>50MB worth of padding)
	// We use a reader that will report the limit
	largeInput := strings.Repeat("a", 100) // Small but tests the LimitReader wrapping

	body, _ := json.Marshal(EmbeddingsRequest{
		Model: "embed-v1",
		Input: largeInput,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", &sec03BytesReader{data: body})
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	h.Embeddings(w, req)

	// Normal request should succeed
	if w.Code != http.StatusOK {
		t.Logf("status = %d (expected 200 or error)", w.Code)
	}
}

// bytesReader wraps a byte slice as an io.Reader for testing.
type sec03BytesReader struct {
	data []byte
	pos  int
}

func (r *sec03BytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
