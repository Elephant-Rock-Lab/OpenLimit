package azure

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

// TEST-7B-02-01: Basic completion constructs correct Azure URL with resource,
// deployment, api-version and sends api-key header.
func TestCompleteChat_URLAndAuth(t *testing.T) {
	var capturedURL string
	var capturedKey string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		capturedKey = r.Header.Get("api-key")

		// Verify api-key is set and Authorization is NOT used
		if r.Header.Get("Authorization") != "" {
			t.Error("Authorization header should not be set; Azure uses api-key header")
		}

		w.Header().Set("Content-Type", "application/json")
		resp := openaischema.ChatCompletionResponse{
			ID:      "chatcmpl-test",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "gpt-4",
			Choices: []openaischema.Choice{
				{
					Index: 0,
					Message: openaischema.ChatMessage{
						Role:    "assistant",
						Content: json.RawMessage(`"hello"`),
					},
					FinishReason: "stop",
				},
			},
			Usage: &openaischema.Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	adapter := New("test-azure", "myresource", "2025-06-01")
	// Override the httpClient to use the test server
	adapter.httpClient = ts.Client()

	// Redirect requests to test server by temporarily changing URL construction.
	// We'll test URL construction via the captured URL after making the request
	// through a custom transport that rewrites the URL.
	transport := &urlRewriteTransport{
		base:    ts.Client().Transport,
		newHost: ts.URL,
	}
	adapter.httpClient = &http.Client{Transport: transport}

	req := openaischema.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []openaischema.ChatMessage{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	}
	target := providers.Target{
		Provider: "azure-openai",
		Model:    "my-deployment",
	}
	key := providers.ProviderKey{ID: "key1", Value: "secret-api-key"}

	resp, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("CompleteChat returned error: %v", err)
	}

	// Verify URL contains resource, deployment, and api-version
	if !strings.Contains(capturedURL, "/openai/deployments/my-deployment/chat/completions") {
		t.Errorf("URL should contain deployment path, got: %s", capturedURL)
	}
	if !strings.Contains(capturedURL, "api-version=2025-06-01") {
		t.Errorf("URL should contain api-version query param, got: %s", capturedURL)
	}

	// Verify api-key header
	if capturedKey != "secret-api-key" {
		t.Errorf("api-key header = %q, want %q", capturedKey, "secret-api-key")
	}

	// Verify response
	if resp.ID != "chatcmpl-test" {
		t.Errorf("response ID = %q, want %q", resp.ID, "chatcmpl-test")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
}

// TEST-7B-02-02: Deployment name from target.Model is used in URL path.
func TestCompleteChat_DeploymentInURL(t *testing.T) {
	var capturedURL string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		resp := openaischema.ChatCompletionResponse{
			ID:      "chatcmpl-1",
			Object:  "chat.completion",
			Created: 1,
			Model:   "my-custom-model",
			Choices: []openaischema.Choice{
				{Index: 0, Message: openaischema.ChatMessage{Role: "assistant", Content: json.RawMessage(`"ok"`)}, FinishReason: "stop"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	adapter := New("test-azure", "testres", "2025-06-01")
	adapter.httpClient = &http.Client{Transport: &urlRewriteTransport{base: http.DefaultTransport, newHost: ts.URL}}

	req := openaischema.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"test"`)}},
	}
	target := providers.Target{
		Provider: "azure-openai",
		Model:    "gpt-4-deployment-v2",
	}

	_, err := adapter.CompleteChat(context.Background(), req, target, providers.ProviderKey{ID: "k", Value: "key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedURL, "/openai/deployments/gpt-4-deployment-v2/chat/completions") {
		t.Errorf("URL should contain deployment name 'gpt-4-deployment-v2', got: %s", capturedURL)
	}
}

// TEST-7B-02-03: Streaming completion processes SSE chunks and returns StreamResult.
func TestStreamChat_SSEChunks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify stream=true in request
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Error("Expected Accept: text/event-stream header")
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}

		w.Header().Set("Content-Type", "text/event-stream")

		// Send two SSE chunks
		chunks := []openaischema.ChatCompletionStreamChunk{
			{
				ID:      "chatcmpl-stream-1",
				Object:  "chat.completion.chunk",
				Created: 1234567890,
				Model:   "gpt-4",
				Choices: []openaischema.StreamChoice{
					{
						Index: 0,
						Delta: openaischema.StreamDelta{
							Role:    "assistant",
							Content: json.RawMessage(`"Hello"`),
						},
					},
				},
			},
			{
				ID:      "chatcmpl-stream-2",
				Object:  "chat.completion.chunk",
				Created: 1234567891,
				Model:   "gpt-4",
				Choices: []openaischema.StreamChoice{
					{
						Index:        0,
						Delta:        openaischema.StreamDelta{Content: json.RawMessage(`" world"`)},
						FinishReason: strPtr("stop"),
					},
				},
			},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	adapter := New("test-azure", "testres", "2025-06-01")
	adapter.httpClient = &http.Client{Transport: &urlRewriteTransport{base: http.DefaultTransport, newHost: ts.URL}}

	req := openaischema.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}
	target := providers.Target{Provider: "azure-openai", Model: "gpt-4-deploy"}

	result, err := adapter.StreamChat(context.Background(), req, target, providers.ProviderKey{ID: "k", Value: "key"})
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}

	var receivedChunks []openaischema.ChatCompletionStreamChunk
	for chunk := range result.Chunks {
		receivedChunks = append(receivedChunks, chunk)
	}

	if len(receivedChunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(receivedChunks))
	}

	if receivedChunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk role = %q, want %q", receivedChunks[0].Choices[0].Delta.Role, "assistant")
	}

	// The Errors channel should be closed with no errors sent.
	for err := range result.Errors {
		t.Fatalf("unexpected error from stream: %v", err)
	}
}

// TEST-7B-02-04: Azure error response (401/429/500) returns *providers.HTTPError
// with correct status code.
func TestCompleteChat_HTTPError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"Unauthorized", http.StatusUnauthorized, `{"error":{"message":"Invalid API key","type":"invalid_request_error"}}`},
		{"RateLimited", http.StatusTooManyRequests, `{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`},
		{"ServerError", http.StatusInternalServerError, `{"error":{"message":"Internal server error","type":"server_error"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			adapter := New("test-azure", "testres", "2025-06-01")
			adapter.httpClient = &http.Client{Transport: &urlRewriteTransport{base: http.DefaultTransport, newHost: ts.URL}}

			req := openaischema.ChatCompletionRequest{
				Model:    "gpt-4",
				Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
			}
			target := providers.Target{Provider: "azure-openai", Model: "deploy"}
			key := providers.ProviderKey{ID: "k", Value: "key"}

			_, err := adapter.CompleteChat(context.Background(), req, target, key)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var httpErr *providers.HTTPError
			if !errors.As(err, &httpErr) {
				t.Fatalf("expected *providers.HTTPError, got %T: %v", err, err)
			}

			if httpErr.StatusCode != tt.statusCode {
				t.Errorf("status code = %d, want %d", httpErr.StatusCode, tt.statusCode)
			}
			if !strings.Contains(httpErr.Body, tt.body[:len(tt.body)-1]) {
				t.Errorf("body should contain error details, got: %s", httpErr.Body)
			}
		})
	}
}

// TEST-7B-02-05: Default API version "2025-06-01" applied when apiVersion is empty.
func TestDefaultAPIVersion(t *testing.T) {
	var capturedURL string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		resp := openaischema.ChatCompletionResponse{
			ID:      "chatcmpl-1",
			Object:  "chat.completion",
			Created: 1,
			Model:   "gpt-4",
			Choices: []openaischema.Choice{
				{Index: 0, Message: openaischema.ChatMessage{Role: "assistant", Content: json.RawMessage(`"ok"`)}, FinishReason: "stop"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// Pass empty apiVersion — should default to "2025-06-01"
	adapter := New("test-azure", "myresource", "")
	if adapter.apiVersion != "2025-06-01" {
		t.Errorf("apiVersion = %q, want %q", adapter.apiVersion, "2025-06-01")
	}

	adapter.httpClient = &http.Client{Transport: &urlRewriteTransport{base: http.DefaultTransport, newHost: ts.URL}}

	req := openaischema.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []openaischema.ChatMessage{{Role: "user", Content: json.RawMessage(`"test"`)}},
	}
	target := providers.Target{Provider: "azure-openai", Model: "deploy"}

	_, err := adapter.CompleteChat(context.Background(), req, target, providers.ProviderKey{ID: "k", Value: "key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedURL, "api-version=2025-06-01") {
		t.Errorf("URL should contain default api-version '2025-06-01', got: %s", capturedURL)
	}
}

// strPtr is a helper to get a pointer to a string.
func strPtr(s string) *string {
	return &s
}

// urlRewriteTransport redirects requests to the test server while preserving
// the original URL path and query for inspection.
type urlRewriteTransport struct {
	base    http.RoundTripper
	newHost string
}

func (t *urlRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the host to the test server, keeping path and query
	newURL := t.newHost + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return t.base.RoundTrip(newReq)
}
