package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openlimit/internal/config"
)

// ── Mock MCP Server ─────────────────────────────────────────────

type mockOptions struct {
	failPing bool
	delay    time.Duration
}

func newMockHandler(opts mockOptions) http.HandlerFunc {
	tools := []map[string]any{
		{"name": "read_file", "description": "Read a file"},
		{"name": "write_file", "description": "Write a file"},
		{"name": "search", "description": "Search files"},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if opts.delay > 0 {
			time.Sleep(opts.delay)
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)
		method, _ := req["method"].(string)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "test-session-id")

		switch method {
		case "initialize":
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"protocolVersion": "2025-11-25",
					"capabilities":    map[string]any{},
					"serverInfo": map[string]any{
						"name":    "mock-mcp-server",
						"version": "1.0.0",
					},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": tools,
				},
			})
		case "ping":
			if opts.failPing {
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req["id"],
					"error": map[string]any{
						"code":    -32603,
						"message": "ping not supported",
					},
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]any{},
			})
		default:
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error": map[string]any{
					"code":    -32601,
					"message": "method not found: " + method,
				},
			})
		}
	}
}

func mockMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(newMockHandler(mockOptions{}))
}

func mockMCPServerWithPingFail(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(newMockHandler(mockOptions{failPing: true}))
}

func mockMCPServerWithDelay(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(newMockHandler(mockOptions{delay: delay}))
}

// ── TASK-01 Tests ───────────────────────────────────────────────

// TEST-51-01-01: isValidName rejects empty and special chars
func TestIsValidName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", false},
		{"spaces", "my server", false},
		{"special char", "server!", false},
		{"underscore", "my_server", false},
		{"leading hyphen", "-server", false},
		{"at symbol", "server@name", false},
		{"dots", "server.name", false},
		{"slash", "server/name", false},
		{"simple alphanumeric", "server1", true},
		{"hyphenated", "my-server", true},
		{"single char", "A", true},
		{"numbers", "test123", true},
		{"multi hyphen", "a-b-c", true},
		{"starts with number", "1server", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidName(tt.input)
			if got != tt.want {
				t.Errorf("isValidName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TEST-51-01-02: URL must start with http:// or https://
func TestURLValidation(t *testing.T) {
	t.Run("empty url", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runAdd(&stdout, &stderr, []string{"my-server"})
		if err == nil {
			t.Fatal("expected error for empty URL")
		}
		if !strings.Contains(err.Error(), "--url is required") {
			t.Errorf("expected '--url is required' error, got: %v", err)
		}
	})

	t.Run("missing protocol", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runAdd(&stdout, &stderr, []string{"my-server", "--url", "localhost:3001"})
		if err == nil {
			t.Fatal("expected error for missing protocol")
		}
		if !strings.Contains(err.Error(), "must start with http:// or https://") {
			t.Errorf("expected invalid URL error, got: %v", err)
		}
	})

	t.Run("ftp protocol", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runAdd(&stdout, &stderr, []string{"my-server", "--url", "ftp://example.com"})
		if err == nil {
			t.Fatal("expected error for ftp protocol")
		}
		if !strings.Contains(err.Error(), "must start with http:// or https://") {
			t.Errorf("expected invalid URL error, got: %v", err)
		}
	})

	t.Run("valid http", func(t *testing.T) {
		// Use a mock server to verify http:// passes URL validation
		server := mockMCPServer(t)
		defer server.Close()
		dir := t.TempDir()
		var stdout, stderr bytes.Buffer
		err := runAdd(&stdout, &stderr, []string{
			"test-srv", "--url", server.URL, "--config", filepath.Join(dir, "config.yaml"),
		})
		if err != nil {
			t.Fatalf("expected success for http URL, got: %v", err)
		}
	})

	t.Run("valid https", func(t *testing.T) {
		// Just check URL validation passes (can't connect to https without a real server)
		// The URL validation is the same check for http and https prefixes
		var stdout, stderr bytes.Buffer
		err := runAdd(&stdout, &stderr, []string{
			"test-srv", "--url", "https://example.com/mcp", "--config", "nonexistent.yaml",
		})
		// Should get past URL validation, may fail on connect — that's fine
		if err != nil && strings.Contains(err.Error(), "must start with http") {
			t.Errorf("https URL should pass URL validation, got: %v", err)
		}
	})
}

