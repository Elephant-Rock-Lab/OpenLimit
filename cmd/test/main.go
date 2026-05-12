package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\n✗ FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Start mock provider
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":      "chatcmpl-smoke-test",
			"object":  "chat.completion",
			"created": float64(time.Now().Unix()),
			"model":   "smoke-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "Smoke test passed!"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     5,
				"completion_tokens": 4,
				"total_tokens":      9,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockProvider.Close()

	// 2. Configure gateway
	cfg := config.Default()
	cfg.Server.Port = 0
	cfg.Logging.Level = "error"
	cfg.Auth.Enabled = false
	cfg.Server.MaxBodySizeKB = 1024
	cfg.Providers = map[string]config.ProviderConfig{
		"smoke_provider": {
			Type:    "openai-compatible",
			BaseURL: mockProvider.URL,
			Keys:    []config.ProviderKeyConfig{{ID: "smoke", Value: "smoke-key", Weight: 100}},
		},
	}
	cfg.Models = map[string]config.ModelConfig{
		"smoke-model": {Routes: []config.ModelRoute{
			{Provider: "smoke_provider", Model: "smoke-model", Weight: 100},
		}},
	}

	// 3. Start gateway
	runtime := server.NewRuntime(cfg, logger, nil)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	gwServer := &http.Server{Handler: runtime.Server.Handler}
	go func() { gwServer.Serve(listener) }()
	defer gwServer.Shutdown(context.Background())

	gwAddr := fmt.Sprintf("http://%s", listener.Addr())

	// 4. Send request
	fmt.Println("OpenLimit Smoke Test")
	fmt.Println("════════════════════")
	fmt.Printf("Gateway:   %s\n", gwAddr)
	fmt.Printf("Provider:  mock (in-process)\n")
	fmt.Println()

	start := time.Now()
	body := strings.NewReader(`{"model":"smoke-model","messages":[{"role":"user","content":"hello"}]}`)
	req, _ := http.NewRequest(http.MethodPost, gwAddr+"/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer smoke-key")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	latency := time.Since(start)

	respBody, _ := io.ReadAll(resp.Body)

	// 5. Validate response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("invalid JSON response: %w", err)
	}

	// Verify required fields
	if result["object"] != "chat.completion" {
		return fmt.Errorf("object = %v, want chat.completion", result["object"])
	}
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) == 0 {
		return fmt.Errorf("no choices in response")
	}

	// 6. Print results
	fmt.Println("✓ PASS — All checks passed")
	fmt.Println()
	fmt.Printf("  Status:    %d OK\n", resp.StatusCode)
	fmt.Printf("  Latency:   %v\n", latency.Round(time.Microsecond))
	fmt.Printf("  Model:     %v\n", result["model"])
	fmt.Printf("  Tokens:    %v\n", result["usage"])

	return nil
}
