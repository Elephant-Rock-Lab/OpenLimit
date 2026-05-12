package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// ────────────────────────────────────────────────────────────────
// Helper: custom mock MCP server for --live tests
// ────────────────────────────────────────────────────────────────

func newLiveMockHandler(tools []map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
						"name":    "mock-live-server",
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
		default:
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]any{},
			})
		}
	}
}

func writeTempConfig(t *testing.T, dir, serverURL string) string {
	t.Helper()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := fmt.Sprintf(
		"mcp:\n  servers:\n    - name: test-live\n      url: %s\n      timeout_ms: 5000\n      tool_prefix: test-live\n",
		serverURL,
	)
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return configPath
}

// ── TASK-01 Tests ───────────────────────────────────────────────

// TEST-52-01-01: Search returns results
func TestSearchReturnsResults(t *testing.T) {
	results := searchCatalog("file", defaultCatalog, "", 10)
	if len(results) == 0 {
		t.Error("expected non-empty results for 'file' query")
	}
}

// TEST-52-01-02: Results ranked by score (descending)
func TestResultsRankedByScore(t *testing.T) {
	results := searchCatalog("file", defaultCatalog, "", 10)
	if len(results) < 2 {
		t.Skip("need at least 2 results to verify ranking")
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].Score < results[i].Score {
			t.Errorf("results not ranked by score: result[%d].Score (%.1f) < result[%d].Score (%.1f)",
				i-1, results[i-1].Score, i, results[i].Score)
		}
	}
}

// TEST-52-01-03: Name matches weighted higher than description matches
func TestNameMatchesWeightedHigher(t *testing.T) {
	catalog := []CatalogEntry{
		{Name: "file_manager", Description: "something completely unrelated", Server: "test", Category: "utility"},
		{Name: "manager", Description: "file operations and file handling utilities", Server: "test", Category: "utility"},
	}
	results := searchCatalog("file", catalog, "", 10)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// "file_manager" should score higher (name match = 2.0) than "manager" (desc match only = 1.0)
	if results[0].Name != "file_manager" {
		t.Errorf("expected 'file_manager' to rank first, got %q (score: %.1f)", results[0].Name, results[0].Score)
	}
	// Verify actual scores
	var fileManagerScore, managerScore float64
	for _, r := range results {
		if r.Name == "file_manager" {
			fileManagerScore = r.Score
		}
		if r.Name == "manager" {
			managerScore = r.Score
		}
	}
	if fileManagerScore <= managerScore {
		t.Errorf("file_manager score (%.1f) should be > manager score (%.1f)", fileManagerScore, managerScore)
	}
}

// TEST-52-01-04: No results for nonsense query
func TestNoResultsForNonsense(t *testing.T) {
	results := searchCatalog("xyzzy123", defaultCatalog, "", 10)
	if len(results) != 0 {
		t.Errorf("expected empty results for nonsense query, got %d results", len(results))
	}
}

// TEST-52-01-05: --limit caps results
func TestLimitCapsResults(t *testing.T) {
	results := searchCatalog("file", defaultCatalog, "", 2)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

// TEST-52-01-06: JSON output format has query, results, total fields
func TestJSONOutputFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runSearch(&stdout, &stderr, []string{"file", "--format", "json"})
	if err != nil {
		t.Fatalf("runSearch failed: %v", err)
	}

	var resp SearchResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, stdout.String())
	}
	if resp.Query != "file" {
		t.Errorf("expected query 'file', got %q", resp.Query)
	}
	if len(resp.Results) == 0 {
		t.Error("expected non-empty results in JSON response")
	}
	if resp.Total != len(resp.Results) {
		t.Errorf("total field (%d) should equal len(results) (%d)", resp.Total, len(resp.Results))
	}
	// Verify result structure
	r := resp.Results[0]
	if r.Name == "" || r.Score == 0 || r.Server == "" || r.Category == "" {
		t.Errorf("result missing expected fields: %+v", r)
	}
}

