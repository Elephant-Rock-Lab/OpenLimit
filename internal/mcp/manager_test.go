package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"openlimit/internal/config"
)

// newMockServer creates a mock MCP server with configurable behavior.
func newMockServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requestCount atomic.Int32
	tools := []ToolDefinition{
		{Name: "tool_a", Description: "Tool A", InputSchema: map[string]any{"type": "object"}},
		{Name: "tool_b", Description: "Tool B", InputSchema: map[string]any{"type": "object"}},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "mock-session")

		body, _ := io.ReadAll(r.Body)
		var req Request
		json.Unmarshal(body, &req)

		switch req.Method {
		case "initialize":
			result, _ := json.Marshal(InitializeResult{
				ProtocolVersion: ProtocolVersion,
				Capabilities:    ServerCaps{Tools: &ToolsCap{ListChanged: true}},
				ServerInfo:      ImplementationInfo{Name: "mock", Version: "1.0.0"},
			})
			json.NewEncoder(w).Encode(Response{JSONRPC: JSONRPCVersion, ID: req.ID, Result: result})
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
		case "ping":
			json.NewEncoder(w).Encode(Response{JSONRPC: JSONRPCVersion, ID: req.ID, Result: json.RawMessage(`{}`)})
		case "tools/list":
			result, _ := json.Marshal(ListToolsResult{Tools: tools})
			json.NewEncoder(w).Encode(Response{JSONRPC: JSONRPCVersion, ID: req.ID, Result: result})
		default:
			json.NewEncoder(w).Encode(Response{
				JSONRPC: JSONRPCVersion,
				ID:      req.ID,
				Error:   &RPCError{Code: CodeMethodNotFound, Message: "not found"},
			})
		}
	})

	return httptest.NewServer(mux), &requestCount
}

func TestRegistryAllTools(t *testing.T) {
	reg := NewRegistry()
	reg.ReplaceServerTools("server1", []Tool{
		{Name: "s1.tool_a", ServerName: "server1", RawName: "tool_a"},
		{Name: "s1.tool_b", ServerName: "server1", RawName: "tool_b"},
	})
	reg.ReplaceServerTools("server2", []Tool{
		{Name: "s2.tool_c", ServerName: "server2", RawName: "tool_c"},
	})

	tools := reg.AllTools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
}

func TestRegistryToolsByServer(t *testing.T) {
	reg := NewRegistry()
	reg.ReplaceServerTools("weather", []Tool{
		{Name: "weather.forecast", ServerName: "weather", RawName: "forecast"},
	})
	reg.ReplaceServerTools("github", []Tool{
		{Name: "github.create_issue", ServerName: "github", RawName: "create_issue"},
	})

	weatherTools := reg.ToolsByServer("weather")
	if len(weatherTools) != 1 {
		t.Fatalf("expected 1 weather tool, got %d", len(weatherTools))
	}
	if weatherTools[0].Name != "weather.forecast" {
		t.Errorf("expected 'weather.forecast', got %q", weatherTools[0].Name)
	}

	githubTools := reg.ToolsByServer("github")
	if len(githubTools) != 1 {
		t.Fatalf("expected 1 github tool, got %d", len(githubTools))
	}
}

func TestRegistryToolByName(t *testing.T) {
	reg := NewRegistry()
	reg.ReplaceServerTools("weather", []Tool{
		{Name: "weather.forecast", ServerName: "weather", RawName: "forecast", Description: "Get forecast"},
	})

	tool, ok := reg.ToolByName("weather.forecast")
	if !ok {
		t.Fatal("expected to find 'weather.forecast'")
	}
	if tool.Description != "Get forecast" {
		t.Errorf("expected description 'Get forecast', got %q", tool.Description)
	}

	_, ok = reg.ToolByName("nonexistent")
	if ok {
		t.Fatal("should not find nonexistent tool")
	}
}

func TestRegistryReplaceServerTools(t *testing.T) {
	reg := NewRegistry()
	reg.ReplaceServerTools("server1", []Tool{
		{Name: "s1.old_tool", ServerName: "server1", RawName: "old_tool"},
	})

	if reg.ToolCount() != 1 {
		t.Fatalf("expected 1 tool, got %d", reg.ToolCount())
	}

	// Replace with new tools
	reg.ReplaceServerTools("server1", []Tool{
		{Name: "s1.new_tool", ServerName: "server1", RawName: "new_tool"},
	})

	if reg.ToolCount() != 1 {
		t.Fatalf("expected 1 tool after replace, got %d", reg.ToolCount())
	}

	_, ok := reg.ToolByName("s1.old_tool")
	if ok {
		t.Fatal("old tool should be removed after replace")
	}

	_, ok = reg.ToolByName("s1.new_tool")
	if !ok {
		t.Fatal("new tool should exist after replace")
	}
}

