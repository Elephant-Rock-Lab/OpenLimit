package groq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openlimit/internal/providers"
	openaischema "openlimit/internal/schema/openai"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestAdapter(serverURL string) *Adapter {
	return New(serverURL)
}

func sampleRequest() openaischema.ChatCompletionRequest {
	return openaischema.ChatCompletionRequest{
		Model: "llama-3.3-70b-versatile",
		Messages: []openaischema.ChatMessage{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
}

func sampleTarget() providers.Target {
	return providers.Target{
		Provider: "groq",
		Model:    "llama-3.3-70b-versatile",
	}
}

func sampleKey() providers.ProviderKey {
	return providers.ProviderKey{ID: "k1", Value: "test-groq-key"}
}

func successResponseJSON() string {
	return `{
		"id": "chatcmpl-groq-123",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "llama-3.3-70b-versatile",
		"choices": [
			{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello! How can I help you?"},
				"finish_reason": "stop"
			}
		],
		"usage": {"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18}
	}`
}

// ---------------------------------------------------------------------------
// TEST-19-03-01: CompleteChat returns valid response from mock
// ---------------------------------------------------------------------------

func TestCompleteChat_ReturnsValidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-groq-key" {
			t.Errorf("expected Bearer token, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, successResponseJSON())
	}))
	defer server.Close()

	adapter := newTestAdapter(server.URL)
	resp, err := adapter.CompleteChat(context.Background(), sampleRequest(), sampleTarget(), sampleKey())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "chatcmpl-groq-123" {
		t.Errorf("expected id chatcmpl-groq-123, got %s", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason stop, got %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 18 {
		t.Errorf("expected usage.total_tokens=18, got %v", resp.Usage)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-03-02: CompleteChat returns HTTPError on 429
// ---------------------------------------------------------------------------

func TestCompleteChat_ReturnsHTTPErrorOn429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`)
	}))
	defer server.Close()

	adapter := newTestAdapter(server.URL)
	_, err := adapter.CompleteChat(context.Background(), sampleRequest(), sampleTarget(), sampleKey())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var httpErr *providers.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *providers.HTTPError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", httpErr.StatusCode)
	}
	if !strings.Contains(httpErr.Body, "Rate limit exceeded") {
		t.Errorf("expected body to contain 'Rate limit exceeded', got %s", httpErr.Body)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-03-03: StreamChat returns chunks from mock SSE
// ---------------------------------------------------------------------------

func TestStreamChat_ReturnsChunksFromMockSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("expected Accept: text/event-stream, got %q", got)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`{"id":"chatcmpl-groq-s1","object":"chat.completion.chunk","created":1700000000,"model":"llama-3.3-70b-versatile","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-groq-s1","object":"chat.completion.chunk","created":1700000000,"model":"llama-3.3-70b-versatile","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-groq-s1","object":"chat.completion.chunk","created":1700000000,"model":"llama-3.3-70b-versatile","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	adapter := newTestAdapter(server.URL)
	result, err := adapter.StreamChat(context.Background(), sampleRequest(), sampleTarget(), sampleKey())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var collected []openaischema.ChatCompletionStreamChunk
	for chunk := range result.Chunks {
		collected = append(collected, chunk)
	}

	if len(collected) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(collected))
	}

	// Verify first chunk content
	var firstContent string
	_ = json.Unmarshal(collected[0].Choices[0].Delta.Content, &firstContent)
	if firstContent != "Hi" {
		t.Errorf("expected first chunk content 'Hi', got %q", firstContent)
	}

	// Verify last chunk finish_reason
	fr := collected[2].Choices[0].FinishReason
	if fr == nil || *fr != "stop" {
		t.Errorf("expected finish_reason 'stop' on last chunk, got %v", fr)
	}

	// Check no errors
	for err := range result.Errors {
		t.Fatalf("unexpected stream error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-03-04: StreamChat returns HTTPError on 500
// ---------------------------------------------------------------------------

func TestStreamChat_ReturnsHTTPErrorOn500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"Internal server error","type":"server_error"}}`)
	}))
	defer server.Close()

	adapter := newTestAdapter(server.URL)
	_, err := adapter.StreamChat(context.Background(), sampleRequest(), sampleTarget(), sampleKey())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var httpErr *providers.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *providers.HTTPError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", httpErr.StatusCode)
	}
	if !strings.Contains(httpErr.Body, "Internal server error") {
		t.Errorf("expected body to contain 'Internal server error', got %s", httpErr.Body)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-03-05: Name() returns "groq"
// ---------------------------------------------------------------------------

func TestName_ReturnsGroq(t *testing.T) {
	adapter := New("")
	if adapter.Name() != "groq" {
		t.Errorf("expected Name() = 'groq', got %q", adapter.Name())
	}
}
