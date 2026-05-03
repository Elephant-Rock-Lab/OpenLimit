package semantic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/schema/openai"
)

// Cache implements semantic caching using vector similarity search.
type Cache struct {
	db           *sql.DB
	embedder     Embedder
	embedCache   *EmbeddingCache
	circuit      *CircuitBreaker
	threshold    float64
	maxEntries   int
	ttl          time.Duration
	dimensions   int
	logger       *slog.Logger
	prunerCancel context.CancelFunc
}

// NewCache creates a semantic cache backed by pgvector.
func NewCache(db *sql.DB, cfg config.SemanticCacheConfig, logger *slog.Logger) (*Cache, error) {
	embedder, err := NewEmbedder(
		cfg.Embedder.Type,
		cfg.Embedder.BaseURL,
		cfg.Embedder.Model,
		cfg.Embedder.APIKey,
		cfg.Embedder.Dimensions,
	)
	if err != nil {
		return nil, fmt.Errorf("semantic cache embedder: %w", err)
	}

	threshold := cfg.SimilarityThreshold
	if threshold <= 0 || threshold > 1 {
		threshold = 0.92
	}

	maxEntries := cfg.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 10000
	}

	ttl := time.Duration(cfg.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}

	embedCacheMax := cfg.EmbeddingCache.MaxEntries
	if embedCacheMax <= 0 {
		embedCacheMax = 5000
	}
	embedCacheTTL := time.Duration(cfg.EmbeddingCache.TTLSeconds) * time.Second
	if embedCacheTTL <= 0 {
		embedCacheTTL = time.Hour
	}

	c := &Cache{
		db:         db,
		embedder:   embedder,
		embedCache: NewEmbeddingCache(embedCacheMax, embedCacheTTL),
		circuit:    NewCircuitBreaker(),
		threshold:  threshold,
		maxEntries: maxEntries,
		ttl:        ttl,
		dimensions: embedder.Dimensions(),
		logger:     logger,
	}

	// Check if pgvector is available
	if err := c.checkPgVector(context.Background()); err != nil {
		return nil, fmt.Errorf("pgvector check failed: %w", err)
	}

	return c, nil
}

// StartPruner starts a background goroutine that removes expired cache entries.
func (c *Cache) StartPruner(ctx context.Context) {
	prunerCtx, cancel := context.WithCancel(ctx)
	c.prunerCancel = cancel

	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-prunerCtx.Done():
				return
			case <-ticker.C:
				c.prune(prunerCtx)
			}
		}
	}()
}

// StopPruner stops the background pruner goroutine.
func (c *Cache) StopPruner() {
	if c.prunerCancel != nil {
		c.prunerCancel()
	}
}

// Get searches for a semantically similar cached response.
// Returns the response and true if a match is found above the similarity threshold.
func (c *Cache) Get(ctx context.Context, model, queryText string) (*openai.ChatCompletionResponse, bool, error) {
	embedding, err := c.getEmbedding(ctx, queryText)
	if err != nil {
		c.logger.Debug("semantic cache embedding failed, treating as miss", "error", err)
		return nil, false, nil // Graceful degradation
	}

	// Cosine similarity search using pgvector's <=> operator (cosine distance)
	// distance = 1 - cosine_similarity, so we want distance < (1 - threshold)
	maxDistance := 1.0 - c.threshold

	query := `
		SELECT response FROM semantic_cache
		WHERE model = $1
		  AND (expires_at IS NULL OR expires_at > now())
		  AND (embedding <=> $2) < $3
		ORDER BY embedding <=> $2
		LIMIT 1`

	var respJSON string
	err = c.db.QueryRowContext(ctx, query, model, vectorString(embedding), maxDistance).Scan(&respJSON)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		c.logger.Debug("semantic cache query error", "error", err)
		return nil, false, nil
	}

	var resp openai.ChatCompletionResponse
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		c.logger.Debug("semantic cache unmarshal error", "error", err)
		return nil, false, nil
	}

	return &resp, true, nil
}

// Set stores a response in the semantic cache.
func (c *Cache) Set(ctx context.Context, model, queryText string, resp *openai.ChatCompletionResponse) error {
	embedding, err := c.getEmbedding(ctx, queryText)
	if err != nil {
		c.logger.Debug("semantic cache store: embedding failed, skipping", "error", err)
		return nil // Best-effort
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	expiresAt := time.Now().Add(c.ttl)
	query := `
		INSERT INTO semantic_cache (model, embedding, request_hash, response, created_at, expires_at)
		VALUES ($1, $2, $3, $4, now(), $5)`

	_, err = c.db.ExecContext(ctx, query, model, vectorString(embedding), hashText(queryText), string(respJSON), expiresAt)
	if err != nil {
		c.logger.Debug("semantic cache store error", "error", err)
		return nil // Best-effort
	}

	return nil
}

// getEmbedding returns the embedding for text, using the cache if available.
func (c *Cache) getEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Check circuit breaker
	if !c.circuit.Allow() {
		return nil, fmt.Errorf("embedding circuit breaker open")
	}

	// Check embedding cache
	if cached := c.embedCache.Get(text); cached != nil {
		return cached, nil
	}

	// Call embedder
	embedding, err := c.embedder.Embed(ctx, text)
	if err != nil {
		c.circuit.RecordFailure()
		return nil, err
	}
	c.circuit.RecordSuccess()

	// Store in embedding cache
	c.embedCache.Set(text, embedding)

	return embedding, nil
}

func (c *Cache) checkPgVector(ctx context.Context) error {
	var exists bool
	err := c.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector')").Scan(&exists)
	if err != nil {
		return fmt.Errorf("pgvector extension check: %w (install pgvector or disable semantic cache)", err)
	}
	if !exists {
		// Try to create it
		_, err := c.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
		if err != nil {
			return fmt.Errorf("pgvector extension not available: %w (install pgvector or disable semantic cache)", err)
		}
	}
	return nil
}

func (c *Cache) prune(ctx context.Context) {
	result, err := c.db.ExecContext(ctx, "DELETE FROM semantic_cache WHERE expires_at < now()")
	if err != nil {
		c.logger.Debug("semantic cache prune error", "error", err)
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		c.logger.Debug("semantic cache pruned entries", "count", n)
	}
}

// vectorString formats a float32 slice as a pgvector-compatible string.
func vectorString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		// Handle NaN/Inf
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			b.WriteString("0")
		} else {
			fmt.Fprintf(&b, "%f", f)
		}
	}
	b.WriteByte(']')
	return b.String()
}
