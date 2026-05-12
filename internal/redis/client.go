package redis

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Client wraps go-redis with health checking and graceful degradation.
// A nil Client means Redis is disabled — callers check `rc != nil && rc.Healthy()`.
// Supports both standalone and cluster modes.
type Client struct {
	client  UniversalClient // supports both standalone and cluster
	healthy  atomic.Bool
	logger   *slog.Logger
	cancel   context.CancelFunc
}

// UniversalClient is an interface satisfied by both *goredis.Client and *goredis.ClusterClient.
type UniversalClient interface {
	Ping(ctx context.Context) *goredis.StatusCmd
	Close() error
	Publish(ctx context.Context, channel string, message interface{}) *goredis.IntCmd
	Subscribe(ctx context.Context, channels ...string) *goredis.PubSub
	Del(ctx context.Context, keys ...string) *goredis.IntCmd
	Get(ctx context.Context, key string) *goredis.StringCmd
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) *goredis.StatusCmd
	HSet(ctx context.Context, key string, values ...interface{}) *goredis.IntCmd
	HGetAll(ctx context.Context, key string) *goredis.MapStringStringCmd
	HIncrBy(ctx context.Context, key, field string, incr int64) *goredis.IntCmd
	ZAdd(ctx context.Context, key string, members ...goredis.Z) *goredis.IntCmd
	ZRemRangeByScore(ctx context.Context, key, min, max string) *goredis.IntCmd
	ZCard(ctx context.Context, key string) *goredis.IntCmd
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *goredis.Cmd
	Incr(ctx context.Context, key string) *goredis.IntCmd
}

// NewClient creates a Redis client. Returns nil if addr is empty (Redis disabled).
// If cluster=true, creates a ClusterClient instead of a standalone Client.
func NewClient(addr string, password string, db int, maxRetries int, poolSize int, healthInterval time.Duration, logger *slog.Logger, cluster bool) *Client {
	if addr == "" {
		return nil
	}

	if poolSize <= 0 {
		poolSize = 20
	}
	if maxRetries < 0 {
		maxRetries = 3
	}
	if healthInterval <= 0 {
		healthInterval = 10 * time.Second
	}

	var rdb UniversalClient
	if cluster {
		rdb = goredis.NewClusterClient(&goredis.ClusterOptions{
			Addrs:        []string{addr},
			Password:     password,
			MaxRetries:   maxRetries,
			PoolSize:     poolSize,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			PoolTimeout:  4 * time.Second,
		})
		logger.Info("redis cluster client configured", "addr", addr)
	} else {
		rdb = goredis.NewClient(&goredis.Options{
			Addr:         addr,
			Password:     password,
			DB:           db,
			MaxRetries:   maxRetries,
			PoolSize:     poolSize,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			PoolTimeout:  4 * time.Second,
		})
	}

	c := &Client{
		client: rdb,
		logger: logger,
	}

	// Initial health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Warn("redis initial connection failed, starting in degraded mode", "addr", addr, "error", err)
		c.healthy.Store(false)
	} else {
		c.healthy.Store(true)
		logger.Info("redis connected", "addr", addr, "cluster", cluster)
	}

	// Background health checker
	bgCtx, bgCancel := context.WithCancel(context.Background())
	c.cancel = bgCancel
	go c.healthCheck(bgCtx, healthInterval)

	return c
}

// Healthy returns true if Redis is reachable.
func (c *Client) Healthy() bool {
	if c == nil {
		return false
	}
	return c.healthy.Load()
}

// Close shuts down the health checker and Redis connection.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	return c.client.Close()
}

// SetHealthy forces the health state (for testing).
func (c *Client) SetHealthy(h bool) {
	c.healthy.Store(h)
}

// Universal returns the underlying universal client (for subsystems that need pub/sub etc).
func (c *Client) Universal() UniversalClient {
	if c == nil {
		return nil
	}
	return c.client
}

// Standalone returns the underlying *goredis.Client (nil for cluster mode).
// Use Universal() for methods that work with both modes.
func (c *Client) Standalone() *goredis.Client {
	if c == nil {
		return nil
	}
	if sc, ok := c.client.(*goredis.Client); ok {
		return sc
	}
	return nil
}

// Del deletes a key.
func (c *Client) Del(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

// Get returns a string value.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

// Set a string value with optional TTL.
func (c *Client) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

// HSet sets hash fields.
func (c *Client) HSet(ctx context.Context, key string, values ...interface{}) error {
	return c.client.HSet(ctx, key, values...).Err()
}

// HGetAll returns all hash fields.
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return c.client.HGetAll(ctx, key).Result()
}

// HIncrBy atomically increments a hash field by the given value.
func (c *Client) HIncrBy(ctx context.Context, key, field string, incr int64) (int64, error) {
	return c.client.HIncrBy(ctx, key, field, incr).Result()
}

// ZAdd adds a member to a sorted set.
func (c *Client) ZAdd(ctx context.Context, key string, members ...goredis.Z) error {
	return c.client.ZAdd(ctx, key, members...).Err()
}

// ZRemRangeByScore removes members with scores between min and max.
func (c *Client) ZRemRangeByScore(ctx context.Context, key string, min, max string) error {
	return c.client.ZRemRangeByScore(ctx, key, min, max).Err()
}

// ZCard returns the cardinality of a sorted set.
func (c *Client) ZCard(ctx context.Context, key string) (int64, error) {
	return c.client.ZCard(ctx, key).Result()
}

// Eval evaluates a Lua script.
func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	return c.client.Eval(ctx, script, keys, args...).Result()
}

// Incr increments a key.
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}

func (c *Client) healthCheck(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := c.client.Ping(checkCtx).Err()
			cancel()

			wasHealthy := c.healthy.Load()
			nowHealthy := err == nil
			c.healthy.Store(nowHealthy)

			if wasHealthy && !nowHealthy {
				c.logger.Warn("redis became unhealthy", "error", fmt.Sprintf("ping failed"))
			} else if !wasHealthy && nowHealthy {
				c.logger.Info("redis recovered")
			}
		}
	}
}