// TEST-51-01-03: Duplicate name rejected
func TestDuplicateNameRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	yamlContent := "mcp:\n  servers:\n    - name: my-server\n      url: http://localhost:3001\n      timeout_ms: 5000\n      tool_prefix: my-server\n"
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runAdd(&stdout, &stderr, []string{
		"my-server", "--url", "http://localhost:3002", "--config", configPath,
	})
	if err == nil {
		t.Fatal("expected error for duplicate server name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

// TEST-51-01-04: Connect and discover tools from mock server
func TestConnectAndDiscoverTools(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	var stdout, stderr bytes.Buffer
	err := runAdd(&stdout, &stderr, []string{
		"test-srv", "--url", server.URL, "--config", configPath,
	})
	if err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Connected") {
		t.Errorf("expected 'Connected' in output, got: %s", output)
	}
	if !strings.Contains(output, "Discovered 3 tools") {
		t.Errorf("expected 'Discovered 3 tools' in output, got: %s", output)
	}
}

// TEST-51-01-05: Tool names appear in output
func TestToolNamesInOutput(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	var stdout, stderr bytes.Buffer
	err := runAdd(&stdout, &stderr, []string{
		"test-srv", "--url", server.URL, "--config", configPath,
	})
	if err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	output := stdout.String()
	// Tools are namespaced with prefix
	for _, name := range []string{"test-srv.read_file", "test-srv.write_file", "test-srv.search"} {
		if !strings.Contains(output, name) {
			t.Errorf("expected tool name %q in output, got: %s", name, output)
		}
	}
}

// TEST-51-01-06: Dry run doesn't write file
func TestDryRunNoWrite(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	var stdout, stderr bytes.Buffer
	err := runAdd(&stdout, &stderr, []string{
		"test-srv", "--url", server.URL, "--config", configPath, "--dry-run",
	})
	if err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	// Config file should not exist
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Error("expected config file to not exist after dry run")
	}

	output := stdout.String()
	if !strings.Contains(output, "dry run") {
		t.Errorf("expected 'dry run' in output, got: %s", output)
	}
}

// TEST-51-01-07: Writes to config file (verify by reloading)
func TestWritesToConfig(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	var stdout, stderr bytes.Buffer
	err := runAdd(&stdout, &stderr, []string{
		"test-srv", "--url", server.URL, "--config", configPath,
	})
	if err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	// Verify config by reloading
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	found := false
	for _, s := range cfg.MCP.Servers {
		if s.Name == "test-srv" {
			found = true
			if s.URL != server.URL {
				t.Errorf("expected URL %q, got %q", server.URL, s.URL)
			}
			if s.ToolPrefix != "test-srv" {
				t.Errorf("expected ToolPrefix 'test-srv', got %q", s.ToolPrefix)
			}
			break
		}
	}
	if !found {
		t.Error("server 'test-srv' not found in reloaded config")
	}
}

// TEST-51-01-08: Creates config if not exists
func TestCreatesConfigIfNotExists(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// Verify file doesn't exist
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("expected config file to not exist initially")
	}

	var stdout, stderr bytes.Buffer
	err := runAdd(&stdout, &stderr, []string{
		"test-srv", "--url", server.URL, "--config", configPath,
	})
	if err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	// File should now exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("expected config file to exist after add")
	}

	// Should be valid YAML loadable by config.Load
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load created config: %v", err)
	}
	if len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Name != "test-srv" {
		t.Errorf("expected 1 server named 'test-srv', got %v", cfg.MCP.Servers)
	}
}

// TEST-51-01-09: Ping validates server
func TestPingValidatesServer(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := mockMCPServer(t)
		defer server.Close()

		dir := t.TempDir()
		var stdout, stderr bytes.Buffer
		err := runAdd(&stdout, &stderr, []string{
			"test-srv", "--url", server.URL, "--config", filepath.Join(dir, "config.yaml"), "--ping",
		})
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if !strings.Contains(stdout.String(), "Ping OK") {
			t.Errorf("expected 'Ping OK' in output, got: %s", stdout.String())
		}
	})

	t.Run("failure", func(t *testing.T) {
		server := mockMCPServerWithPingFail(t)
		defer server.Close()

		dir := t.TempDir()
		var stdout, stderr bytes.Buffer
		err := runAdd(&stdout, &stderr, []string{
			"test-srv", "--url", server.URL, "--config", filepath.Join(dir, "config.yaml"), "--ping",
		})
		if err == nil {
			t.Fatal("expected error when ping fails")
		}
		if !strings.Contains(err.Error(), "ping failed") {
			t.Errorf("expected 'ping failed' in error, got: %v", err)
		}
	})
}

// TEST-51-01-10: Timeout respected
func TestTimeoutRespected(t *testing.T) {
	// Server delays 3 seconds, client timeout is 1 second
	server := mockMCPServerWithDelay(t, 3*time.Second)
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	var stdout, stderr bytes.Buffer
	err := runAdd(&stdout, &stderr, []string{
		"test-srv", "--url", server.URL, "--config", configPath, "--timeout", "1",
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to connect") {
		t.Errorf("expected connection/timeout error, got: %v", err)
	}
}

// TEST-51-01-11: Custom config path works
func TestCustomConfigPath(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	dir := t.TempDir()
	customPath := filepath.Join(dir, "my-custom-config.yaml")

	var stdout, stderr bytes.Buffer
	err := runAdd(&stdout, &stderr, []string{
		"test-srv", "--url", server.URL, "--config", customPath,
	})
	if err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	// Verify config was written to the custom path
	if _, err := os.Stat(customPath); os.IsNotExist(err) {
		t.Fatal("expected config file at custom path")
	}

	output := stdout.String()
	if !strings.Contains(output, customPath) {
		t.Errorf("expected custom path %q in output, got: %s", customPath, output)
	}
}

// TEST-51-01-12: Tool prefix from flag
func TestToolPrefixFromFlag(t *testing.T) {
	server := mockMCPServer(t)
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	var stdout, stderr bytes.Buffer
	err := runAdd(&stdout, &stderr, []string{
		"test-srv", "--url", server.URL, "--config", configPath, "--prefix", "custom",
	})
	if err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	// Verify prefix in config
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}
	found := false
	for _, s := range cfg.MCP.Servers {
		if s.Name == "test-srv" {
			found = true
			if s.ToolPrefix != "custom" {
				t.Errorf("expected ToolPrefix 'custom', got %q", s.ToolPrefix)
			}
		}
	}
	if !found {
		t.Error("server 'test-srv' not found in config")
	}

	// Verify tool names in output use the custom prefix
	output := stdout.String()
	if !strings.Contains(output, "custom.read_file") {
		t.Errorf("expected 'custom.read_file' in output, got: %s", output)
	}
}

