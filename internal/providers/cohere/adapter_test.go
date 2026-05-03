package cohere

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

// ---------- TEST-19-04-01: CompleteChat returns valid response from mock ----------
func TestCompleteChat_ValidResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/chat" {
			t.Errorf("path = %q, want /chat", r.URL.Path)
		}

		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key-123" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key-123")
		}

		// Verify content type
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		// Verify request body
		var reqBody cohereRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if reqBody.Model != "command-r-plus" {
			t.Errorf("model = %q, want %q", reqBody.Model, "command-r-plus")
		}
		if reqBody.Stream {
			t.Error("stream should be false for CompleteChat")
		}

		// Return Cohere v2 response
		w.Header().Set("Content-Type", "application/json")
		resp := cohereResponse{
			ID: "chat-abc123",
			Message: cohereRespMsg{
				Content: []cohereContentBlock{
					{Type: "text", Text: "Hello from Cohere!"},
				},
			},
			Usage: cohereUsage{
				Tokens: cohereTokens{
					Input:  15,
					Output: 8,
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	adapter := New(ts.URL)

	req := openaischema.ChatCompletionRequest{
		Model: "command-r-plus",
		Messages: []openaischema.ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}
	target := providers.Target{Provider: "cohere", Model: "command-r-plus"}
	key := providers.ProviderKey{ID: "k1", Value: "test-key-123"}

	resp, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("CompleteChat returned error: %v", err)
	}

	// Verify response fields
	if resp.ID != "chat-abc123" {
		t.Errorf("ID = %q, want %q", resp.ID, "chat-abc123")
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want %q", resp.Object, "chat.completion")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("role = %q, want %q", resp.Choices[0].Message.Role, "assistant")
	}

	// Verify content
	var contentStr string
	if err := json.Unmarshal(resp.Choices[0].Message.Content, &contentStr); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if contentStr != "Hello from Cohere!" {
		t.Errorf("content = %q, want %q", contentStr, "Hello from Cohere!")
	}

	// Verify usage
	if resp.Usage == nil {
		t.Fatal("usage is nil")
	}
	if resp.Usage.PromptTokens != 15 {
		t.Errorf("prompt_tokens = %d, want 15", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 8 {
		t.Errorf("completion_tokens = %d, want 8", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 23 {
		t.Errorf("total_tokens = %d, want 23", resp.Usage.TotalTokens)
	}
}

// ---------- TEST-19-04-02: CompleteChat returns HTTPError on 429 ----------
func TestCompleteChat_HTTPError429(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"Rate limit exceeded"}`))
	}))
	defer ts.Close()

	adapter := New(ts.URL)

	req := openaischema.ChatCompletionRequest{
		Model:    "command-r-plus",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}
	target := providers.Target{Provider: "cohere", Model: "command-r-plus"}
	key := providers.ProviderKey{ID: "k1", Value: "test-key"}

	_, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var httpErr *providers.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *providers.HTTPError, got %T: %v", err, err)
	}

	if httpErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status code = %d, want %d", httpErr.StatusCode, http.StatusTooManyRequests)
	}
	if !strings.Contains(httpErr.Body, "Rate limit exceeded") {
		t.Errorf("body should contain error message, got: %s", httpErr.Body)
	}
}

// ---------- TEST-19-04-03: StreamChat returns chunks from mock SSE ----------
func TestStreamChat_SSEChunks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify stream is set in request body
		var reqBody cohereRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !reqBody.Stream {
			t.Error("stream should be true for StreamChat")
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}

		w.Header().Set("Content-Type", "text/event-stream")

		events := []string{
			`{"type":"message-start"}`,
			`{"type":"content-delta","delta":{"content":{"text":"Hello"}}}`,
			`{"type":"content-delta","delta":{"content":{"text":" world"}}}`,
		}

		for _, evt := range events {
			fmt.Fprintf(w, "data: %s\n\n", evt)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	adapter := New(ts.URL)

	req := openaischema.ChatCompletionRequest{
		Model:    "command-r-plus",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}
	target := providers.Target{Provider: "cohere", Model: "command-r-plus"}
	key := providers.ProviderKey{ID: "k1", Value: "test-key"}

	result, err := adapter.StreamChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}

	var receivedChunks []openaischema.ChatCompletionStreamChunk
	for chunk := range result.Chunks {
		receivedChunks = append(receivedChunks, chunk)
	}

	// Expect: message-start, 2 content-deltas, 1 [DONE] finish chunk = 4 chunks
	if len(receivedChunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(receivedChunks))
	}

	// First chunk: role "assistant"
	if receivedChunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk role = %q, want %q", receivedChunks[0].Choices[0].Delta.Role, "assistant")
	}

	// Second chunk: "Hello"
	var text1 string
	if err := json.Unmarshal(receivedChunks[1].Choices[0].Delta.Content, &text1); err != nil {
		t.Fatalf("unmarshal chunk 1 content: %v", err)
	}
	if text1 != "Hello" {
		t.Errorf("chunk 1 text = %q, want %q", text1, "Hello")
	}

	// Third chunk: " world"
	var text2 string
	if err := json.Unmarshal(receivedChunks[2].Choices[0].Delta.Content, &text2); err != nil {
		t.Fatalf("unmarshal chunk 2 content: %v", err)
	}
	if text2 != " world" {
		t.Errorf("chunk 2 text = %q, want %q", text2, " world")
	}

	// Fourth chunk: finish_reason "stop"
	if receivedChunks[3].Choices[0].FinishReason == nil || *receivedChunks[3].Choices[0].FinishReason != "stop" {
		t.Errorf("chunk 3 finish_reason = %v, want %q", receivedChunks[3].Choices[0].FinishReason, "stop")
	}

	// No errors
	for err := range result.Errors {
		t.Fatalf("unexpected error from stream: %v", err)
	}
}

// ---------- TEST-19-04-04: StreamChat returns HTTPError on 500 ----------
func TestStreamChat_HTTPError500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"Internal server error"}`))
	}))
	defer ts.Close()

	adapter := New(ts.URL)

	req := openaischema.ChatCompletionRequest{
		Model:    "command-r-plus",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}
	target := providers.Target{Provider: "cohere", Model: "command-r-plus"}
	key := providers.ProviderKey{ID: "k1", Value: "test-key"}

	_, err := adapter.StreamChat(context.Background(), req, target, key)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var httpErr *providers.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *providers.HTTPError, got %T: %v", err, err)
	}

	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", httpErr.StatusCode, http.StatusInternalServerError)
	}
	if !strings.Contains(httpErr.Body, "Internal server error") {
		t.Errorf("body should contain error message, got: %s", httpErr.Body)
	}
}

// ---------- TEST-19-04-05: Name() returns "cohere" ----------
func TestName(t *testing.T) {
	adapter := New("")
	if adapter.Name() != "cohere" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "cohere")
	}
}
