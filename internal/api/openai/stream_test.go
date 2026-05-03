package openaiapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openlimit/internal/cache"
	"openlimit/internal/config"
	"openlimit/internal/metrics"
	"openlimit/internal/providers"
	"openlimit/internal/routing"
	openaischema "openlimit/internal/schema/openai"
	"openlimit/internal/tracing"
)

// ---------------------------------------------------------------------------
// Streaming test adapter
// ---------------------------------------------------------------------------

// streamTestAdapter is a test adapter that supports StreamChat with
// controllable chunks and error injection.
type streamTestAdapter struct {
	chunks    []openaischema.ChatCompletionStreamChunk
	streamErr error // if non-nil, sent on Errors channel after chunks
}

func newStreamTestAdapter(chunks []openaischema.ChatCompletionStreamChunk, streamErr error) *streamTestAdapter {
	return &streamTestAdapter{chunks: chunks, streamErr: streamErr}
}

func (a *streamTestAdapter) Name() string { return "stream-test" }

func (a *streamTestAdapter) CompleteChat(_ context.Context, _ openaischema.ChatCompletionRequest, _ providers.Target, _ providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	return nil, nil
}

func (a *streamTestAdapter) StreamChat(_ context.Context, _ openaischema.ChatCompletionRequest, _ providers.Target, _ providers.ProviderKey) (*providers.StreamResult, error) {
	chunks := make(chan openaischema.ChatCompletionStreamChunk, len(a.chunks)+1)
	errs := make(chan error, 1)

	for _, ch := range a.chunks {
		chunks <- ch
	}
	if a.streamErr != nil {
		errs <- a.streamErr
	} else {
		close(chunks)
	}

	return &providers.StreamResult{
		Chunks: chunks,
		Errors: errs,
	}, nil
}

// ---------------------------------------------------------------------------
// Stream test helpers
// ---------------------------------------------------------------------------

func streamTestHandler(t *testing.T, adapter providers.Adapter) *Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := config.Default()
	cfg.Routing.Defaults.TimeoutMS = 5000
	cfg.Routing.Defaults.Retry.Attempts = 1
	cfg.Routing.Defaults.Retry.InitialMS = 1
	cfg.Routing.Defaults.Retry.MaxMS = 2
	cfg.Providers = map[string]config.ProviderConfig{
		"mock": {
			Type: "openai-compatible",
			Keys: []config.ProviderKeyConfig{{ID: "test", Value: "test-key", Weight: 100}},
		},
	}
	cfg.Models = map[string]config.ModelConfig{
		"fast": {Routes: []config.ModelRoute{{Provider: "mock", Model: "provider-model", Weight: 100}}},
	}

	router := routing.New(cfg.Models, cfg.Providers, cfg.Routing, nil)
	c := cache.NewExactLRU(100)
	adapters := map[string]providers.Adapter{"mock": adapter}
	keys := map[string]*providers.KeyRing{
		"mock": providers.NewKeyRing(cfg.Providers["mock"], nil),
	}
	m := metrics.NewCollector(false)
	tr, _ := tracing.NewTracer(false, "", "", 0, logger)

	return NewHandler(cfg, logger, router, c, adapters, keys, nil, nil, m, tr, nil, nil, nil, nil)
}

func makeChunk(text string) openaischema.ChatCompletionStreamChunk {
	return openaischema.ChatCompletionStreamChunk{
		ID:      "chatcmpl-test",
		Object:  "chat.completion.chunk",
		Created: 123,
		Model:   "provider-model",
		Choices: []openaischema.StreamChoice{
			{Index: 0, Delta: openaischema.StreamDelta{Role: "assistant", Content: json.RawMessage(`"` + text + `"`)}},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TEST-7D-02-01: Simulated provider error mid-stream → response body contains
// "event: error", does NOT contain "data: [DONE]".
func TestStreamChat_ProviderError_SendsErrorEvent(t *testing.T) {
	// Adapter sends one chunk, then an error (simulating provider failure mid-stream)
	adapter := newStreamTestAdapter(
		[]openaischema.ChatCompletionStreamChunk{makeChunk("Hello")},
		&providers.HTTPError{StatusCode: 500, Body: "internal server error"},
	)

	h := streamTestHandler(t, adapter)

	// Build streaming request
	body := `{"model":"fast","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	bodyStr := string(bodyBytes)

	// Must contain SSE error event
	if !strings.Contains(bodyStr, "event: error") {
		t.Errorf("response body does not contain 'event: error'\nbody:\n%s", bodyStr)
	}

	// Must contain provider_error type in the error data
	if !strings.Contains(bodyStr, "provider_error") {
		t.Errorf("response body does not contain 'provider_error'\nbody:\n%s", bodyStr)
	}

	// Must NOT contain data: [DONE] (stream interrupted before completion)
	if strings.Contains(bodyStr, "data: [DONE]") {
		t.Errorf("response body should NOT contain 'data: [DONE]' on error\nbody:\n%s", bodyStr)
	}

	// Verify the enriched error message is present
	if !strings.Contains(bodyStr, "internal error") {
		t.Errorf("response body should contain enriched error message with 'internal error'\nbody:\n%s", bodyStr)
	}
}

// TEST-7D-02-02: Normal stream completion → response body does NOT contain
// "event: error", ends with "data: [DONE]".
func TestStreamChat_NormalCompletion_NoErrorEvent(t *testing.T) {
	// Adapter sends two chunks and no error (normal completion)
	adapter := newStreamTestAdapter(
		[]openaischema.ChatCompletionStreamChunk{
			makeChunk("Hello"),
			makeChunk(" world"),
		},
		nil, // no error
	)

	h := streamTestHandler(t, adapter)

	body := `{"model":"fast","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	bodyStr := string(bodyBytes)

	// Must NOT contain event: error
	if strings.Contains(bodyStr, "event: error") {
		t.Errorf("response body should NOT contain 'event: error' on normal completion\nbody:\n%s", bodyStr)
	}

	// Must end with data: [DONE]
	if !strings.Contains(bodyStr, "data: [DONE]") {
		t.Errorf("response body must contain 'data: [DONE]' on normal completion\nbody:\n%s", bodyStr)
	}

	// Should contain the streamed chunks
	if !strings.Contains(bodyStr, "Hello") {
		t.Errorf("response body should contain 'Hello'\nbody:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, " world") {
		t.Errorf("response body should contain ' world'\nbody:\n%s", bodyStr)
	}
}

// ---------------------------------------------------------------------------
// extractHTTPStatus unit tests
// ---------------------------------------------------------------------------

// TEST-7D-02-03 (supporting): extractHTTPStatus returns status from HTTPError.
func TestExtractHTTPStatus_HTTPError(t *testing.T) {
	err := &providers.HTTPError{StatusCode: 429, Body: "rate limited"}
	if got := extractHTTPStatus(err); got != 429 {
		t.Errorf("extractHTTPStatus(HTTPError{429}) = %d, want 429", got)
	}
}

// TEST-7D-02-04 (supporting): extractHTTPStatus returns 0 for non-HTTPError.
func TestExtractHTTPStatus_NonHTTPError(t *testing.T) {
	var v any
	if got := extractHTTPStatus(json.Unmarshal([]byte("bad"), &v)); got != 0 {
		t.Errorf("extractHTTPStatus(non-HTTPError) = %d, want 0", got)
	}
}