// ── TASK-02 Tests ───────────────────────────────────────────────

// TEST-51-02-01: appendServer adds to existing config
func TestAppendServerToExisting(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// Write initial config with one server
	yamlContent := "mcp:\n  servers:\n    - name: existing\n      url: http://localhost:3001\n      timeout_ms: 5000\n      tool_prefix: existing\n"
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Load and append
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendServerToConfig(configPath, cfg, config.MCPServerConfig{
		Name: "new-server", URL: "http://localhost:3002", TimeoutMS: 3000, ToolPrefix: "new-server",
	}); err != nil {
		t.Fatal(err)
	}

	// Reload and verify
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to reload: %v", err)
	}
	if len(reloaded.MCP.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(reloaded.MCP.Servers))
	}
	names := make(map[string]bool)
	for _, s := range reloaded.MCP.Servers {
		names[s.Name] = true
	}
	if !names["existing"] {
		t.Error("expected 'existing' server in config")
	}
	if !names["new-server"] {
		t.Error("expected 'new-server' in config")
	}
}

// TEST-51-02-02: appendServer creates new file
func TestAppendServerCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// File doesn't exist — config.Load returns defaults
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := appendServerToConfig(configPath, cfg, config.MCPServerConfig{
		Name: "first", URL: "http://localhost:3001", TimeoutMS: 5000, ToolPrefix: "first",
	}); err != nil {
		t.Fatal(err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("expected config file to be created")
	}

	// Verify contents
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to reload: %v", err)
	}
	if len(reloaded.MCP.Servers) != 1 || reloaded.MCP.Servers[0].Name != "first" {
		t.Errorf("expected 1 server named 'first', got %v", reloaded.MCP.Servers)
	}
}

// TEST-51-02-03: Preserves existing config fields
func TestPreservesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// Write config with specific non-default values
	yamlContent := "server:\n  port: 9090\nlogging:\n  level: debug\nmcp:\n  servers:\n    - name: existing\n      url: http://localhost:3001\n      timeout_ms: 5000\n      tool_prefix: existing\n"
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := appendServerToConfig(configPath, cfg, config.MCPServerConfig{
		Name: "added", URL: "http://localhost:4000", TimeoutMS: 3000, ToolPrefix: "added",
	}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to reload: %v", err)
	}

	// Verify preserved fields
	if reloaded.Server.Port != 9090 {
		t.Errorf("server.port: got %d, want 9090", reloaded.Server.Port)
	}
	if reloaded.Logging.Level != "debug" {
		t.Errorf("logging.level: got %q, want 'debug'", reloaded.Logging.Level)
	}

	// Verify both servers present
	if len(reloaded.MCP.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(reloaded.MCP.Servers))
	}
	names := make(map[string]bool)
	for _, s := range reloaded.MCP.Servers {
		names[s.Name] = true
	}
	if !names["existing"] || !names["added"] {
		t.Errorf("expected 'existing' and 'added' servers, got: %v", names)
	}
}

// TEST-51-02-04: Sequential adds accumulate
func TestSequentialAddsAccumulate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// First add
	cfg1, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendServerToConfig(configPath, cfg1, config.MCPServerConfig{
		Name: "first", URL: "http://localhost:3001", TimeoutMS: 5000, ToolPrefix: "first",
	}); err != nil {
		t.Fatal(err)
	}

	// Second add — must reload to get updated state
	cfg2, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendServerToConfig(configPath, cfg2, config.MCPServerConfig{
		Name: "second", URL: "http://localhost:3002", TimeoutMS: 5000, ToolPrefix: "second",
	}); err != nil {
		t.Fatal(err)
	}

	// Reload and verify both accumulated
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to reload: %v", err)
	}
	if len(reloaded.MCP.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(reloaded.MCP.Servers))
	}
	names := make(map[string]bool)
	for _, s := range reloaded.MCP.Servers {
		names[s.Name] = true
	}
	if !names["first"] {
		t.Error("expected 'first' server in config")
	}
	if !names["second"] {
		t.Error("expected 'second' server in config")
	}
}
