package vertex

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
		Model: "gemini-2.0-flash",
		Messages: []openaischema.ChatMessage{
			{Role: "user", Content: content},
		},
	}
}

func vertexResponseJSON(text, finishReason string) string {
	return fmt.Sprintf(`{
		"candidates": [{
			"content": {"parts": [{"text": %q}], "role": "model"},
			"finishReason": %q,
			"index": 0
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 20,
			"totalTokenCount": 30
		}
	}`, text, finishReason)
}

func vertexStreamPayload(text, finishReason string) string {
	return fmt.Sprintf(`data: {"candidates":[{"content":{"parts":[{"text":%q}],"role":"model"},"finishReason":%q,"index":0}]}`, text, finishReason)
}

// ---------------------------------------------------------------------------
// TEST-19-02-01: CompleteChat returns valid response from mock
// ---------------------------------------------------------------------------

func TestCompleteChat_ValidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Bearer auth
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("expected 'Bearer test-token', got %q", auth)
		}
		// Verify no x-goog-api-key header
		if key := r.Header.Get("x-goog-api-key"); key != "" {
			t.Errorf("expected no x-goog-api-key header, got %q", key)
		}
		// Verify Vertex endpoint structure
		if !strings.Contains(r.URL.Path, "/projects/my-project/") {
			t.Errorf("expected project in path, got %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.Path, "/locations/us-central1/") {
			t.Errorf("expected region in path, got %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.Path, "/publishers/google/") {
			t.Errorf("expected publisher in path, got %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.Path, "models/gemini-2.0-flash:generateContent") {
			t.Errorf("expected model and action in path, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vertexResponseJSON("Hello from Vertex AI", "STOP"))
	}))
	defer srv.Close()

	// Create adapter — we'll override the httpClient to point at the test server
	adapter := New("vertex", "my-project", "us-central1", "google")
	adapter.httpClient = srv.Client()

	// Override buildEndpoint by monkey-patching via a custom transport
	// that redirects Vertex URLs to the test server.
	// Instead, let's test via a server that accepts any URL.
	// Re-create with a server that doesn't check URL (for unit test simplicity).

	req := basicRequest()
	target := providers.Target{Provider: "vertex", Model: "gemini-2.0-flash"}
	key := providers.ProviderKey{ID: "test", Value: "test-token"}

	// We need to make the adapter talk to the test server.
	// Use a transport that rewrites the request.
	transport := &redirectTransport{targetURL: srv.URL}
	adapter.httpClient = &http.Client{Transport: transport}

	resp, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify model
	if resp.Model != "gemini-2.0-flash" {
		t.Errorf("expected model 'gemini-2.0-flash', got %q", resp.Model)
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
	if contentStr != "Hello from Vertex AI" {
		t.Errorf("expected content 'Hello from Vertex AI', got %q", contentStr)
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
// TEST-19-02-02: CompleteChat returns HTTPError on 429
// ---------------------------------------------------------------------------

func TestCompleteChat_HTTPErrorOn429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED"}}`)
	}))
	defer srv.Close()

	adapter := New("vertex", "my-project", "us-central1", "")
	adapter.httpClient = &http.Client{Transport: &redirectTransport{targetURL: srv.URL}}

	req := basicRequest()
	target := providers.Target{Provider: "vertex", Model: "gemini-2.0-flash"}
	key := providers.ProviderKey{ID: "test", Value: "test-token"}

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
	if !strings.Contains(httpErr.Body, "Resource exhausted") {
		t.Errorf("expected body to contain 'Resource exhausted', got %q", httpErr.Body)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-02-03: StreamChat returns chunks from mock SSE
// ---------------------------------------------------------------------------

func TestStreamChat_ChunksFromMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming endpoint
		if !strings.Contains(r.URL.String(), "streamGenerateContent") {
			t.Errorf("expected streamGenerateContent in URL, got %s", r.URL.String())
		}
		if !strings.Contains(r.URL.String(), "alt=sse") {
			t.Errorf("expected alt=sse query param, got %s", r.URL.String())
		}
		// Verify Bearer auth
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer auth, got %q", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, vertexStreamPayload("Hello ", "")+"\n\n")
		fmt.Fprint(w, vertexStreamPayload("world", "")+"\n\n")
		fmt.Fprint(w, `data: {"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"STOP","index":0}]}`+"\n\n")
	}))
	defer srv.Close()

	adapter := New("vertex", "my-project", "us-central1", "")
	adapter.httpClient = &http.Client{Transport: &redirectTransport{targetURL: srv.URL}}

	req := basicRequest()
	target := providers.Target{Provider: "vertex", Model: "gemini-2.0-flash"}
	key := providers.ProviderKey{ID: "test", Value: "test-token"}

	result, err := adapter.StreamChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []openaischema.ChatCompletionStreamChunk
	for chunk := range result.Chunks {
		chunks = append(chunks, chunk)
	}
	for err := range result.Errors {
		t.Fatalf("unexpected stream error: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// First chunk: content
	var text1 string
	if err := json.Unmarshal(chunks[0].Choices[0].Delta.Content, &text1); err != nil {
		t.Fatalf("unmarshal chunk 0 content: %v", err)
	}
	if text1 != "Hello " {
		t.Errorf("expected 'Hello ', got %q", text1)
	}
	if chunks[0].Object != "chat.completion.chunk" {
		t.Errorf("expected object 'chat.completion.chunk', got %q", chunks[0].Object)
	}

	// Second chunk: content
	var text2 string
	if err := json.Unmarshal(chunks[1].Choices[0].Delta.Content, &text2); err != nil {
		t.Fatalf("unmarshal chunk 1 content: %v", err)
	}
	if text2 != "world" {
		t.Errorf("expected 'world', got %q", text2)
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
// TEST-19-02-04: StreamChat returns HTTPError on 500
// ---------------------------------------------------------------------------

func TestStreamChat_HTTPErrorOn500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"code":500,"message":"Internal server error","status":"INTERNAL"}}`)
	}))
	defer srv.Close()

	adapter := New("vertex", "my-project", "us-central1", "")
	adapter.httpClient = &http.Client{Transport: &redirectTransport{targetURL: srv.URL}}

	req := basicRequest()
	target := providers.Target{Provider: "vertex", Model: "gemini-2.0-flash"}
	key := providers.ProviderKey{ID: "test", Value: "test-token"}

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
	if !strings.Contains(httpErr.Body, "Internal server error") {
		t.Errorf("expected body to contain 'Internal server error', got %q", httpErr.Body)
	}
}

// ---------------------------------------------------------------------------
// TEST-19-02-05: Name() returns "vertex"
// ---------------------------------------------------------------------------

func TestName_ReturnsVertex(t *testing.T) {
	adapter := New("vertex", "my-project", "us-central1", "")
	if adapter.Name() != "vertex" {
		t.Errorf("expected Name() = 'vertex', got %q", adapter.Name())
	}
}

// ---------------------------------------------------------------------------
// redirectTransport redirects all requests to the test server while
// preserving the original URL path and headers for verification.
// ---------------------------------------------------------------------------

type redirectTransport struct {
	targetURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Build new URL pointing at the test server but keeping original path
	newURL := t.targetURL + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	// Copy headers
	for k, vv := range req.Header {
		for _, v := range vv {
			newReq.Header.Add(k, v)
		}
	}
	return http.DefaultTransport.RoundTrip(newReq)
}
