package bedrock

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

func basicRequest() openaischema.ChatCompletionRequest {
	content, _ := json.Marshal("Hello")
	return openaischema.ChatCompletionRequest{
		Model: "anthropic.claude-3-sonnet-20240229-v1:0",
		Messages: []openaischema.ChatMessage{
			{Role: "user", Content: content},
		},
	}
}

func bedrockResponseJSON(text, stopReason string) string {
	return fmt.Sprintf(`{
		"output": {
			"message": {
				"role": "assistant",
				"content": [{"text": %q}]
			}
		},
		"stopReason": %q,
		"usage": {
			"inputTokens": 10,
			"outputTokens": 20
		}
	}`, text, stopReason)
}

// ---------------------------------------------------------------------------
// TEST-19-01-01: CompleteChat returns valid response from mock
// ---------------------------------------------------------------------------

func TestCompleteChat_BasicResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key header, got %q", r.Header.Get("x-api-key"))
		}
		// Verify endpoint path contains /model/{modelId}/converse
		if !strings.Contains(r.URL.Path, "/converse") {
			t.Errorf("expected /converse in path, got %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.Path, "anthropic.claude-3-sonnet-20240229-v1:0") {
			t.Errorf("expected model ID in path, got %s", r.URL.Path)
		}
		// Verify content type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", r.Header.Get("Content-Type"))
		}

		// Verify request body was translated to Bedrock format
		var reqBody converseRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(reqBody.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(reqBody.Messages))
		}
		if reqBody.Messages[0].Role != "user" {
			t.Errorf("expected role 'user', got %q", reqBody.Messages[0].Role)
		}
		if len(reqBody.Messages[0].Content) != 1 || reqBody.Messages[0].Content[0].Text != "Hello" {
			t.Errorf("expected content text 'Hello', got %+v", reqBody.Messages[0].Content)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, bedrockResponseJSON("Hello from Bedrock", "end_turn"))
	}))
	defer srv.Close()

	adapter := New("bedrock", srv.URL)
	req := basicRequest()
	target := providers.Target{Provider: "bedrock", Model: "anthropic.claude-3-sonnet-20240229-v1:0"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	resp, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify model
	if resp.Model != "anthropic.claude-3-sonnet-20240229-v1:0" {
		t.Errorf("expected model 'anthropic.claude-3-sonnet-20240229-v1:0', got %q", resp.Model)
	}
	// Verify object
	if resp.Object != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got %q", resp.Object)
	}
	// Verify choices
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if choice.Index != 0 {
		t.Errorf("expected index 0, got %d", choice.Index)
	}
	var contentStr string
	if err := json.Unmarshal(choice.Message.Content, &contentStr); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if contentStr != "Hello from Bedrock" {
		t.Errorf("expected content 'Hello from Bedrock', got %q", contentStr)
	}
	if choice.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", choice.FinishReason)
	}
	// Verify usage
	if resp.Usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected prompt_tokens 10, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 20 {
		t.Errorf("expected completion_tokens 20, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("expected total_tokens 30, got %d", resp.Usage.TotalTokens)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-01-02: CompleteChat returns HTTPError on 429
// ---------------------------------------------------------------------------

func TestCompleteChat_HTTPErrorOn429(t *testing.T) {
	errorBody := `{"message":"Rate limit exceeded"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, errorBody)
	}))
	defer srv.Close()

	adapter := New("bedrock", srv.URL)
	req := basicRequest()
	target := providers.Target{Provider: "bedrock", Model: "anthropic.claude-3-sonnet-20240229-v1:0"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	_, err := adapter.CompleteChat(context.Background(), req, target, key)
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
	if httpErr.Body != errorBody {
		t.Errorf("expected body %q, got %q", errorBody, httpErr.Body)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-01-03: StreamChat returns chunks from mock SSE
// ---------------------------------------------------------------------------

func TestStreamChat_ChunksFromSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming endpoint
		if !strings.Contains(r.URL.Path, "/converse-stream") {
			t.Errorf("expected /converse-stream in path, got %s", r.URL.Path)
		}
		// Verify auth
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key header")
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected Accept: text/event-stream header, got %q", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// messageStart event
		fmt.Fprint(w, `data: {"type":"messageStart","role":"assistant"}`+"\n\n")
		flusher.Flush()

		// contentBlockDelta events
		fmt.Fprint(w, `data: {"type":"contentBlockDelta","contentBlockIndex":0,"delta":{"text":"Hello "}}`+"\n\n")
		flusher.Flush()

		fmt.Fprint(w, `data: {"type":"contentBlockDelta","contentBlockIndex":0,"delta":{"text":"world"}}`+"\n\n")
		flusher.Flush()

		// messageStop event
		fmt.Fprint(w, `data: {"type":"messageStop","stopReason":"end_turn"}`+"\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	adapter := New("bedrock", srv.URL)
	req := basicRequest()
	target := providers.Target{Provider: "bedrock", Model: "anthropic.claude-3-sonnet-20240229-v1:0"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	result, err := adapter.StreamChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []openaischema.ChatCompletionStreamChunk
	for chunk := range result.Chunks {
		chunks = append(chunks, chunk)
	}
	// Drain errors channel
	for err := range result.Errors {
		t.Fatalf("unexpected stream error: %v", err)
	}

	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}

	// First chunk: messageStart → role chunk
	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("expected first chunk role 'assistant', got %q", chunks[0].Choices[0].Delta.Role)
	}
	if chunks[0].Object != "chat.completion.chunk" {
		t.Errorf("expected object 'chat.completion.chunk', got %q", chunks[0].Object)
	}

	// Second chunk: content
	var text1 string
	if err := json.Unmarshal(chunks[1].Choices[0].Delta.Content, &text1); err != nil {
		t.Fatalf("unmarshal chunk 1 content: %v", err)
	}
	if text1 != "Hello " {
		t.Errorf("expected 'Hello ', got %q", text1)
	}

	// Third chunk: more content
	var text2 string
	if err := json.Unmarshal(chunks[2].Choices[0].Delta.Content, &text2); err != nil {
		t.Fatalf("unmarshal chunk 2 content: %v", err)
	}
	if text2 != "world" {
		t.Errorf("expected 'world', got %q", text2)
	}

	// Fourth chunk: messageStop → finish reason
	if chunks[3].Choices[0].FinishReason == nil {
		t.Fatal("expected finish_reason on last chunk")
	}
	if *chunks[3].Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", *chunks[3].Choices[0].FinishReason)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-01-04: StreamChat returns HTTPError on 500
// ---------------------------------------------------------------------------

func TestStreamChat_HTTPErrorOn500(t *testing.T) {
	errorBody := `{"message":"Internal server error"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, errorBody)
	}))
	defer srv.Close()

	adapter := New("bedrock", srv.URL)
	req := basicRequest()
	target := providers.Target{Provider: "bedrock", Model: "anthropic.claude-3-sonnet-20240229-v1:0"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	_, err := adapter.StreamChat(context.Background(), req, target, key)
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
	if httpErr.Body != errorBody {
		t.Errorf("expected body %q, got %q", errorBody, httpErr.Body)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-01-05: Name() returns "bedrock"
// ---------------------------------------------------------------------------

func TestName_ReturnsBedrock(t *testing.T) {
	adapter := New("bedrock", "https://bedrock-runtime.us-east-1.amazonaws.com")
	if adapter.Name() != "bedrock" {
		t.Errorf("expected Name() = 'bedrock', got %q", adapter.Name())
	}
}
