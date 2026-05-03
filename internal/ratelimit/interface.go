package ratelimit

import "time"

// RateLimiter is satisfied by both local Limiter and RedisLimiter.
type RateLimiter interface {
	CheckRPM(keyID string) (allowed bool, limit int, remaining int, resetAt time.Time)
}
