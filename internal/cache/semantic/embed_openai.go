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

// OpenAIEmbedder calls an OpenAI-compatible /v1/embeddings endpoint.
// Works with OpenAI, Ollama's OpenAI compatibility mode, and any compatible service.
type OpenAIEmbedder struct {
	baseURL    string
	model      string
	apiKey     string
	dimensions int
	client     *http.Client
}

// NewOpenAIEmbedder creates an embedder that calls /v1/embeddings.
func NewOpenAIEmbedder(baseURL, model, apiKey string, dimensions int) (*OpenAIEmbedder, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("openai embedder requires base_url")
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	if dimensions <= 0 {
		dimensions = 1536
	}
	return &OpenAIEmbedder{
		baseURL:    baseURL,
		model:      model,
		apiKey:     apiKey,
		dimensions: dimensions,
		client:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (e *OpenAIEmbedder) Dimensions() int { return e.dimensions }

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	payload := map[string]any{
		"model": e.model,
		"input": text,
	}
	if e.dimensions > 0 {
		payload["dimensions"] = e.dimensions
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("embedding API returned no data")
	}

	return result.Data[0].Embedding, nil
}
