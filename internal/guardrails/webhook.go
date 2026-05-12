package guardrails

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"openlimit/internal/providers"
)

// WebhookStage calls an external HTTP service for guardrail validation.
// Expected response: {"action": "pass"|"block", "message": "...", "redacted": "..."}
type WebhookStage struct {
	url            string
	client         *http.Client
	blockOnError   bool
	blockOnTimeout bool
}

// NewWebhookStage creates a webhook guardrail stage.
func NewWebhookStage(url string, timeoutMS int, blockOnError, blockOnTimeout bool) *WebhookStage {
	timeout := 250 * time.Millisecond
	if timeoutMS > 0 {
		timeout = time.Duration(timeoutMS) * time.Millisecond
	}
	return &WebhookStage{
		url: url,
		client: &http.Client{
			Timeout: timeout,
		},
		blockOnError:   blockOnError,
		blockOnTimeout: blockOnTimeout,
	}
}

// NewWebhookStageWithTLS creates a webhook stage with mTLS support.
// certFile and keyFile are the client certificate pair. caFile is the CA to verify the server.
func NewWebhookStageWithTLS(url string, timeoutMS int, blockOnError, blockOnTimeout bool, certFile, keyFile, caFile string) (*WebhookStage, error) {
	timeout := 250 * time.Millisecond
	if timeoutMS > 0 {
		timeout = time.Duration(timeoutMS) * time.Millisecond
	}

	tlsCfg := &tls.Config{}

	// Load client certificate
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// Load CA for server verification
	if caFile != "" {
		caData, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to append CA certs from %s", caFile)
		}
		tlsCfg.RootCAs = caPool
	}

	return &WebhookStage{
		url: url,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsCfg,
			},
		},
		blockOnError:   blockOnError,
		blockOnTimeout: blockOnTimeout,
	}, nil
}

func (s *WebhookStage) Name() string { return "webhook" }

type webhookRequest struct {
	Direction string `json:"direction"`
	Content   string `json:"content"`
}

type webhookResponse struct {
	Action   string `json:"action"` // "pass", "block", "redact"
	Message  string `json:"message,omitempty"`
	Redacted string `json:"redacted,omitempty"`
}

func (s *WebhookStage) CheckInput(ctx context.Context, messages []Message) (Result, error) {
	// Concatenate messages for webhook payload
	var buf bytes.Buffer
	for i, msg := range messages {
		if i > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(msg.Content)
	}
	return s.call(ctx, "input", buf.String())
}

func (s *WebhookStage) CheckOutput(ctx context.Context, content string) (Result, error) {
	return s.call(ctx, "output", content)
}

func (s *WebhookStage) call(ctx context.Context, direction, content string) (Result, error) {
	payload := webhookRequest{Direction: direction, Content: content}
	body, err := json.Marshal(payload)
	if err != nil {
		if s.blockOnError {
			return Result{Action: Block, Message: "webhook encoding error", StageName: s.Name()}, nil
		}
		return Result{Action: Pass}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		if s.blockOnError {
			return Result{Action: Block, Message: "webhook request error", StageName: s.Name()}, nil
		}
		return Result{Action: Pass}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		// Check for timeout
		if ctx.Err() == context.DeadlineExceeded {
			if s.blockOnTimeout {
				return Result{Action: Block, Message: "webhook timeout", StageName: s.Name()}, nil
			}
			return Result{Action: Pass}, nil
		}
		if s.blockOnError {
			return Result{Action: Block, Message: fmt.Sprintf("webhook error: %v", err), StageName: s.Name()}, nil
		}
		return Result{Action: Pass}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if s.blockOnError {
			return Result{Action: Block, Message: fmt.Sprintf("webhook returned %d", resp.StatusCode), StageName: s.Name()}, nil
		}
		return Result{Action: Pass}, nil
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, providers.MaxProviderResponseSize))
	if err != nil {
		if s.blockOnError {
			return Result{Action: Block, Message: "webhook response read error", StageName: s.Name()}, nil
		}
		return Result{Action: Pass}, nil
	}

	var wr webhookResponse
	if err := json.Unmarshal(respBody, &wr); err != nil {
		if s.blockOnError {
			return Result{Action: Block, Message: "webhook invalid response", StageName: s.Name()}, nil
		}
		return Result{Action: Pass}, nil
	}

	switch wr.Action {
	case "block":
		msg := wr.Message
		if msg == "" {
			msg = "blocked by webhook"
		}
		return Result{Action: Block, Message: msg, StageName: s.Name()}, nil
	case "redact":
		if wr.Redacted != "" {
			return Result{Action: Redact, Message: wr.Redacted, StageName: s.Name()}, nil
		}
		return Result{Action: Pass}, nil
	default:
		return Result{Action: Pass}, nil
	}
}
