package semantic

import (
	"context"
	"fmt"
)

// Embedder computes vector embeddings for text.
type Embedder interface {
	// Embed returns the embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
	// Dimensions returns the dimensionality of the embedding vectors.
	Dimensions() int
}

// EmbedderFactory creates an Embedder from config.
func NewEmbedder(embedderType, baseURL, model, apiKey string, dimensions int) (Embedder, error) {
	switch embedderType {
	case "openai":
		return NewOpenAIEmbedder(baseURL, model, apiKey, dimensions)
	case "ollama":
		return NewOllamaEmbedder(baseURL, model, dimensions)
	default:
		return nil, fmt.Errorf("unsupported embedder type %q (use \"openai\" or \"ollama\")", embedderType)
	}
}
