package ratelimit

import (
	"context"
	"fmt"
	"time"

	rediscli "openlimit/internal/redis"
)

// RedisLimiter implements distributed rate limiting using Redis sorted sets
// with a sliding window algorithm. Falls back to local Limiter when Redis
// is unavailable (handled by the caller).
type RedisLimiter struct {
	rc     *rediscli.Client
	keyID  string
	rpm    int
	tpm    int
	window time.Duration
}

// NewRedisLimiter creates a Redis-backed rate limiter for the given key.
func NewRedisLimiter(rc *rediscli.Client, keyID string, rpm, tpm int) *RedisLimiter {
	return &RedisLimiter{
		rc:     rc,
		keyID:  keyID,
		rpm:    rpm,
		tpm:    tpm,
		window: time.Minute,
	}
}

// CheckRPM checks if a request is allowed under the RPM limit using a
// sliding window. Returns (allowed, limit, remaining, resetAt).
func (rl *RedisLimiter) CheckRPM(_ string) (bool, int, int, time.Time) {
	if rl.rpm <= 0 {
		return true, 0, 0, time.Time{}
	}
	return rl.checkCount(context.Background(), "rpm", 1)
}

// CheckTPM checks if a token count is allowed under the TPM limit.
func (rl *RedisLimiter) CheckTPM(ctx context.Context, tokenCount int) (bool, int, int, time.Time) {
	if rl.tpm <= 0 {
		return true, 0, 0, time.Time{}
	}
	return rl.checkCount(ctx, "tpm", tokenCount)
}

// checkCount uses a Lua script to atomically:
// 1. Remove expired entries (score < now - window)
// 2. Count remaining entries
// 3. If under limit, add new entry
// 4. Return current count
//
// Sorted set key format: rl:{keyID}:{type}
// Member: unique ID (timestamp:counter)
// Score: Unix nano timestamp
func (rl *RedisLimiter) checkCount(ctx context.Context, limitType string, count int) (bool, int, int, time.Time) {
	key := fmt.Sprintf("rl:%s:%s", rl.keyID, limitType)
	limit := rl.rpm
	if limitType == "tpm" {
		limit = rl.tpm
	}

	now := time.Now()
	windowStart := now.Add(-rl.window).UnixNano()
	// Use a unique member ID via INCR to prevent sorted set deduplication
	// when multiple requests arrive at the same nanosecond.
	uniqueID, err := rl.rc.Incr(ctx, key+":seq")
	if err != nil {
		return true, limit, limit, now.Add(rl.window)
	}
	member := fmt.Sprintf("%d:%d", now.UnixNano(), uniqueID)
	score := float64(now.UnixNano())

	// Lua script for atomic sliding window check
	script := `
local key = KEYS[1]
local window_start = tonumber(ARGV[1])
local member = ARGV[2]
local score = tonumber(ARGV[3])
local limit = tonumber(ARGV[4])
local count = tonumber(ARGV[5])

-- Remove expired entries
redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

-- Count current entries
local current = redis.call('ZCARD', key)

if current + count <= limit then
    -- Add new entry (member may represent multiple tokens for TPM)
    for i = 1, count do
        redis.call('ZADD', key, score, member .. ':' .. i)
    end
    -- Set TTL to 2x window to prevent unbounded growth
    redis.call('EXPIRE', key, 120)
    return {1, limit, limit - current - count}
else
    redis.call('EXPIRE', key, 120)
    return {0, limit, limit - current}
end
`

	result, err := rl.rc.Eval(ctx, script, []string{key},
		windowStart,
		member,
		score,
		limit,
		count,
	)
	if err != nil {
		// On Redis error, allow the request (fail-open)
		return true, limit, limit, now.Add(rl.window)
	}

	res, ok := result.([]interface{})
	if !ok || len(res) < 3 {
		return true, limit, limit, now.Add(rl.window)
	}

	allowed := res[0].(int64) == 1
	limitVal := int(res[1].(int64))
	remaining := int(res[2].(int64))
	if remaining < 0 {
		remaining = 0
	}

	resetAt := now.Add(rl.window)
	return allowed, limitVal, remaining, resetAt
}
