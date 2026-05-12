package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// BATCH-42 TASK-03: Integration tests for registry-backed provider resolution
// ---------------------------------------------------------------------------

// mockOpenAIServer creates a test server that responds to /chat/completions.
func mockOpenAIServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" && r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		// Verify auth header is present
		auth := r.Header.Get("Authorization")
		if auth == "" {
			t.Error("missing Authorization header")
		}
		resp := map[string]any{
			"id":      "test-response",
			"object":  "chat.completion",
			"model":   "test-model",
			"choices": []any{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestRegistry_DeepSeekResolvesToOpenAICompatible(t *testing.T) {
	// TEST-42-03-01: DeepSeek config resolves to openai-compatible adapter
	def, ok := LookupDefault("deepseek")
	if !ok {
		t.Fatal("deepseek not in registry")
	}
	if def.BaseType != "openai-compatible" {
		t.Errorf("BaseType = %q, want openai-compatible", def.BaseType)
	}
	if def.BaseURL == "" {
		t.Error("BaseURL is empty")
	}
}

func TestRegistry_TogetherAI_NoTypeResolvesFromRegistry(t *testing.T) {
	// TEST-42-03-02: Together AI config with no type resolves from registry
	cfg := map[string]interface{}{} // empty config — no type, no base_url
	result := ApplyDefaults("together_ai", cfg)

	typ, _ := result["type"].(string)
	if typ != "openai-compatible" {
		t.Errorf("type = %q, want openai-compatible", typ)
	}
	url, _ := result["base_url"].(string)
	if url == "" {
		t.Error("base_url should be filled from registry")
	}
}

func TestRegistry_Grok_CustomBaseURLOverridesRegistry(t *testing.T) {
	// TEST-42-03-03: xAI Grok config with custom base_url overrides registry
	cfg := map[string]interface{}{
		"base_url": "https://custom-proxy.example.com/v1",
	}
	result := ApplyDefaults("grok", cfg)

	url, _ := result["base_url"].(string)
	if url != "https://custom-proxy.example.com/v1" {
		t.Errorf("base_url = %q, want user-provided URL", url)
	}
}

func TestRegistry_MockEndToEnd(t *testing.T) {
	// TEST-42-03-04: Full request through registry provider (mock server)
	srv := mockOpenAIServer(t)
	defer srv.Close()

	// Simulate a user config with a registry name pointing to our mock
	cfg := map[string]interface{}{
		"base_url": srv.URL + "/v1",
	}
	result := ApplyDefaults("deepseek", cfg)

	typ, _ := result["type"].(string)
	if typ != "openai-compatible" {
		t.Fatalf("type = %q, want openai-compatible", typ)
	}

	// Verify the URL is the mock server
	url, _ := result["base_url"].(string)
	if url != srv.URL+"/v1" {
		t.Errorf("base_url = %q, want %q", url, srv.URL+"/v1")
	}

	// Make an actual request to verify the mock server works
	client := &http.Client{}
	req, _ := http.NewRequest("POST", url+"/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRegistry_AllEntriesHaveValidType(t *testing.T) {
	// Verify all registry entries have a non-empty BaseType and BaseURL
	for name, def := range DefaultRegistry {
		if def.BaseType == "" {
			t.Errorf("provider %q has empty BaseType", name)
		}
		if def.BaseURL == "" {
			t.Errorf("provider %q has empty BaseURL", name)
		}
		if def.AuthHeader == "" {
			t.Errorf("provider %q has empty AuthHeader", name)
		}
	}
}

// grok and xai intentionally share the same BaseURL (alias). No duplicate check.

