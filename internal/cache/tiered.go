package cache

import (
	"context"
	"time"

	"openlimit/internal/cache/semantic"
	"openlimit/internal/schema/openai"
)

// TieredCache checks exact cache first (O(1) hash), then semantic cache (vector search).
type TieredCache struct {
	exact    Cache
	semantic *semantic.Cache
}

// NewTieredCache creates a tiered cache combining exact and semantic backends.
func NewTieredCache(exact Cache, sem *semantic.Cache) *TieredCache {
	return &TieredCache{
		exact:    exact,
		semantic: sem,
	}
}

// TieredResult extends the basic cache result with information about which tier hit.
type TieredResult struct {
	Response *openai.ChatCompletionResponse
	Hit      bool
	Tier     string // "exact", "semantic", or ""
}

// GetTiered checks both cache tiers and returns which tier hit.
func (tc *TieredCache) GetTiered(ctx context.Context, exactKey string, model, queryText string) TieredResult {
	// 1. Check exact cache
	if tc.exact != nil {
		if cached, ok, err := tc.exact.Get(ctx, exactKey); err == nil && ok {
			return TieredResult{Response: cached, Hit: true, Tier: "exact"}
		}
	}

	// 2. Check semantic cache
	if tc.semantic != nil {
		if cached, ok, err := tc.semantic.Get(ctx, model, queryText); err == nil && ok {
			return TieredResult{Response: cached, Hit: true, Tier: "semantic"}
		}
	}

	return TieredResult{Hit: false}
}

// Get implements the Cache interface for backward compatibility.
func (tc *TieredCache) Get(ctx context.Context, key string) (*openai.ChatCompletionResponse, bool, error) {
	if tc.exact != nil {
		return tc.exact.Get(ctx, key)
	}
	return nil, false, nil
}

// Set stores in the exact cache. Semantic cache storage is handled separately.
func (tc *TieredCache) Set(ctx context.Context, key string, value *openai.ChatCompletionResponse, ttl time.Duration) error {
	if tc.exact != nil {
		return tc.exact.Set(ctx, key, value, ttl)
	}
	return nil
}

// SetBoth stores in both exact and semantic caches.
func (tc *TieredCache) SetBoth(ctx context.Context, exactKey string, model, queryText string, value *openai.ChatCompletionResponse, ttl time.Duration) {
	// Store in exact cache
	if tc.exact != nil {
		_ = tc.exact.Set(ctx, exactKey, value, ttl)
	}
	// Store in semantic cache (best-effort)
	if tc.semantic != nil {
		_ = tc.semantic.Set(ctx, model, queryText, value)
	}
}

// Exact returns the underlying exact cache.
func (tc *TieredCache) Exact() Cache { return tc.exact }

// Semantic returns the underlying semantic cache (may be nil).
func (tc *TieredCache) Semantic() *semantic.Cache { return tc.semantic }
