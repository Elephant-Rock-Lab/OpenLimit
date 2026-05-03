package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// mockMCPServer creates an httptest server that simulates a basic MCP server.
func mockMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	sessionID := "test-session-123"
	tools := []ToolDefinition{
		{
			Name:        "get_forecast",
			Description: "Get weather forecast for a location",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{"type": "string"},
				},
				"required": []string{"location"},
			},
		},
		{
			Name:        "get_alerts",
			Description: "Get weather alerts for a region",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"region": map[string]any{"type": "string"},
				},
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Validate headers
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "invalid content type", http.StatusBadRequest)
			return
		}
		if pv := r.Header.Get("MCP-Protocol-Version"); pv == "" {
			http.Error(w, "missing MCP-Protocol-Version", http.StatusBadRequest)
			return
		}

		// Set session ID on first response
		mu.Lock()
		sid := sessionID
		mu.Unlock()
		w.Header().Set("Mcp-Session-Id", sid)
		w.Header().Set("Content-Type", "application/json")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "initialize":
			result := InitializeResult{
				ProtocolVersion: ProtocolVersion,
				Capabilities: ServerCaps{
					Tools: &ToolsCap{ListChanged: true},
				},
				ServerInfo: ImplementationInfo{
					Name:    "mock-mcp-server",
					Version: "1.0.0",
				},
			}
			resultBytes, _ := json.Marshal(result)
			resp := Response{
				JSONRPC: JSONRPCVersion,
				ID:      req.ID,
				Result:  resultBytes,
			}
			json.NewEncoder(w).Encode(resp)

		case "notifications/initialized":
			// No response for notifications
			w.WriteHeader(http.StatusNoContent)

		case "ping":
			resp := Response{
				JSONRPC: JSONRPCVersion,
				ID:      req.ID,
				Result:  json.RawMessage(`{}`),
			}
			json.NewEncoder(w).Encode(resp)

		case "tools/list":
			result := ListToolsResult{
				Tools: tools,
			}
			resultBytes, _ := json.Marshal(result)
			resp := Response{
				JSONRPC: JSONRPCVersion,
				ID:      req.ID,
				Result:  resultBytes,
			}
			json.NewEncoder(w).Encode(resp)

		case "tools/call":
			var params CallToolParams
			if p, ok := req.Params.(map[string]any); ok {
				if n, ok := p["name"].(string); ok {
					params.Name = n
				}
				if a, ok := p["arguments"].(map[string]any); ok {
					params.Arguments = a
				}
			}

			result := CallToolResult{
				Content: []ToolContent{
					{Type: "text", Text: "result for " + params.Name},
				},
			}
			resultBytes, _ := json.Marshal(result)
			resp := Response{
				JSONRPC: JSONRPCVersion,
				ID:      req.ID,
				Result:  resultBytes,
			}
			json.NewEncoder(w).Encode(resp)

		default:
			resp := Response{
				JSONRPC: JSONRPCVersion,
				ID:      req.ID,
				Error:   &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + req.Method},
			}
			json.NewEncoder(w).Encode(resp)
		}
	})

	return httptest.NewServer(mux)
}

func TestClientInitialize(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	client := NewClient("test", server.URL, nil, 5*time.Second, "weather", nil)
	ctx := context.Background()

	err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if !client.Ready() {
		t.Fatal("client should be ready after initialization")
	}

	// Verify session ID was captured
	if client.transport.SessionID() != "test-session-123" {
		t.Errorf("expected session ID 'test-session-123', got %q", client.transport.SessionID())
	}
}

