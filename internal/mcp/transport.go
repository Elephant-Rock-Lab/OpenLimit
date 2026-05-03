package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StreamableHTTP implements MCP transport over HTTP with SSE for server-initiated messages.
// Spec: https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
type StreamableHTTP struct {
	url       string
	headers   map[string]string
	client    *http.Client
	sessionID string
	nextID    atomic.Int64
	logger    *slog.Logger

	// mu protects pending map
	mu      sync.Mutex
	pending map[int]chan *Response

	// notification handlers
	notifMu sync.Mutex
	notifCh chan *Notification

	// SSE listener management
	sseMu    sync.Mutex
	sseDone  chan struct{}
	sseStart chan struct{} // signaled when SSE goroutine is ready
}

// NewStreamableHTTP creates a new Streamable HTTP transport.
func NewStreamableHTTP(url string, headers map[string]string, timeout time.Duration, logger *slog.Logger) *StreamableHTTP {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &StreamableHTTP{
		url:      url,
		headers:  headers,
		client:   &http.Client{Timeout: timeout},
		pending:  make(map[int]chan *Response),
		notifCh:  make(chan *Notification, 64),
		sseStart: make(chan struct{}),
	}
}

// SendRequest sends a JSON-RPC request and waits for the response.
func (t *StreamableHTTP) SendRequest(ctx context.Context, method string, params any) (*Response, error) {
	id := int(t.nextID.Add(1))
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("MCP-Protocol-Version", ProtocolVersion)

	// Set session ID if we have one
	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()
	if sid != "" {
		httpReq.Header.Set("MCP-Session-Id", sid)
	}

	// Apply custom headers
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	// Register pending response channel
	respCh := make(chan *Response, 1)
	t.mu.Lock()
	t.pending[id] = respCh
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}()

	httpResp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	// Capture session ID from response
	if sid := httpResp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}

	contentType := httpResp.Header.Get("Content-Type")

	switch {
	case strings.Contains(contentType, "application/json"):
		// Single response
		var resp Response
		if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return &resp, nil

	case strings.Contains(contentType, "text/event-stream"):
		// SSE response — parse events until we find our response or context is done
		return t.readSSEResponse(ctx, httpResp.Body, id)

	default:
		// Try JSON anyway
		var resp Response
		if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
			return nil, fmt.Errorf("unexpected content type %q: %w", contentType, err)
		}
		return &resp, nil
	}
}

// SendNotification sends a JSON-RPC notification (no ID, no response expected).
func (t *StreamableHTTP) SendNotification(ctx context.Context, method string, params any) error {
	notif := Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("MCP-Protocol-Version", ProtocolVersion)

	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()
	if sid != "" {
		httpReq.Header.Set("MCP-Session-Id", sid)
	}

	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	resp.Body.Close()
	return nil
}

// Notifications returns a channel that receives server-initiated notifications.
func (t *StreamableHTTP) Notifications() <-chan *Notification {
	return t.notifCh
}

// SessionID returns the current session ID.
func (t *StreamableHTTP) SessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

// Close shuts down the transport.
func (t *StreamableHTTP) Close() {
	t.sseMu.Lock()
	defer t.sseMu.Unlock()
	if t.sseDone != nil {
		close(t.sseDone)
		t.sseDone = nil
	}
}

// readSSEResponse reads SSE events from the response body, looking for the response
// with the given ID. Notifications are forwarded to the notification channel.
func (t *StreamableHTTP) readSSEResponse(ctx context.Context, body io.Reader, expectedID int) (*Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var dataBuf strings.Builder

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()

		if line == "" {
			// End of event — process accumulated data
			if dataBuf.Len() > 0 {
				data := dataBuf.String()
				dataBuf.Reset()

				// Try to parse as response
				var base struct {
					JSONRPC string `json:"jsonrpc"`
					ID      *int   `json:"id"`
					Method  string `json:"method,omitempty"`
				}
				if err := json.Unmarshal([]byte(data), &base); err == nil {
					if base.ID != nil && *base.ID == expectedID {
						// This is our response
						var resp Response
						if err := json.Unmarshal([]byte(data), &resp); err != nil {
							return nil, fmt.Errorf("decode SSE response: %w", err)
						}
						return &resp, nil
					}
					if base.Method != "" && base.ID == nil {
						// This is a notification
						var notif Notification
						if err := json.Unmarshal([]byte(data), &notif); err == nil {
							select {
							case t.notifCh <- &notif:
							default:
								t.logger.Warn("notification channel full, dropping notification", "method", notif.Method)
							}
						}
					}
				}
			}
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			dataBuf.WriteString(line[6:])
		} else if strings.HasPrefix(line, "data:") {
			dataBuf.WriteString(line[5:])
		}
	}

	// Stream ended without finding our response — check if we accumulated data
	if dataBuf.Len() > 0 {
		data := dataBuf.String()
		var resp Response
		if err := json.Unmarshal([]byte(data), &resp); err == nil && resp.ID == expectedID {
			return &resp, nil
		}
	}

	return nil, fmt.Errorf("SSE stream ended without response for id %d", expectedID)
}