func TestRegistryRemoveServerTools(t *testing.T) {
	reg := NewRegistry()
	reg.ReplaceServerTools("server1", []Tool{
		{Name: "s1.tool_a", ServerName: "server1"},
	})
	reg.ReplaceServerTools("server2", []Tool{
		{Name: "s2.tool_b", ServerName: "server2"},
	})

	if reg.ToolCount() != 2 {
		t.Fatalf("expected 2 tools, got %d", reg.ToolCount())
	}

	reg.RemoveServerTools("server1")
	if reg.ToolCount() != 1 {
		t.Fatalf("expected 1 tool after remove, got %d", reg.ToolCount())
	}

	_, ok := reg.ToolByName("s1.tool_a")
	if ok {
		t.Fatal("server1 tool should be removed")
	}
	_, ok = reg.ToolByName("s2.tool_b")
	if !ok {
		t.Fatal("server2 tool should still exist")
	}
}

func TestManagerStartAndStatus(t *testing.T) {
	server, _ := newMockServer(t)
	defer server.Close()

	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{Name: "mock-server", URL: server.URL, ToolPrefix: "mock"},
		},
	}, registry, nil)

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer mgr.Stop()

	time.Sleep(100 * time.Millisecond) // let it connect

	status := mgr.ServerStatus()
	if len(status) != 1 {
		t.Fatalf("expected 1 server status, got %d", len(status))
	}
	if status[0].Status != "connected" {
		t.Errorf("expected status 'connected', got %q", status[0].Status)
	}
	if status[0].Tools != 2 {
		t.Errorf("expected 2 tools, got %d", status[0].Tools)
	}

	// Check registry
	if registry.ToolCount() != 2 {
		t.Fatalf("expected 2 tools in registry, got %d", registry.ToolCount())
	}
}

func TestManagerMultipleServers(t *testing.T) {
	server1, _ := newMockServer(t)
	defer server1.Close()
	server2, _ := newMockServer(t)
	defer server2.Close()

	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{Name: "server1", URL: server1.URL, ToolPrefix: "s1"},
			{Name: "server2", URL: server2.URL, ToolPrefix: "s2"},
		},
	}, registry, nil)

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer mgr.Stop()

	time.Sleep(100 * time.Millisecond)

	if registry.ToolCount() != 4 {
		t.Fatalf("expected 4 tools (2 per server), got %d", registry.ToolCount())
	}

	s1Tools := registry.ToolsByServer("server1")
	if len(s1Tools) != 2 {
		t.Fatalf("expected 2 tools for server1, got %d", len(s1Tools))
	}

	// Verify namespacing
	_, ok := registry.ToolByName("s1.tool_a")
	if !ok {
		t.Fatal("expected to find 's1.tool_a'")
	}
	_, ok = registry.ToolByName("s2.tool_a")
	if !ok {
		t.Fatal("expected to find 's2.tool_a'")
	}
}

func TestManagerDisabledMCP(t *testing.T) {
	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{Enabled: false}, registry, nil)

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start should not fail when disabled: %v", err)
	}

	if registry.ToolCount() != 0 {
		t.Fatal("registry should be empty when MCP is disabled")
	}
}

func TestManagerDisconnectedServer(t *testing.T) {
	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{Name: "bad-server", URL: "http://localhost:1", ToolPrefix: "bad"},
		},
	}, registry, nil)

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start should not fail on connection error: %v", err)
	}
	defer mgr.Stop()

	time.Sleep(100 * time.Millisecond)

	status := mgr.ServerStatus()
	if len(status) != 1 {
		t.Fatalf("expected 1 server status, got %d", len(status))
	}
	if status[0].Status != "disconnected" {
		t.Errorf("expected status 'disconnected', got %q", status[0].Status)
	}

	// Reconnect attempt happens in background health checks (30s interval)
	// Not testing that here since it would require waiting too long
}

func TestToolPrefixNamespacing(t *testing.T) {
	server, _ := newMockServer(t)
	defer server.Close()

	registry := NewRegistry()
	mgr := NewManager(config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{Name: "myservice", URL: server.URL, ToolPrefix: "myservice"},
		},
	}, registry, nil)

	ctx := context.Background()
	mgr.Start(ctx)
	defer mgr.Stop()

	time.Sleep(100 * time.Millisecond)

	tools := registry.AllTools()
	for _, tool := range tools {
		if tool.ServerName != "myservice" {
			t.Errorf("expected server name 'myservice', got %q", tool.ServerName)
		}
	}

	_, ok := registry.ToolByName("myservice.tool_a")
	if !ok {
		t.Fatal("expected namespaced tool 'myservice.tool_a'")
	}
}
