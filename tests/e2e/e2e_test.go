package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/providers"
	"openlimit/internal/server"
)

// ---------------------------------------------------------------------------
// BATCH-44: E2E integration tests for registry-backed provider resolution
// ---------------------------------------------------------------------------

// mockProviderServer creates a test server simulating an OpenAI-compatible API.
func mockProviderServer(t *testing.T, expectedAuth string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if expectedAuth != "" && auth != expectedAuth {
			t.Errorf("auth header = %q, want %q", auth, expectedAuth)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var reqBody map[string]any
		if err := json.Unmarshal(body, &reqBody); err != nil {
			t.Errorf("invalid JSON body: %v", err)
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		resp := map[string]any{
			"id":      "chatcmpl-e2e-test",
			"object":  "chat.completion",
			"created": float64(time.Now().Unix()),
			"model":   reqBody["model"],
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "E2E test response"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

// mockStreamingServer returns SSE streaming responses.
func mockStreamingServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)
		chunks := []string{
			`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hello"},"index":0}]}`,
			`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"delta":{"content":" world"},"index":0}]}`,
			`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop","index":0}]}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

// mockErrorServer always returns 500.
func mockErrorServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "internal server error from provider",
				"type":    "internal_error",
			},
		})
	}))
}

// setupGateway creates a gateway with the given provider and returns its URL.
func setupGateway(t *testing.T, providerName string, providerConfig config.ProviderConfig) string {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := config.Default()
	cfg.Server.Port = 0
	cfg.Logging.Level = "error"
	cfg.Auth.Enabled = false
	cfg.Server.MaxBodySizeKB = 1024
	cfg.Providers = map[string]config.ProviderConfig{
		providerName: providerConfig,
	}
	cfg.Models = map[string]config.ModelConfig{
		"test-model": {Routes: []config.ModelRoute{
			{Provider: providerName, Model: "test-model", Weight: 100},
		}},
	}

	runtime := server.NewRuntime(cfg, logger, nil)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := &http.Server{Handler: runtime.Server.Handler}
	go func() { srv.Serve(listener) }()
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	return fmt.Sprintf("http://%s", listener.Addr())
}

func sendChatRequest(t *testing.T, gwURL string) *http.Response {
	t.Helper()
	body := strings.NewReader(`{"model":"test-model","messages":[{"role":"user","content":"hello"}]}`)
	req, err := http.NewRequest(http.MethodPost, gwURL+"/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// TEST-44-01-01 through TEST-44-01-05: Five provider E2E tests
// ---------------------------------------------------------------------------

func TestE2E_DeepSeek(t *testing.T) {
	mock := mockProviderServer(t, "")
	defer mock.Close()

	cfg := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: mock.URL,
		Keys:    []config.ProviderKeyConfig{{ID: "ds", Value: "ds-key", Weight: 100}},
	}

	// Verify registry resolution
	defaults := providers.ApplyDefaults("deepseek", map[string]interface{}{"base_url": mock.URL})
	if typ, _ := defaults["type"].(string); typ != "openai-compatible" {
		t.Fatalf("registry type = %q, want openai-compatible", typ)
	}

	gwURL := setupGateway(t, "deepseek", cfg)
	resp := sendChatRequest(t, gwURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", result["object"])
	}
}

func TestE2E_TogetherAI(t *testing.T) {
	mock := mockProviderServer(t, "")
	defer mock.Close()

	cfg := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: mock.URL,
		Keys:    []config.ProviderKeyConfig{{ID: "together", Value: "together-key", Weight: 100}},
	}

	gwURL := setupGateway(t, "together_ai", cfg)
	resp := sendChatRequest(t, gwURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestE2E_Grok(t *testing.T) {
	mock := mockProviderServer(t, "")
	defer mock.Close()

	cfg := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: mock.URL,
		Keys:    []config.ProviderKeyConfig{{ID: "grok", Value: "grok-key", Weight: 100}},
	}

	gwURL := setupGateway(t, "grok", cfg)
	resp := sendChatRequest(t, gwURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestE2E_FireworksAI(t *testing.T) {
	mock := mockProviderServer(t, "")
	defer mock.Close()

	cfg := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: mock.URL,
		Keys:    []config.ProviderKeyConfig{{ID: "fw", Value: "fw-key", Weight: 100}},
	}

	gwURL := setupGateway(t, "fireworks_ai", cfg)
	resp := sendChatRequest(t, gwURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestE2E_Perplexity(t *testing.T) {
	mock := mockProviderServer(t, "")
	defer mock.Close()

	cfg := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: mock.URL,
		Keys:    []config.ProviderKeyConfig{{ID: "pplx", Value: "pplx-key", Weight: 100}},
	}

	gwURL := setupGateway(t, "perplexity", cfg)
	resp := sendChatRequest(t, gwURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

// ---------------------------------------------------------------------------
// TEST-44-01-06: Auth header forwarded
// ---------------------------------------------------------------------------

func TestE2E_AuthHeaderForwarded(t *testing.T) {
	var receivedAuth string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "test", "object": "chat.completion", "choices": []any{},
		})
	}))
	defer mock.Close()

	cfg := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: mock.URL,
		Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "my-secret-key", Weight: 100}},
	}

	gwURL := setupGateway(t, "deepseek", cfg)
	resp := sendChatRequest(t, gwURL)
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if receivedAuth != "Bearer my-secret-key" {
		t.Errorf("provider received auth = %q, want %q", receivedAuth, "Bearer my-secret-key")
	}
}

// ---------------------------------------------------------------------------
// TEST-44-01-07: Provider 500 → gateway 502
// ---------------------------------------------------------------------------

func TestE2E_ProviderErrorForwarded(t *testing.T) {
	mock := mockErrorServer(t)
	defer mock.Close()

	cfg := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: mock.URL,
		Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "test-key", Weight: 100}},
	}

	gwURL := setupGateway(t, "cerebras", cfg)
	resp := sendChatRequest(t, gwURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// TEST-44-01-08: Case-insensitive provider name
// ---------------------------------------------------------------------------

func TestE2E_CaseInsensitiveName(t *testing.T) {
	mock := mockProviderServer(t, "")
	defer mock.Close()

	for _, name := range []string{"DeepSeek", "deepseek", "DEEPSEEK"} {
		t.Run(name, func(t *testing.T) {
			cfg := config.ProviderConfig{
				Type:    "openai-compatible",
				BaseURL: mock.URL,
				Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "test-key", Weight: 100}},
			}

			gwURL := setupGateway(t, name, cfg)
			resp := sendChatRequest(t, gwURL)
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("name=%q: status = %d", name, resp.StatusCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TEST-44-01-09: User base_url overrides registry
// ---------------------------------------------------------------------------

func TestE2E_UserBaseURLOverridesRegistry(t *testing.T) {
	var receivedHost string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "test", "object": "chat.completion", "choices": []any{},
		})
	}))
	defer mock.Close()

	cfg := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: mock.URL, // overrides registry's https://api.deepseek.com/v1
		Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "test-key", Weight: 100}},
	}

	gwURL := setupGateway(t, "deepseek", cfg)
	resp := sendChatRequest(t, gwURL)
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(mock.URL, receivedHost) {
		t.Errorf("request went to %q, expected mock at %q", receivedHost, mock.URL)
	}
}

// ---------------------------------------------------------------------------
// TEST-44-01-10: Streaming SSE forwarded
// ---------------------------------------------------------------------------

func TestE2E_StreamingSSEForwarded(t *testing.T) {
	mock := mockStreamingServer(t)
	defer mock.Close()

	cfg := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: mock.URL,
		Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "test-key", Weight: 100}},
	}

	gwURL := setupGateway(t, "sambanova", cfg)

	body := strings.NewReader(`{"model":"test-model","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	req, _ := http.NewRequest(http.MethodPost, gwURL+"/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}

	var chunks []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
			chunks = append(chunks, strings.TrimPrefix(line, "data: "))
		}
	}

	if len(chunks) < 2 {
		t.Errorf("expected >= 2 SSE chunks, got %d", len(chunks))
	}

	var hasContent bool
	for _, chunk := range chunks {
		var parsed map[string]any
		if json.Unmarshal([]byte(chunk), &parsed) == nil {
			if choices, ok := parsed["choices"].([]any); ok && len(choices) > 0 {
				hasContent = true
			}
		}
	}
	if !hasContent {
		t.Error("no SSE chunks with choices content found")
	}
}

// ---------------------------------------------------------------------------
// Registry metadata tests
// ---------------------------------------------------------------------------

func TestE2E_AllRegistryEntriesUseBearerAuth(t *testing.T) {
	for name, def := range providers.DefaultRegistry {
		if def.AuthPrefix != "Bearer " {
			t.Errorf("provider %q has AuthPrefix %q, want 'Bearer '", name, def.AuthPrefix)
		}
		if def.AuthHeader != "Authorization" {
			t.Errorf("provider %q has AuthHeader %q, want 'Authorization'", name, def.AuthHeader)
		}
	}
}

func TestE2E_AllRegistryProvidersResolve(t *testing.T) {
	for name := range providers.DefaultRegistry {
		result := providers.ApplyDefaults(name, map[string]interface{}{})
		typ, ok := result["type"].(string)
		if !ok || typ == "" {
			t.Errorf("provider %q: ApplyDefaults returned empty type", name)
		}
		url, ok := result["base_url"].(string)
		if !ok || url == "" {
			t.Errorf("provider %q: ApplyDefaults returned empty base_url", name)
		}
	}
}
