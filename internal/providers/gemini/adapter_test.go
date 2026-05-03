package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func ptrInt(v int) *int           { return &v }
func ptrFloat(v float64) *float64 { return &v }

func basicRequest() openaischema.ChatCompletionRequest {
	content, _ := json.Marshal("Hello")
	return openaischema.ChatCompletionRequest{
		Model: "gemini-pro",
		Messages: []openaischema.ChatMessage{
			{Role: "user", Content: content},
		},
	}
}

func geminiResponseJSON(text, finishReason string) string {
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

func geminiStreamPayload(text, finishReason string) string {
	return fmt.Sprintf(`data: {"candidates":[{"content":{"parts":[{"text":%q}],"role":"model"},"finishReason":%q,"index":0}]}`, text, finishReason)
}

// ---------------------------------------------------------------------------
// TEST-7B-01-01: Basic non-streaming completion returns correctly transformed
// OpenAI response with model, usage, choices
// ---------------------------------------------------------------------------

func TestCompleteChat_BasicResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("expected x-goog-api-key header, got %q", r.Header.Get("x-goog-api-key"))
		}
		// Verify no Authorization header
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no Authorization header, got %q", auth)
		}
		// Verify endpoint path contains generateContent
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("expected generateContent in path, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, geminiResponseJSON("Hello from Gemini", "STOP"))
	}))
	defer srv.Close()

	adapter := New("gemini-test", srv.URL, nil)
	req := basicRequest()
	target := providers.Target{Provider: "gemini", Model: "gemini-pro"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	resp, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify model
	if resp.Model != "gemini-pro" {
		t.Errorf("expected model 'gemini-pro', got %q", resp.Model)
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
	if contentStr != "Hello from Gemini" {
		t.Errorf("expected content 'Hello from Gemini', got %q", contentStr)
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
// TEST-7B-01-02: System prompt is extracted into systemInstruction field
// ---------------------------------------------------------------------------

func TestCompleteChat_SystemPromptInSystemInstruction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		defer r.Body.Close()

		var req geminiRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}

		// Verify systemInstruction is set
		if req.SystemInstruction == nil {
			t.Fatal("expected systemInstruction to be set, got nil")
		}
		if len(req.SystemInstruction.Parts) != 1 {
			t.Fatalf("expected 1 part in systemInstruction, got %d", len(req.SystemInstruction.Parts))
		}
		if req.SystemInstruction.Parts[0].Text != "You are a helpful assistant" {
			t.Errorf("expected system text 'You are a helpful assistant', got %q", req.SystemInstruction.Parts[0].Text)
		}

		// Verify "system" message is NOT in contents
		for _, c := range req.Contents {
			if c.Role == "system" {
				t.Error("system message should not appear in contents")
			}
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, geminiResponseJSON("ok", "STOP"))
	}))
	defer srv.Close()

	adapter := New("gemini-test", srv.URL, nil)
	content, _ := json.Marshal("You are a helpful assistant")
	req := openaischema.ChatCompletionRequest{
		Model: "gemini-pro",
		Messages: []openaischema.ChatMessage{
			{Role: "system", Content: content},
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}

	target := providers.Target{Provider: "gemini", Model: "gemini-pro"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	_, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TEST-7B-01-03: Tool call functions are transformed to Gemini
// functionDeclarations format
// ---------------------------------------------------------------------------

func TestCompleteChat_ToolCallTransformation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		defer r.Body.Close()

		var req geminiRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}

		if len(req.Tools) != 1 {
			t.Fatalf("expected 1 tool group, got %d", len(req.Tools))
		}
		decls := req.Tools[0].FunctionDeclarations
		if len(decls) != 1 {
			t.Fatalf("expected 1 function declaration, got %d", len(decls))
		}
		if decls[0].Name != "get_weather" {
			t.Errorf("expected function name 'get_weather', got %q", decls[0].Name)
		}
		if decls[0].Description != "Get weather for a location" {
			t.Errorf("expected description 'Get weather for a location', got %q", decls[0].Description)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, geminiResponseJSON("tool result", "STOP"))
	}))
	defer srv.Close()

	adapter := New("gemini-test", srv.URL, nil)
	tools := json.RawMessage(`[{"type":"function","function":{"name":"get_weather","description":"Get weather for a location","parameters":{"type":"object","properties":{"location":{"type":"string"}}}}}]`)
	req := openaischema.ChatCompletionRequest{
		Model: "gemini-pro",
		Messages: []openaischema.ChatMessage{
			{Role: "user", Content: json.RawMessage(`"What's the weather?"`)},
		},
		Tools: tools,
	}

	target := providers.Target{Provider: "gemini", Model: "gemini-pro"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	_, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TEST-7B-01-04: Gemini error response returns *providers.HTTPError
// ---------------------------------------------------------------------------

func TestCompleteChat_ErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "400 bad request",
			statusCode: 400,
			body:       `{"error":{"code":400,"message":"Invalid request","status":"INVALID_ARGUMENT"}}`,
		},
		{
			name:       "401 unauthorized",
			statusCode: 401,
			body:       `{"error":{"code":401,"message":"API key not valid","status":"UNAUTHENTICATED"}}`,
		},
		{
			name:       "429 rate limited",
			statusCode: 429,
			body:       `{"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			adapter := New("gemini-test", srv.URL, nil)
			req := basicRequest()
			target := providers.Target{Provider: "gemini", Model: "gemini-pro"}
			key := providers.ProviderKey{ID: "test", Value: "test-key"}

			_, err := adapter.CompleteChat(context.Background(), req, target, key)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var httpErr *providers.HTTPError
			if !errors.As(err, &httpErr) {
				t.Fatalf("expected *providers.HTTPError, got %T: %v", err, err)
			}
			if httpErr.StatusCode != tc.statusCode {
				t.Errorf("expected status %d, got %d", tc.statusCode, httpErr.StatusCode)
			}
			if httpErr.Body != tc.body {
				t.Errorf("expected body %q, got %q", tc.body, httpErr.Body)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TEST-7B-01-05: Stream completion returns chunks transformed to OpenAI SSE
// format, channel closes on completion
// ---------------------------------------------------------------------------

func TestStreamChat_BasicStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming endpoint
		if !strings.Contains(r.URL.String(), "streamGenerateContent") {
			t.Errorf("expected streamGenerateContent in URL, got %s", r.URL.String())
		}
		if !strings.Contains(r.URL.String(), "alt=sse") {
			t.Errorf("expected alt=sse query param, got %s", r.URL.String())
		}
		// Verify auth
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("expected x-goog-api-key header")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, geminiStreamPayload("Hello ", "")+"\n\n")
		fmt.Fprint(w, geminiStreamPayload("world", "")+"\n\n")
		fmt.Fprint(w, `data: {"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"STOP","index":0}]}`+"\n\n")
	}))
	defer srv.Close()

	adapter := New("gemini-test", srv.URL, nil)
	req := basicRequest()
	target := providers.Target{Provider: "gemini", Model: "gemini-pro"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	result, err := adapter.StreamChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []openaischema.ChatCompletionStreamChunk
	for chunk := range result.Chunks {
		chunks = append(chunks, chunk)
	}
	// Drain errors channel; a closed channel yields zero value (nil),
	// so we must check if the channel still had a real error buffered.
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
// TEST-7B-01-06: Stream error returns error on Errors channel
// ---------------------------------------------------------------------------

func TestStreamChat_StreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"candidates":[{"content":{"parts":[{"text":"partial"}],"role":"model"},"finishReason":"","index":0}]}`+"\n\n")
		fmt.Fprint(w, `data: {"error":{"code":500,"message":"Internal error","status":"INTERNAL"}}`+"\n\n")
	}))
	defer srv.Close()

	adapter := New("gemini-test", srv.URL, nil)
	req := basicRequest()
	target := providers.Target{Provider: "gemini", Model: "gemini-pro"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	result, err := adapter.StreamChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect chunks and errors
	var gotError error
	for chunk := range result.Chunks {
		_ = chunk
	}
	select {
	case err := <-result.Errors:
		gotError = err
	default:
	}

	if gotError == nil {
		t.Fatal("expected an error on the Errors channel, got nil")
	}
	if !strings.Contains(gotError.Error(), "Internal error") {
		t.Errorf("expected error to contain 'Internal error', got %v", gotError)
	}
}

// ---------------------------------------------------------------------------
// TEST-7B-01-07: Model name "gemini-pro" is resolved via model map
// ---------------------------------------------------------------------------

func TestCompleteChat_ModelMapResolution(t *testing.T) {
	var receivedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract the model from the URL path
		parts := strings.Split(r.URL.Path, "/")
		for i, p := range parts {
			if p == "models" && i+1 < len(parts) {
				receivedModel = strings.Split(parts[i+1], ":")[0]
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, geminiResponseJSON("ok", "STOP"))
	}))
	defer srv.Close()

	modelMap := map[string]string{
		"gemini-pro": "gemini-2.0-pro-exp-02-05",
	}
	adapter := New("gemini-test", srv.URL, modelMap)
	req := basicRequest()
	req.Model = "gemini-pro"
	target := providers.Target{Provider: "gemini", Model: "gemini-pro"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	_, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedModel != "gemini-2.0-pro-exp-02-05" {
		t.Errorf("expected model 'gemini-2.0-pro-exp-02-05' in URL, got %q", receivedModel)
	}
}

// ---------------------------------------------------------------------------
// TEST-7B-01-08: Empty model map causes pass-through
// ---------------------------------------------------------------------------

func TestCompleteChat_EmptyModelMapPassthrough(t *testing.T) {
	var receivedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		for i, p := range parts {
			if p == "models" && i+1 < len(parts) {
				receivedModel = strings.Split(parts[i+1], ":")[0]
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, geminiResponseJSON("ok", "STOP"))
	}))
	defer srv.Close()

	adapter := New("gemini-test", srv.URL, nil) // nil model map = pass-through
	req := basicRequest()
	req.Model = "my-custom-model"
	target := providers.Target{Provider: "gemini", Model: "my-custom-model"}
	key := providers.ProviderKey{ID: "test", Value: "test-key"}

	_, err := adapter.CompleteChat(context.Background(), req, target, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedModel != "my-custom-model" {
		t.Errorf("expected model 'my-custom-model' (pass-through), got %q", receivedModel)
	}
}
