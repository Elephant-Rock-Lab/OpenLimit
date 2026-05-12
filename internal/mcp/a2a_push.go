package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	client       *http.Client
	logger       *slog.Logger
	ssrfDisabled bool // test-only: bypass SSRF validation
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
// The URL is validated against SSRF attacks before any request is sent.
func (p *PushNotifier) Notify(ctx context.Context, task *A2ATask, config *PushConfig) error {
	if config == nil || config.URL == "" {
		return nil
	}

	if !p.ssrfDisabled {
		if err := validatePushURL(config.URL); err != nil {
			return fmt.Errorf("push URL rejected: %w", err)
		}
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

// validatePushURL rejects URLs that could be used for SSRF attacks.
// It checks:
//   - Scheme must be http or https
//   - Hostname must resolve to a public IP (not private, loopback, or link-local)
func validatePushURL(rawURL string) error {
	if rawURL == "" {
		return nil // no URL configured — caller handles nil/empty separately
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow http and https schemes
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (only http and https)", parsed.Scheme)
	}

	// Must have a host
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL must have a host")
	}

	// Check if host is a raw IP — validate directly without DNS lookup
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrReserved(ip) {
			return fmt.Errorf("IP %s is private/reserved", ip)
		}
		return nil
	}

	// Resolve hostname and check against private/reserved ranges
	ips, err := net.LookupIP(host)
	if err != nil {
		// If resolution fails, reject — we can't verify it's safe
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}

	for _, ip := range ips {
		if isPrivateOrReserved(ip) {
			return fmt.Errorf("host %q resolves to private/reserved IP %s", host, ip)
		}
	}

	return nil
}

// isPrivateOrReserved checks if an IP is in a private, loopback, or
// link-local range that should not be accessible via SSRF.
func isPrivateOrReserved(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if ip.IsPrivate() {
		return true // covers 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7
	}
	if ip.IsLinkLocalUnicast() {
		return true // covers 169.254.0.0/16, fe80::/10
	}
	if ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsUnspecified() {
		return true // 0.0.0.0, ::
	}
	return false
}
