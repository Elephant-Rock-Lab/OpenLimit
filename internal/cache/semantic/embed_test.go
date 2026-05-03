package semantic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedder_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{0.1, 0.2, 0.3}},
			},
		})
	}))
	defer server.Close()

	embedder, err := NewOpenAIEmbedder(server.URL, "test-model", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if embedder.Dimensions() != 3 {
		t.Errorf("expected 3 dimensions, got %d", embedder.Dimensions())
	}

	vec, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 || vec[0] != 0.1 {
		t.Errorf("unexpected embedding: %v", vec)
	}
}

func TestOpenAIEmbedder_NoBaseURL(t *testing.T) {
	_, err := NewOpenAIEmbedder("", "model", "", 3)
	if err == nil {
		t.Fatal("expected error for empty base_url")
	}
}

func TestOllamaEmbedder_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"embedding": []float32{0.4, 0.5, 0.6},
		})
	}))
	defer server.Close()

	embedder, err := NewOllamaEmbedder(server.URL, "nomic-embed-text", 3)
	if err != nil {
		t.Fatal(err)
	}
	if embedder.Dimensions() != 3 {
		t.Errorf("expected 3 dimensions, got %d", embedder.Dimensions())
	}

	vec, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 || vec[0] != 0.4 {
		t.Errorf("unexpected embedding: %v", vec)
	}
}

func TestOllamaEmbedder_NoBaseURL(t *testing.T) {
	_, err := NewOllamaEmbedder("", "model", 3)
	if err == nil {
		t.Fatal("expected error for empty base_url")
	}
}

func TestNewEmbedder_UnsupportedType(t *testing.T) {
	_, err := NewEmbedder("unknown", "http://localhost", "model", "", 3)
	if err == nil {
		t.Fatal("expected error for unknown embedder type")
	}
}

func TestVectorString(t *testing.T) {
	tests := []struct {
		input []float32
		want  string
	}{
		{[]float32{}, "[]"},
		{[]float32{1.0}, "[1.000000]"},
		{[]float32{0.1, 0.2}, "[0.100000,0.200000]"},
	}
	for _, tt := range tests {
		got := vectorString(tt.input)
		if got != tt.want {
			t.Errorf("vectorString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
