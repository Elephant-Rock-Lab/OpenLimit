package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// mockMCPServerStatus returns test data for MCP server tests.
func mockMCPServerStatus() []MCPServerEntry {
	return []MCPServerEntry{
		{Name: "filesystem", Status: "connected", Tools: 5, ToolList: []string{"read_file", "write_file", "list_dir", "search", "mkdir"}, LastError: ""},
		{Name: "github", Status: "connected", Tools: 3, ToolList: []string{"create_issue", "list_prs", "search_code"}, LastError: ""},
		{Name: "postgres", Status: "error", Tools: 0, ToolList: []string{}, LastError: "connection refused"},
	}
}

// ---------------------------------------------------------------------------
// TEST-53-01-01: Empty config returns empty list
// ---------------------------------------------------------------------------
func TestMCPServers_EmptyConfig(t *testing.T) {
	handler := MCPServersHandler(func() []MCPServerEntry { return nil })
	req := httptest.NewRequest("GET", "/admin/mcp/servers", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	servers, _ := resp["servers"].([]any)
	if servers != nil && len(servers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(servers))
	}
	total, _ := resp["total"].(float64)
	if total != 0 {
		t.Errorf("expected total=0, got %v", total)
	}
}

// ---------------------------------------------------------------------------
// TEST-53-01-02: Server status from provider
// ---------------------------------------------------------------------------
func TestMCPServers_ServerStatusFromProvider(t *testing.T) {
	handler := MCPServersHandler(func() []MCPServerEntry {
		return mockMCPServerStatus()
	})
	req := httptest.NewRequest("GET", "/admin/mcp/servers", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	servers := resp["servers"].([]any)
	if len(servers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(servers))
	}
	// Verify first server data
	fs := servers[0].(map[string]any)
	if fs["name"] != "filesystem" {
		t.Errorf("expected filesystem, got %v", fs["name"])
	}
	if fs["status"] != "connected" {
		t.Errorf("expected connected, got %v", fs["status"])
	}
	// Verify error server
	es := servers[2].(map[string]any)
	if es["status"] != "error" {
		t.Errorf("expected error, got %v", es["status"])
	}
}

// ---------------------------------------------------------------------------
// TEST-53-01-03: Tool list per server
// ---------------------------------------------------------------------------
func TestMCPServers_ToolListPerServer(t *testing.T) {
	handler := MCPServersHandler(func() []MCPServerEntry {
		return mockMCPServerStatus()
	})
	req := httptest.NewRequest("GET", "/admin/mcp/servers", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	servers := resp["servers"].([]any)

	fs := servers[0].(map[string]any)
	toolList := fs["tool_list"].([]any)
	if len(toolList) != 5 {
		t.Errorf("expected 5 tools for filesystem, got %d", len(toolList))
	}
	if toolList[0] != "read_file" {
		t.Errorf("expected read_file, got %v", toolList[0])
	}

	gs := servers[1].(map[string]any)
	ghTools := gs["tool_list"].([]any)
	if len(ghTools) != 3 {
		t.Errorf("expected 3 tools for github, got %d", len(ghTools))
	}
}

// ---------------------------------------------------------------------------
// TEST-53-01-04: Total and connected counts
// ---------------------------------------------------------------------------
func TestMCPServers_TotalAndConnectedCounts(t *testing.T) {
	handler := MCPServersHandler(func() []MCPServerEntry {
		return mockMCPServerStatus()
	})
	req := httptest.NewRequest("GET", "/admin/mcp/servers", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	total, _ := resp["total"].(float64)
	if total != 3 {
		t.Errorf("expected total=3, got %v", total)
	}
	connected, _ := resp["connected"].(float64)
	if connected != 2 {
		t.Errorf("expected connected=2, got %v", connected)
	}
}

// ---------------------------------------------------------------------------
// TEST-53-01-05: Tool list capped at 50
// ---------------------------------------------------------------------------
func TestMCPServers_ToolListCappedAt50(t *testing.T) {
	// Generate 60 tool names
	tools := make([]string, 60)
	for i := 0; i < 60; i++ {
		tools[i] = "tool_" + strings.Repeat("x", i%10+1)
	}

	handler := MCPServersHandler(func() []MCPServerEntry {
		return []MCPServerEntry{
			{Name: "bigserver", Status: "connected", Tools: 60, ToolList: tools[:50], LastError: ""},
		}
	})
	req := httptest.NewRequest("GET", "/admin/mcp/servers", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	servers := resp["servers"].([]any)
	bs := servers[0].(map[string]any)
	toolList := bs["tool_list"].([]any)
	if len(toolList) != 50 {
		t.Errorf("expected tool list capped at 50, got %d", len(toolList))
	}
}

// ---------------------------------------------------------------------------
// TEST-53-01-06: Method not allowed
// ---------------------------------------------------------------------------
func TestMCPServers_MethodNotAllowed(t *testing.T) {
	handler := MCPServersHandler(func() []MCPServerEntry {
		return mockMCPServerStatus()
	})
	req := httptest.NewRequest("POST", "/admin/mcp/servers", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// TEST-53-01-07: JSON response structure
// ---------------------------------------------------------------------------
func TestMCPServers_JSONResponseStructure(t *testing.T) {
	handler := MCPServersHandler(func() []MCPServerEntry {
		return mockMCPServerStatus()
	})
	req := httptest.NewRequest("GET", "/admin/mcp/servers", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Check required top-level fields
	for _, field := range []string{"servers", "total", "connected"} {
		if _, ok := resp[field]; !ok {
			t.Errorf("missing field %q in response", field)
		}
	}
}

// ---------------------------------------------------------------------------
// TEST-53-02-01: HTML contains data-panel="mcp" tab element
// ---------------------------------------------------------------------------
func TestMCPTab_ElementPresent(t *testing.T) {
	data, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Skipf("cannot read static/index.html: %v", err)
	}
	html := string(data)
	if !strings.Contains(html, `data-panel="mcp"`) {
		t.Error(`HTML missing data-panel="mcp" tab element`)
	}
}

// ---------------------------------------------------------------------------
// TEST-53-02-02: HTML contains loadMCP function definition
// ---------------------------------------------------------------------------
func TestMCPTab_LoadMCPFunctionExists(t *testing.T) {
	data, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Skipf("cannot read static/index.html: %v", err)
	}
	html := string(data)
	if !strings.Contains(html, "function loadMCP()") {
		t.Error("HTML missing loadMCP function definition")
	}
}

// ---------------------------------------------------------------------------
// TEST-53-02-03: HTML contains setInterval reference for auto-refresh
// ---------------------------------------------------------------------------
func TestMCPTab_AutoRefreshInterval(t *testing.T) {
	data, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Skipf("cannot read static/index.html: %v", err)
	}
	html := string(data)
	// The MCP auto-refresh uses setInterval with loadMCP
	if !strings.Contains(html, "setInterval(loadMCP") {
		t.Error("HTML missing setInterval(loadMCP) for auto-refresh")
	}
	// Timer cleanup on tab switch
	if !strings.Contains(html, "mcpTimer") {
		t.Error("HTML missing mcpTimer variable for cleanup")
	}
}
