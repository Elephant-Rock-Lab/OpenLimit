package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	rediscli "openlimit/internal/redis"

	"openlimit/internal/schema/openai"
)

// RedisCache implements the Cache interface backed by Redis.
// Used when Redis is available for shared cache across gateway instances.
type RedisCache struct {
	rc  *rediscli.Client
	ttl time.Duration
}

// NewRedisCache creates a Redis-backed cache. Responses are serialized as JSON.
func NewRedisCache(rc *rediscli.Client, ttl time.Duration) *RedisCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &RedisCache{rc: rc, ttl: ttl}
}

// Get retrieves a cached response from Redis.
func (c *RedisCache) Get(ctx context.Context, key string) (*openai.ChatCompletionResponse, bool, error) {
	data, err := c.rc.Get(ctx, c.redisKey(key))
	if err != nil {
		// Key not found is a miss, not an error
		return nil, false, nil
	}

	var resp openai.ChatCompletionResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		// Corrupt data — treat as miss
		return nil, false, nil
	}

	return &resp, true, nil
}

// Set stores a response in Redis with the configured TTL.
func (c *RedisCache) Set(ctx context.Context, key string, value *openai.ChatCompletionResponse, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.ttl
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal cache entry: %w", err)
	}

	return c.rc.Set(ctx, c.redisKey(key), data, ttl)
}

// redisKey prefixes cache keys to avoid collisions with other Redis data.
func (c *RedisCache) redisKey(key string) string {
	return "cache:" + key
}
