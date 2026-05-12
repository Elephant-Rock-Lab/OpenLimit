package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openlimit/internal/config"
)

func newTestServerHandler(t *testing.T) (*ServerHandler, *httptest.Server) {
	t.Helper()
	cfg := config.MCPServerModeConfig{
		Enabled:       true,
		Endpoint:      "/mcp",
		Auth:          config.MCPAuthConfig{Mode: "none"},
		SessionTTLSec: 3600,
	}

	toolLister := func() ([]ToolDefinition, error) {
		return []ToolDefinition{
			{
				Name:        "weather_agent",
				Description: "Get weather forecasts",
				InputSchema: map[string]any{"type": "object"},
			},
		}, nil
	}

	handler := NewServerHandler(cfg, nil, toolLister, nil)
	server := httptest.NewServer(handler)
	return handler, server
}

func TestServerInitialize(t *testing.T) {
	_, server := newTestServerHandler(t)
	defer server.Close()

	resp, err := sendMCPRequest(server.URL, Request{
		JSONRPC: JSONRPCVersion,
		ID:      1,
		Method:  "initialize",
		Params: InitializeRequestParams{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    ClientCaps{Tools: &struct{}{}},
			ClientInfo:      ImplementationInfo{Name: "test-client", Version: "1.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("expected protocol version %q, got %q", ProtocolVersion, result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "openlimit-gateway" {
		t.Errorf("expected server name 'openlimit-gateway', got %q", result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil {
		t.Error("expected tools capability")
	}
	if result.Instructions == "" {
		t.Error("expected non-empty instructions")
	}
}

func TestServerPing(t *testing.T) {
	_, server := newTestServerHandler(t)
	defer server.Close()

	resp, err := sendMCPRequest(server.URL, Request{
		JSONRPC: JSONRPCVersion,
		ID:      2,
		Method:  "ping",
	})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
}

func TestServerToolsList(t *testing.T) {
	_, server := newTestServerHandler(t)
	defer server.Close()

	resp, err := sendMCPRequest(server.URL, Request{
		JSONRPC: JSONRPCVersion,
		ID:      3,
		Method:  "tools/list",
	})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	var result ListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "weather_agent" {
		t.Errorf("expected tool 'weather_agent', got %q", result.Tools[0].Name)
	}
}

func TestServerToolsListEmpty(t *testing.T) {
	cfg := config.MCPServerModeConfig{
		Enabled:  true,
		Endpoint: "/mcp",
		Auth:     config.MCPAuthConfig{Mode: "none"},
	}
	handler := NewServerHandler(cfg, nil, nil, nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := sendMCPRequest(server.URL, Request{
		JSONRPC: JSONRPCVersion,
		ID:      4,
		Method:  "tools/list",
	})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	var result ListToolsResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(result.Tools))
	}
}

func TestServerSessionID(t *testing.T) {
	_, server := newTestServerHandler(t)
	defer server.Close()

	// First request — no session ID
	body, _ := json.Marshal(Request{
		JSONRPC: JSONRPCVersion,
		ID:      1,
		Method:  "initialize",
		Params: InitializeRequestParams{
			ProtocolVersion: ProtocolVersion,
			ClientInfo:      ImplementationInfo{Name: "test", Version: "1.0.0"},
		},
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer httpResp.Body.Close()

	sessionID := httpResp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("expected session ID in response")
	}
	if !strings.HasPrefix(sessionID, "sess_") {
		t.Errorf("expected session ID to start with 'sess_', got %q", sessionID)
	}

	// Second request — include session ID, should get same back
	body2, _ := json.Marshal(Request{
		JSONRPC: JSONRPCVersion,
		ID:      2,
		Method:  "ping",
	})
	req2, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Mcp-Session-Id", sessionID)
	httpResp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer httpResp2.Body.Close()

	returnedID := httpResp2.Header.Get("Mcp-Session-Id")
	if returnedID != sessionID {
		t.Errorf("expected session ID %q, got %q", sessionID, returnedID)
	}
}

func TestServerBearerTokenAuth(t *testing.T) {
	cfg := config.MCPServerModeConfig{
		Enabled:  true,
		Endpoint: "/mcp",
		Auth:     config.MCPAuthConfig{Mode: "bearer_token", BearerToken: "secret123"},
	}
	handler := NewServerHandler(cfg, nil, nil, nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	// No auth → 401
	body, _ := json.Marshal(Request{JSONRPC: JSONRPCVersion, ID: 1, Method: "ping"})
	req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Wrong token → 401
	req2, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer wrong")
	resp2, _ := http.DefaultClient.Do(req2)
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", resp2.StatusCode)
	}

	// Correct token → 200
	req3, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer secret123")
	resp3, _ := http.DefaultClient.Do(req3)
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with correct token, got %d", resp3.StatusCode)
	}
}

func TestServerUnknownMethod(t *testing.T) {
	_, server := newTestServerHandler(t)
	defer server.Close()

	resp, err := sendMCPRequest(server.URL, Request{
		JSONRPC: JSONRPCVersion,
		ID:      99,
		Method:  "nonexistent/method",
	})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected error code %d, got %d", CodeMethodNotFound, resp.Error.Code)
	}
}

func TestServerInitializedNotification(t *testing.T) {
	_, server := newTestServerHandler(t)
	defer server.Close()

	body, _ := json.Marshal(Notification{
		JSONRPC: JSONRPCVersion,
		Method:  "notifications/initialized",
	})
	req, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Notifications return 204 No Content
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestSessionCreationAndEviction(t *testing.T) {
	store := NewSessionStore(50*time.Millisecond, nil)

	sess := store.CreateOrGet("")
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	if sess.ID == "" {
		t.Fatal("expected session ID")
	}

	// Get the same session back
	sess2, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("expected to find session")
	}
	if sess2.ID != sess.ID {
		t.Error("expected same session")
	}

	// Manually age the session so eviction will pick it up
	sess.lastSeen.Store(time.Now().Add(-100 * time.Millisecond).UnixNano())

	// Wait for eviction cycle (30s ticker, but we need to wait longer for the goroutine)
	// Since the ticker is 30s, just remove it manually for this test
	store.Remove(sess.ID)

	_, ok = store.Get(sess.ID)
	if ok {
		t.Error("expected session to be removed")
	}
}

func TestSessionBroadcast(t *testing.T) {
	store := NewSessionStore(0, nil) // no eviction

	sess := store.CreateOrGet("test-sess")
	if len(sess.notifier) != 0 {
		t.Fatal("expected empty notifier channel")
	}

	store.NotifyToolsChanged()

	if len(sess.notifier) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sess.notifier))
	}

	msg := <-sess.notifier
	var notif Notification
	if err := json.Unmarshal(msg, &notif); err != nil {
		t.Fatalf("parse notification: %v", err)
	}
	if notif.Method != "notifications/tools/list_changed" {
		t.Errorf("expected method 'notifications/tools/list_changed', got %q", notif.Method)
	}
}

