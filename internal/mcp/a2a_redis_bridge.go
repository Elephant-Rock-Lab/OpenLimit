package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// RedisTaskBridge publishes A2A task updates to a Redis Pub/Sub channel and
// subscribes to relay remote updates from other gateway instances to the local
// TaskNotifier. It is nil-safe — all methods are no-ops when the receiver is nil
// or when the Redis client is unavailable.
type RedisTaskBridge struct {
	redisClient *goredis.Client
	channel     string
	notifier    *TaskNotifier // local notifier for relaying remote updates
	logger      *slog.Logger
	instanceID  string // unique per gateway instance for loop prevention
	subCancel   context.CancelFunc
	closeOnce   sync.Once
	closed      atomic.Bool
}

// bridgeMessage wraps an A2ATask with an origin field so subscribers can
// skip messages they published themselves (loop prevention).
type bridgeMessage struct {
	Origin     string   `json:"origin"`
	Task       A2ATask  `json:"task"`
}

// NewRedisTaskBridge creates a Redis-backed bridge for cross-instance A2A task
// notification. If redisClient is nil, returns nil (single-instance mode).
func NewRedisTaskBridge(redisClient *goredis.Client, channel string, notifier *TaskNotifier, instanceID string, logger *slog.Logger) *RedisTaskBridge {
	if redisClient == nil {
		return nil
	}
	if channel == "" {
		channel = "openlimit:a2a:task_updates"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RedisTaskBridge{
		redisClient: redisClient,
		channel:     channel,
		notifier:    notifier,
		instanceID:  instanceID,
		logger:      logger.With("component", "a2a_redis_bridge"),
	}
}

// Start begins subscribing to the Redis channel. Must be called after
// construction. Safe to call on nil receiver.
func (b *RedisTaskBridge) Start() {
	if b == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.subCancel = cancel
	go b.subscribeLoop(ctx)
	b.logger.Info("Redis task bridge started", "channel", b.channel, "instance", b.instanceID)
}

// Publish serializes the task update and publishes it to the Redis channel.
// No-op on nil receiver. Errors are logged but not propagated — Redis Pub/Sub
// is best-effort; the local notifier already handled the update synchronously.
func (b *RedisTaskBridge) Publish(task *A2ATask) {
	if b == nil || b.closed.Load() {
		return
	}
	msg := bridgeMessage{
		Origin: b.instanceID,
		Task:   *task,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		b.logger.Error("failed to marshal task for Redis publish", "task_id", task.ID, "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := b.redisClient.Publish(ctx, b.channel, data).Err(); err != nil {
		b.logger.Warn("failed to publish task update to Redis", "task_id", task.ID, "error", err)
	}
}

// Close stops the subscriber and cleans up. Safe to call multiple times.
// No-op on nil receiver.
func (b *RedisTaskBridge) Close() {
	if b == nil {
		return
	}
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		if b.subCancel != nil {
			b.subCancel()
		}
		b.logger.Info("Redis task bridge closed")
	})
}

// subscribeLoop manages the Redis subscription with reconnection and
// exponential backoff. Runs until the context is cancelled.
func (b *RedisTaskBridge) subscribeLoop(ctx context.Context) {
	backoff := 100 * time.Millisecond
	maxBackoff := 10 * time.Second

	for {
		if b.closed.Load() {
			return
		}

		sub := b.redisClient.Subscribe(ctx, b.channel)
		// Confirm subscription is active
		if _, err := sub.Receive(ctx); err != nil {
			b.logger.Warn("Redis subscribe failed, reconnecting", "error", err, "backoff", backoff)
			sub.Close()

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff = backoff * 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
		}

		// Reset backoff on successful connection
		backoff = 100 * time.Millisecond
		b.logger.Info("Redis bridge subscribed", "channel", b.channel)

		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				sub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					// Channel closed — connection lost, reconnect
					sub.Close()
					b.logger.Warn("Redis subscription channel closed, reconnecting")
					goto reconnect
				}
				b.handleMessage(msg)
			}
		}

	reconnect:
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// handleMessage deserializes a Redis message and relays it to the local
// notifier, skipping messages from this instance (loop prevention).
func (b *RedisTaskBridge) handleMessage(msg *goredis.Message) {
	var bm bridgeMessage
	if err := json.Unmarshal([]byte(msg.Payload), &bm); err != nil {
		b.logger.Warn("failed to unmarshal Redis task message", "error", err)
		return
	}

	// Loop prevention: skip messages we published
	if bm.Origin == b.instanceID {
		return
	}

	// Relay to local notifier so SSE watchers on this instance receive it
	if b.notifier != nil {
		b.notifier.Notify(&bm.Task)
	}
}
