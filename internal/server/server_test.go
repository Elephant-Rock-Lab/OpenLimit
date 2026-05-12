package server_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/server"
)

func TestReadyEndpointReportsShutdownState(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:1", "openai-compatible")
	runtime := newRuntime(t, cfg)
	gateway := httptest.NewServer(runtime.Server.Handler)
	defer gateway.Close()

	runtime.Tracker.MarkShuttingDown()

	resp, err := http.Get(gateway.URL + "/ready")
	if err != nil {
		t.Fatalf("get ready: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}

	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["status"] != "shutting_down" {
		t.Fatalf("expected shutting_down, got %#v", body["status"])
	}
	if body["shutting_down"] != true {
		t.Fatalf("expected shutting_down=true, got %#v", body)
	}
}

func TestProviderRequestTimeout(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writeJSON(t, w, map[string]any{"id": "too_late"})
	}))
	defer provider.Close()

	cfg := testConfig(provider.URL, "openai-compatible")
	cfg.Routing.Defaults.TimeoutMS = 10
	cfg.Routing.Defaults.Retry.Attempts = 1
	gateway := newGateway(t, cfg)
	defer gateway.Close()

	resp := postChat(t, gateway.URL, `{"model":"fast","messages":[{"role":"user","content":"timeout"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}
}

func TestReadyEndpointReportsMissingEnvKey(t *testing.T) {
	t.Setenv("OPENLIMIT_TEST_MISSING_KEY", "")
	cfg := baseConfig()
	cfg.Providers = map[string]config.ProviderConfig{
		"openai": {
			Type: "openai",
			Keys: []config.ProviderKeyConfig{{ID: "missing", Env: "OPENLIMIT_TEST_MISSING_KEY"}},
		},
	}
	cfg.Models = map[string]config.ModelConfig{
		"fast": {Routes: []config.ModelRoute{{Provider: "openai", Model: "gpt-mock", Weight: 100}}},
	}

	gateway := newGateway(t, cfg)
	defer gateway.Close()

	resp, err := http.Get(gateway.URL + "/ready")
	if err != nil {
		t.Fatalf("get ready: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}

	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["status"] != "not_ready" {
		t.Fatalf("expected not_ready, got %#v", body["status"])
	}
	providers := body["providers"].([]any)
	provider := providers[0].(map[string]any)
	if provider["ready"] != false {
		t.Fatalf("expected provider not ready, got %#v", provider)
	}
	missing := provider["missing_env"].([]any)
	if len(missing) != 1 || missing[0] != "OPENLIMIT_TEST_MISSING_KEY" {
		t.Fatalf("unexpected missing env list: %#v", missing)
	}
}

func TestAuthRequiredProviderWithoutActiveKeyFailsBeforeProviderCall(t *testing.T) {
	t.Setenv("OPENLIMIT_TEST_MISSING_KEY", "")
	var calls int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer provider.Close()

	cfg := baseConfig()
	cfg.Providers = map[string]config.ProviderConfig{
		"openai": {
			Type:    "openai",
			BaseURL: provider.URL,
			Keys:    []config.ProviderKeyConfig{{ID: "missing", Env: "OPENLIMIT_TEST_MISSING_KEY"}},
		},
	}
	cfg.Models = map[string]config.ModelConfig{
		"fast": {Routes: []config.ModelRoute{{Provider: "openai", Model: "gpt-mock", Weight: 100}}},
	}

	gateway := newGateway(t, cfg)
	defer gateway.Close()

	resp := postChat(t, gateway.URL, `{"model":"fast","messages":[{"role":"user","content":"hello"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected provider not to be called, got %d calls", got)
	}
}

func TestModelsEndpointListsLogicalAliases(t *testing.T) {
	cfg := baseConfig()
	cfg.Providers = map[string]config.ProviderConfig{
		"openai":    providerConfig("openai-compatible", "http://127.0.0.1:1"),
		"anthropic": providerConfig("anthropic", "http://127.0.0.1:2"),
	}
	cfg.Models = map[string]config.ModelConfig{
		"claude": {Routes: []config.ModelRoute{{Provider: "anthropic", Model: "claude-mock", Weight: 100}}},
		"fast":   {Routes: []config.ModelRoute{{Provider: "openai", Model: "gpt-mock", Weight: 100}}},
	}

	gateway := newGateway(t, cfg)
	defer gateway.Close()

	resp, err := http.Get(gateway.URL + "/v1/models")
	if err != nil {
		t.Fatalf("get models: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}

	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["object"] != "list" {
		t.Fatalf("expected list object, got %#v", body["object"])
	}
	data := body["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(data))
	}
	first := data[0].(map[string]any)
	second := data[1].(map[string]any)
	if first["id"] != "claude" || first["owned_by"] != "anthropic" {
		t.Fatalf("unexpected first model: %#v", first)
	}
	if second["id"] != "fast" || second["owned_by"] != "openai" {
		t.Fatalf("unexpected second model: %#v", second)
	}
}

func TestOpenAICompatibleNonStreamingProxy(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected provider path: %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		if req["model"] != "provider-model" {
			t.Fatalf("expected provider model, got %v", req["model"])
		}
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl_test",
			"object":  "chat.completion",
			"created": float64(123),
			"model":   "provider-model",
			"choices": []map[string]any{{
				"index": float64(0),
				"message": map[string]any{
					"role":    "assistant",
					"content": "hello from mock openai",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": float64(2), "completion_tokens": float64(3), "total_tokens": float64(5)},
		})
	}))
	defer provider.Close()

	gateway := newGateway(t, testConfig(provider.URL, "openai-compatible"))
	defer gateway.Close()

	resp := postChat(t, gateway.URL, `{"model":"fast","messages":[{"role":"user","content":"hello"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}

	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["id"] != "chatcmpl_test" {
		t.Fatalf("unexpected response id: %#v", body["id"])
	}
}

func TestOpenAICompatibleStreamingProxy(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected provider path: %s", r.URL.Path)
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("provider response writer does not flush")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chunk_1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"provider-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"id\":\"chunk_1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"provider-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer provider.Close()

	gateway := newGateway(t, testConfig(provider.URL, "openai-compatible"))
	defer gateway.Close()

	resp := postChat(t, gateway.URL, `{"model":"fast","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %q", got)
	}

	dataLines := collectDataLines(t, resp.Body)
	if len(dataLines) != 3 {
		t.Fatalf("expected 3 data lines, got %d: %#v", len(dataLines), dataLines)
	}
	if !strings.Contains(dataLines[1], `"content":"hi"`) {
		t.Fatalf("expected content chunk, got %q", dataLines[1])
	}
	if dataLines[2] != "data: [DONE]" {
		t.Fatalf("expected done sentinel, got %q", dataLines[2])
	}
}

func TestAnthropicNonStreamingTransform(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Fatalf("unexpected provider path: %s", r.URL.Path)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Fatal("expected anthropic-version header")
		}
		var req map[string]any
		decodeJSON(t, r.Body, &req)
		if req["model"] != "claude-mock" {
			t.Fatalf("expected claude-mock model, got %#v", req["model"])
		}
		if req["system"] != "be brief" {
			t.Fatalf("expected system prompt transform, got %#v", req["system"])
		}
		writeJSON(t, w, map[string]any{
			"id":          "msg_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-mock",
			"content":     []map[string]any{{"type": "text", "text": "hello from anthropic"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": float64(4), "output_tokens": float64(5)},
		})
	}))
	defer provider.Close()

	gateway := newGateway(t, testConfig(provider.URL, "anthropic"))
	defer gateway.Close()

	resp := postChat(t, gateway.URL, `{"model":"fast","messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hello"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}

	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	choices := body["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "hello from anthropic" {
		t.Fatalf("unexpected anthropic mapped content: %#v", message["content"])
	}
}

func TestAnthropicStreamingTransform(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Fatalf("unexpected provider path: %s", r.URL.Path)
		}
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-mock\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		flusher.Flush()
	}))
	defer provider.Close()

	gateway := newGateway(t, testConfig(provider.URL, "anthropic"))
	defer gateway.Close()

	resp := postChat(t, gateway.URL, `{"model":"fast","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}

	dataLines := collectDataLines(t, resp.Body)
	joined := strings.Join(dataLines, "\n")
	if !strings.Contains(joined, `"role":"assistant"`) {
		t.Fatalf("expected assistant role chunk, got %s", joined)
	}
	if !strings.Contains(joined, `"content":"hello"`) {
		t.Fatalf("expected content chunk, got %s", joined)
	}
	if !strings.Contains(joined, `"finish_reason":"stop"`) {
		t.Fatalf("expected stop finish reason, got %s", joined)
	}
}

func TestFallbackFromPrimaryFailure(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"id":      "fallback_response",
			"object":  "chat.completion",
			"created": float64(123),
			"model":   "fallback-model",
			"choices": []map[string]any{{"index": float64(0), "message": map[string]any{"role": "assistant", "content": "fallback ok"}, "finish_reason": "stop"}},
		})
	}))
	defer fallback.Close()

	cfg := baseConfig()
	cfg.Routing.Defaults.Retry.Attempts = 1
	cfg.Providers = map[string]config.ProviderConfig{
		"primary":  providerConfig("openai-compatible", primary.URL),
		"fallback": providerConfig("openai-compatible", fallback.URL),
	}
	cfg.Models = map[string]config.ModelConfig{
		"fast": {
			Routes:    []config.ModelRoute{{Provider: "primary", Model: "primary-model", Weight: 100}},
			Fallbacks: []config.ModelRoute{{Provider: "fallback", Model: "fallback-model"}},
		},
	}

	gateway := newGateway(t, cfg)
	defer gateway.Close()

	resp := postChat(t, gateway.URL, `{"model":"fast","messages":[{"role":"user","content":"hello"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected fallback status 200, got %d: %s", resp.StatusCode, readBody(t, resp.Body))
	}
	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["id"] != "fallback_response" {
		t.Fatalf("expected fallback response, got %#v", body["id"])
	}
}

func TestExactCacheHit(t *testing.T) {
	var calls int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeJSON(t, w, map[string]any{
			"id":      "cached_response",
			"object":  "chat.completion",
			"created": float64(123),
			"model":   "provider-model",
			"choices": []map[string]any{{"index": float64(0), "message": map[string]any{"role": "assistant", "content": "cache me"}, "finish_reason": "stop"}},
		})
	}))
	defer provider.Close()

	cfg := testConfig(provider.URL, "openai-compatible")
	cfg.Cache.Exact.Enabled = true
	gateway := newGateway(t, cfg)
	defer gateway.Close()

	body := `{"model":"fast","messages":[{"role":"user","content":"cache please"}]}`
	for i := 0; i < 2; i++ {
		resp := postChat(t, gateway.URL, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d: %s", i, resp.StatusCode, readBody(t, resp.Body))
		}
		_ = resp.Body.Close()
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected provider to be called once due to cache, got %d", got)
	}
}

func newGateway(t *testing.T, cfg config.Config) *httptest.Server {
	t.Helper()
	runtime := newRuntime(t, cfg)
	return httptest.NewServer(runtime.Server.Handler)
}

func newRuntime(t *testing.T, cfg config.Config) *server.Runtime {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return server.NewRuntime(cfg, logger, nil)
}

func baseConfig() config.Config {
	cfg := config.Default()
	cfg.Cache.Exact.Enabled = true
	cfg.Cache.Exact.MaxEntries = 100
	cfg.Cache.Exact.TTLSeconds = 60
	cfg.Routing.Defaults.TimeoutMS = 5000
	cfg.Routing.Defaults.Retry.Attempts = 2
	cfg.Routing.Defaults.Retry.InitialMS = 1
	cfg.Routing.Defaults.Retry.MaxMS = 2
	return cfg
}

func testConfig(providerURL string, providerType string) config.Config {
	cfg := baseConfig()
	cfg.Providers = map[string]config.ProviderConfig{
		"mock": providerConfig(providerType, providerURL),
	}
	cfg.Models = map[string]config.ModelConfig{
		"fast": {Routes: []config.ModelRoute{{Provider: "mock", Model: modelFor(providerType), Weight: 100}}},
	}
	return cfg
}

func providerConfig(providerType string, baseURL string) config.ProviderConfig {
	return config.ProviderConfig{
		Type:    providerType,
		BaseURL: baseURL,
		Keys:    []config.ProviderKeyConfig{{ID: "test", Value: "test-key", Weight: 100}},
	}
}

func modelFor(providerType string) string {
	if providerType == "anthropic" {
		return "claude-mock"
	}
	return "provider-model"
}

func postChat(t *testing.T, baseURL string, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post chat: %v", err)
	}
	return resp
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode JSON: %v", err)
	}
}

func decodeJSON(t *testing.T, r io.Reader, value any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(value); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

func readBody(t *testing.T, r io.Reader) string {
	t.Helper()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// BATCH-59 / TASK-01: API route body limit independence test
// ---------------------------------------------------------------------------
// Note: Admin body limit tests (TEST-59-01-01, TEST-59-01-02) are in sec03_test.go
// because they need white-box access to maxBodySizeMiddleware.

// TEST-59-01-04: API route is independent of admin 64KB body limit.
// API route uses MaxBodySizeKB from config (default), NOT the admin 64KB limit.
func TestAPIRoute_IndependentOfAdminBodyLimit(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl_test",
			"object":  "chat.completion",
			"created": float64(123),
			"model":   "provider-model",
			"choices": []map[string]any{{
				"index": float64(0),
				"message": map[string]any{
					"role":    "assistant",
					"content": "ok",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": float64(1), "completion_tokens": float64(1), "total_tokens": float64(2)},
		})
	}))
	defer provider.Close()

	cfg := testConfig(provider.URL, "openai-compatible")
	// Set MaxBodySizeKB to 100KB so API route accepts >64KB bodies
	cfg.Server.MaxBodySizeKB = 100

	runtime := newRuntime(t, cfg)
	gateway := httptest.NewServer(runtime.Server.Handler)
	defer gateway.Close()

	// 70KB body — exceeds admin 64KB limit but under API 100KB limit
	bigContent := strings.Repeat("x", 70*1024)
	body := fmt.Sprintf(`{"model":"fast","messages":[{"role":"user","content":"%s"}]}`, bigContent)

	resp := postChat(t, gateway.URL, body)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		t.Errorf("API route rejected 70KB body with 413; API body limit should be independent of admin 64KB limit")
	}
}

func collectDataLines(t *testing.T, r io.Reader) []string {
	t.Helper()
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, line)
		}
		if line == "data: [DONE]" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}
	return lines
}