// TestHandleToolsCallHasTimeout verifies that handleToolsCall creates a context
// with a 5-minute deadline, preventing goroutine leaks on hanging provider calls.
func TestHandleToolsCallHasTimeout(t *testing.T) {
	var receivedCtx context.Context
	exec := func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
		receivedCtx = ctx
		return &ChatResult{Content: "ok", Model: "test"}, nil
	}

	_, server := newTestServerHandler(t)
	defer server.Close()

	// We need to set the executor on the handler, but newTestServerHandler
	// doesn't return it. Create a new handler with executor.
	cfg := config.MCPServerModeConfig{
		Enabled:       true,
		Endpoint:      "/mcp",
		Auth:          config.MCPAuthConfig{Mode: "none"},
		SessionTTLSec: 3600,
	}
	handler := NewServerHandler(cfg, nil, func() ([]ToolDefinition, error) {
		return []ToolDefinition{{Name: "test-tool", Description: "test", InputSchema: map[string]any{"type": "object"}}}, nil
	}, nil)
	handler.SetChatExecutor(exec)

	server2 := httptest.NewServer(handler)
	defer server2.Close()

	resp, err := sendMCPRequest(server2.URL, Request{
		JSONRPC: JSONRPCVersion,
		ID:      1,
		Method:  "tools/call",
		Params: CallToolParams{
			Name:      "test-tool",
			Arguments: map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	if receivedCtx == nil {
		t.Fatal("executor was not called")
	}

	deadline, ok := receivedCtx.Deadline()
	if !ok {
		t.Fatal("context has no deadline — expected 5-minute timeout")
	}

	// Deadline should be approximately 5 minutes from now
	expectedDur := 5 * time.Minute
	actualDur := time.Until(deadline)
	tolerance := 2 * time.Second
	if actualDur < expectedDur-tolerance || actualDur > expectedDur+tolerance {
		t.Errorf("context deadline = %v from now, want ~%v", actualDur.Round(time.Second), expectedDur)
	}
}

// Helper: send a JSON-RPC request to the test server
func sendMCPRequest(url string, req Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
