package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"time"
)

// PushConfig contains the webhook push notification configuration.
type PushConfig struct {
	URL       string `json:"url"`
	AuthType  string `json:"auth_type,omitempty"`
	AuthToken string `json:"auth_token,omitempty"`
}

// PushNotifier sends webhook push notifications for task status changes.
type PushNotifier struct {
	client *http.Client
	logger *slog.Logger
}

// NewPushNotifier creates a new push notifier.
func NewPushNotifier(logger *slog.Logger) *PushNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &PushNotifier{
		client: &http.Client{Timeout: 5 * time.Second},
		logger: logger,
	}
}

// Notify sends a task update to the configured webhook URL.
// Retries up to 3 times with exponential backoff + jitter.
func (p *PushNotifier) Notify(ctx context.Context, task *A2ATask, config *PushConfig) error {
	if config == nil || config.URL == "" {
		return nil
	}

	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(250<<attempt) * time.Millisecond // 500ms, 1s
			jitter := time.Duration(rand.Int63n(int64(backoff) / 2))
			select {
			case <-time.After(backoff + jitter):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		if config.AuthType == "bearer" && config.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+config.AuthToken)
		}

		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = err
			p.logger.Warn("push notification attempt failed",
				"task_id", task.ID,
				"attempt", attempt+1,
				"error", err,
			)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		p.logger.Warn("push notification received non-2xx",
			"task_id", task.ID,
			"attempt", attempt+1,
			"status", resp.StatusCode,
		)
	}

	return fmt.Errorf("push notification failed after 3 attempts: %w", lastErr)
}
