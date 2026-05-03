package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaEmbedder calls Ollama's native /api/embeddings endpoint.
// Response shape: {"embedding": [...]} (differs from OpenAI format).
type OllamaEmbedder struct {
	baseURL    string
	model      string
	dimensions int
	client     *http.Client
}

// NewOllamaEmbedder creates an embedder that calls Ollama's /api/embeddings.
func NewOllamaEmbedder(baseURL, model string, dimensions int) (*OllamaEmbedder, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("ollama embedder requires base_url")
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	if dimensions <= 0 {
		dimensions = 768
	}
	return &OllamaEmbedder{
		baseURL:    baseURL,
		model:      model,
		dimensions: dimensions,
		client:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (e *OllamaEmbedder) Dimensions() int { return e.dimensions }

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	payload := map[string]any{
		"model":  e.model,
		"prompt": text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embedding API returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Ollama response: {"embedding": [...]}
	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama embedding response: %w", err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama embedding API returned empty embedding")
	}

	return result.Embedding, nil
}