func TestClientPing(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	client := NewClient("test", server.URL, nil, 5*time.Second, "weather", nil)
	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestClientListTools(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	client := NewClient("test", server.URL, nil, 5*time.Second, "weather", nil)
	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	tools := client.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// Check namespacing
	if tools[0].Name != "weather.get_forecast" {
		t.Errorf("expected tool name 'weather.get_forecast', got %q", tools[0].Name)
	}
	if tools[0].RawName != "get_forecast" {
		t.Errorf("expected raw name 'get_forecast', got %q", tools[0].RawName)
	}
	if tools[0].ServerName != "test" {
		t.Errorf("expected server name 'test', got %q", tools[0].ServerName)
	}

	if tools[1].Name != "weather.get_alerts" {
		t.Errorf("expected tool name 'weather.get_alerts', got %q", tools[1].Name)
	}
}

func TestClientCallTool(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	client := NewClient("test", server.URL, nil, 5*time.Second, "weather", nil)
	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	result, err := client.CallTool(ctx, "get_forecast", map[string]any{"location": "NYC"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if result.IsError {
		t.Error("result should not be an error")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "result for get_forecast" {
		t.Errorf("unexpected result text: %q", result.Content[0].Text)
	}
}

func TestClientCallToolWithRawName(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	client := NewClient("test", server.URL, nil, 5*time.Second, "weather", nil)
	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// CallTool uses the raw (un-prefixed) name
	result, err := client.CallTool(ctx, "get_alerts", map[string]any{"region": "east"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if result.Content[0].Text != "result for get_alerts" {
		t.Errorf("unexpected result text: %q", result.Content[0].Text)
	}
}

func TestClientRefreshTools(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	client := NewClient("test", server.URL, nil, 5*time.Second, "weather", nil)
	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Initial tools loaded during Initialize
	if len(client.Tools()) != 2 {
		t.Fatalf("expected 2 tools after init, got %d", len(client.Tools()))
	}

	// Refresh should succeed
	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	if len(client.Tools()) != 2 {
		t.Fatalf("expected 2 tools after refresh, got %d", len(client.Tools()))
	}
}

func TestClientCustomHeaders(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")

		var req Request
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			result := InitializeResult{
				ProtocolVersion: ProtocolVersion,
				Capabilities:    ServerCaps{},
				ServerInfo:      ImplementationInfo{Name: "test", Version: "1.0.0"},
			}
			resultBytes, _ := json.Marshal(result)
			json.NewEncoder(w).Encode(Response{JSONRPC: JSONRPCVersion, ID: req.ID, Result: resultBytes})
		} else {
			json.NewEncoder(w).Encode(Response{JSONRPC: JSONRPCVersion, ID: req.ID, Result: json.RawMessage(`{}`)})
		}
	}))
	defer server.Close()

	headers := map[string]string{"Authorization": "Bearer test-token-123"}
	client := NewClient("test", server.URL, headers, 5*time.Second, "test", nil)

	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if receivedAuth != "Bearer test-token-123" {
		t.Errorf("expected Authorization header 'Bearer test-token-123', got %q", receivedAuth)
	}
}

func TestClientConnectionFailure(t *testing.T) {
	client := NewClient("test", "http://localhost:1", nil, 1*time.Second, "test", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.Initialize(ctx)
	if err == nil {
		t.Fatal("expected connection failure, got nil error")
	}
}

func TestClientClose(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	client := NewClient("test", server.URL, nil, 5*time.Second, "weather", nil)
	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	client.Close()

	if client.Ready() {
		t.Fatal("client should not be ready after close")
	}
}

func TestTransportJSONRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req Request
		json.NewDecoder(r.Body).Decode(&req)

		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidParams, Message: "invalid params"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewStreamableHTTP(server.URL, nil, 5*time.Second, nil)
	ctx := context.Background()

	resp, err := transport.SendRequest(ctx, "some/method", nil)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("expected error code %d, got %d", CodeInvalidParams, resp.Error.Code)
	}
}

func TestTransportSessionIDPropagation(t *testing.T) {
	callCount := 0
	var sessionIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		sessionIDs = append(sessionIDs, r.Header.Get("Mcp-Session-Id"))

		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			w.Header().Set("Mcp-Session-Id", "session-abc")
		}

		var req Request
		json.NewDecoder(r.Body).Decode(&req)

		var result json.RawMessage
		switch req.Method {
		case "initialize":
			initResult, _ := json.Marshal(InitializeResult{
				ProtocolVersion: ProtocolVersion,
				Capabilities:    ServerCaps{},
				ServerInfo:      ImplementationInfo{Name: "test", Version: "1.0.0"},
			})
			result = initResult
		default:
			result = json.RawMessage(`{}`)
		}

		json.NewEncoder(w).Encode(Response{JSONRPC: JSONRPCVersion, ID: req.ID, Result: result})
	}))
	defer server.Close()

	transport := NewStreamableHTTP(server.URL, nil, 5*time.Second, nil)
	ctx := context.Background()

	// First request — no session ID yet
	_, _ = transport.SendRequest(ctx, "initialize", nil)

	// Second request — should include session ID from first response
	_, _ = transport.SendRequest(ctx, "ping", nil)

	if len(sessionIDs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(sessionIDs))
	}
	if sessionIDs[0] != "" {
		t.Errorf("first request should not have session ID, got %q", sessionIDs[0])
	}
	if sessionIDs[1] != "session-abc" {
		t.Errorf("second request should have session ID 'session-abc', got %q", sessionIDs[1])
	}
}
