package mistral

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"openlimit/internal/providers"
	openaischema "openlimit/internal/schema/openai"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func ptrStr(v string) *string { return &v }

func basicRequest() openaischema.ChatCompletionRequest {
	return openaischema.ChatCompletionRequest{
		Model: "mistral-small-latest",
		Messages: []openaischema.ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}
}

func successResponseJSON() string {
	return `{
		"id": "cmpl-mistral-01",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "mistral-small-latest",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello from Mistral"
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 5,
			"completion_tokens": 4,
			"total_tokens": 9
		}
	}`
}

func streamChunkJSON(delta string, finishReason *string) string {
	chunk := map[string]any{
		"id":      "chatcmpl-mistral-stream",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "mistral-small-latest",
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{
					"content": delta,
				},
			},
		},
	}
	if finishReason != nil {
		chunk["choices"].([]map[string]any)[0]["finish_reason"] = *finishReason
	}
	data, _ := json.Marshal(chunk)
	return string(data)
}

// ---------------------------------------------------------------------------
// TEST-19-05-01: CompleteChat returns valid response from mock
// ---------------------------------------------------------------------------

func TestCompleteChat_ReturnsValidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method and path
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions path, got %s", r.URL.Path)
		}
		// Verify auth header
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %q", r.Header.Get("Authorization"))
		}
		// Verify content type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type, got %q", r.Header.Get("Content-Type"))
		}
		// Verify stream=false in request body
		var bodyReq openaischema.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&bodyReq); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if bodyReq.Stream {
			t.Errorf("expected stream=false, got true")
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, successResponseJSON())
	}))
	defer srv.Close()

	adapter := New(srv.URL)
	req := basicRequest()
	target := providers.Target{Provider: "mistral", Model: "mistral-small-latest"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	resp, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify response fields
	if resp.ID != "cmpl-mistral-01" {
		t.Errorf("expected id 'cmpl-mistral-01', got %q", resp.ID)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got %q", resp.Object)
	}
	if resp.Model != "mistral-small-latest" {
		t.Errorf("expected model 'mistral-small-latest', got %q", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if choice.Index != 0 {
		t.Errorf("expected index 0, got %d", choice.Index)
	}
	if choice.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", choice.FinishReason)
	}
	var contentStr string
	if err := json.Unmarshal(choice.Message.Content, &contentStr); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if contentStr != "Hello from Mistral" {
		t.Errorf("expected content 'Hello from Mistral', got %q", contentStr)
	}
	// Verify usage
	if resp.Usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if resp.Usage.PromptTokens != 5 {
		t.Errorf("expected prompt_tokens 5, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 4 {
		t.Errorf("expected completion_tokens 4, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 9 {
		t.Errorf("expected total_tokens 9, got %d", resp.Usage.TotalTokens)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-05-02: CompleteChat returns HTTPError on 429
// ---------------------------------------------------------------------------

func TestCompleteChat_ReturnsHTTPErrorOn429(t *testing.T) {
	errorBody := `{"error":{"message":"Rate limit exceeded","type":"rate_limit_error","code":"429"}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, errorBody)
	}))
	defer srv.Close()

	adapter := New(srv.URL)
	req := basicRequest()
	target := providers.Target{Provider: "mistral", Model: "mistral-small-latest"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	resp, err := adapter.CompleteChat(context.Background(), req, target, key)
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
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
// TEST-19-05-03: StreamChat returns chunks from mock SSE
// ---------------------------------------------------------------------------

func TestStreamChat_ReturnsChunksFromMockSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Accept header for SSE
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected Accept: text/event-stream, got %q", r.Header.Get("Accept"))
		}
		// Verify stream=true in request body
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["stream"] != true {
			t.Errorf("expected stream=true, got %v", body["stream"])
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}

		w.Header().Set("Content-Type", "text/event-stream")

		// Send two content chunks and a final chunk with finish_reason
		fmt.Fprintf(w, "data: %s\n\n", streamChunkJSON("Hello", nil))
		flusher.Flush()

		fmt.Fprintf(w, "data: %s\n\n", streamChunkJSON(" world", nil))
		flusher.Flush()

		fmt.Fprintf(w, "data: %s\n\n", streamChunkJSON("", ptrStr("stop")))
		flusher.Flush()

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	adapter := New(srv.URL)
	req := basicRequest()
	target := providers.Target{Provider: "mistral", Model: "mistral-small-latest"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	result, err := adapter.StreamChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []openaischema.ChatCompletionStreamChunk
	for chunk := range result.Chunks {
		chunks = append(chunks, chunk)
	}
	// Drain errors
	for err := range result.Errors {
		t.Fatalf("unexpected stream error: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// First chunk
	var text1 string
	if err := json.Unmarshal(chunks[0].Choices[0].Delta.Content, &text1); err != nil {
		t.Fatalf("unmarshal chunk 0 content: %v", err)
	}
	if text1 != "Hello" {
		t.Errorf("expected chunk 0 content 'Hello', got %q", text1)
	}
	if chunks[0].Object != "chat.completion.chunk" {
		t.Errorf("expected object 'chat.completion.chunk', got %q", chunks[0].Object)
	}

	// Second chunk
	var text2 string
	if err := json.Unmarshal(chunks[1].Choices[0].Delta.Content, &text2); err != nil {
		t.Fatalf("unmarshal chunk 1 content: %v", err)
	}
	if text2 != " world" {
		t.Errorf("expected chunk 1 content ' world', got %q", text2)
	}

	// Third chunk: finish
	if chunks[2].Choices[0].FinishReason == nil {
		t.Fatal("expected finish_reason on last chunk")
	}
	if *chunks[2].Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", *chunks[2].Choices[0].FinishReason)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-05-04: StreamChat returns HTTPError on 500
// ---------------------------------------------------------------------------

func TestStreamChat_ReturnsHTTPErrorOn500(t *testing.T) {
	errorBody := `{"error":{"message":"Internal server error","type":"server_error"}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, errorBody)
	}))
	defer srv.Close()

	adapter := New(srv.URL)
	req := basicRequest()
	target := providers.Target{Provider: "mistral", Model: "mistral-small-latest"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	result, err := adapter.StreamChat(context.Background(), req, target, key)
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
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
// TEST-19-05-05: Name() returns "mistral"
// ---------------------------------------------------------------------------

func TestName_ReturnsMistral(t *testing.T) {
	adapter := New("")
	if adapter.Name() != "mistral" {
		t.Errorf("expected Name() = 'mistral', got %q", adapter.Name())
	}
}

// ---------------------------------------------------------------------------
// Additional: Verify default base URL
// ---------------------------------------------------------------------------

func TestDefaultBaseURL(t *testing.T) {
	adapter := New("")
	if adapter.baseURL != DefaultBaseURL {
		t.Errorf("expected baseURL %q, got %q", DefaultBaseURL, adapter.baseURL)
	}
}