// TEST-52-01-07: Catalog has 30+ entries
func TestCatalogHas30PlusEntries(t *testing.T) {
	if len(defaultCatalog) < 30 {
		t.Errorf("expected at least 30 catalog entries, got %d", len(defaultCatalog))
	}
}

// TEST-52-01-08: Case-insensitive search — "FILE" matches "read_file"
func TestCaseInsensitiveSearch(t *testing.T) {
	results := searchCatalog("FILE", defaultCatalog, "", 10)
	found := false
	for _, r := range results {
		if r.Name == "read_file" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'read_file' in results for uppercase query 'FILE'")
	}
}

// TEST-52-01-09: Multi-word query — "read file" matches "read_file"
func TestMultiWordQueryWorks(t *testing.T) {
	results := searchCatalog("read file", defaultCatalog, "", 10)
	found := false
	for _, r := range results {
		if r.Name == "read_file" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'read_file' in results for multi-word query 'read file'")
	}
	// Also verify "read_file" ranks high (both terms match name tokens)
	if len(results) > 0 && results[0].Name != "read_file" {
		t.Errorf("expected 'read_file' as top result, got %q (score: %.1f)", results[0].Name, results[0].Score)
	}
}

// TEST-52-01-10: Category filter — only "files" category returned
func TestCategoryFilter(t *testing.T) {
	results := searchCatalog("file", defaultCatalog, "files", 10)
	if len(results) == 0 {
		t.Fatal("expected results for 'file' query with category 'files'")
	}
	for _, r := range results {
		if r.Category != "files" {
			t.Errorf("expected all results to have category 'files', got %q for %q", r.Category, r.Name)
		}
	}
}

// ── TASK-02 Tests ───────────────────────────────────────────────

// TEST-52-02-01: --live merges server tools into search results
func TestLiveMergesServerTools(t *testing.T) {
	// Mock server with a unique tool NOT in the embedded catalog
	tools := []map[string]any{
		{"name": "custom_live_tool", "description": "A unique tool from the live server"},
	}
	server := httptest.NewServer(newLiveMockHandler(tools))
	defer server.Close()

	dir := t.TempDir()
	configPath := writeTempConfig(t, dir, server.URL)

	var stdout, stderr bytes.Buffer
	err := runSearch(&stdout, &stderr, []string{
		"custom", "--format", "json", "--live", "--config", configPath,
	})
	if err != nil {
		t.Fatalf("runSearch failed: %v\nstderr: %s", err, stderr.String())
	}

	var resp SearchResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, stdout.String())
	}

	found := false
	for _, r := range resp.Results {
		if r.Name == "custom_live_tool" {
			found = true
			if r.Server != "test-live" {
				t.Errorf("expected server 'test-live', got %q", r.Server)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected 'custom_live_tool' in results;\ngot %d results: %+v", len(resp.Results), resp.Results)
	}
}

// TEST-52-02-02: --live deduplicates by name (catalog wins over live)
func TestLiveDeduplication(t *testing.T) {
	// Mock server returns "read_file" which already exists in the embedded catalog
	tools := []map[string]any{
		{"name": "read_file", "description": "Read a file from mock server"},
	}
	server := httptest.NewServer(newLiveMockHandler(tools))
	defer server.Close()

	dir := t.TempDir()
	configPath := writeTempConfig(t, dir, server.URL)

	var stdout, stderr bytes.Buffer
	err := runSearch(&stdout, &stderr, []string{
		"read file", "--format", "json", "--live", "--config", configPath,
	})
	if err != nil {
		t.Fatalf("runSearch failed: %v\nstderr: %s", err, stderr.String())
	}

	var resp SearchResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, stdout.String())
	}

	// Count occurrences of "read_file" — should appear exactly once
	count := 0
	for _, r := range resp.Results {
		if r.Name == "read_file" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 'read_file' result (deduped), got %d", count)
	}
}
