package groq

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"openlimit/internal/providers"
	openaischema "openlimit/internal/schema/openai"
)

// Compile-time check that Adapter implements providers.Adapter.
var _ providers.Adapter = (*Adapter)(nil)

// Adapter implements providers.Adapter for Groq.
// Groq exposes an OpenAI-compatible API, so request/response shapes are
// identical to OpenAI. Only the default base URL differs:
//
//	https://api.groq.com/openai/v1
type Adapter struct {
	baseURL    string
	httpClient *http.Client
}

const DefaultBaseURL = "https://api.groq.com/openai/v1"

// New creates a new Groq adapter.
// If baseURL is empty the DefaultBaseURL is used.
func New(baseURL string) *Adapter {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Adapter{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 0},
	}
}

// Name returns "groq".
func (a *Adapter) Name() string {
	return "groq"
}

// CompleteChat sends a non-streaming chat completion request to Groq.
func (a *Adapter) CompleteChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	providerReq := req
	providerReq.Model = target.Model
	providerReq.Stream = false

	baseURL := a.baseURL
	if target.BaseURL != "" {
		baseURL = target.BaseURL
	}

	body, err := json.Marshal(providerReq)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key.Value != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key.Value)
	}

	start := time.Now()
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", providers.ErrRetryable, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, providers.MaxProviderResponseSize))
	if err != nil {
		return nil, fmt.Errorf("read provider response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &providers.HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	var out openaischema.ChatCompletionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode provider response after %s: %w", time.Since(start), err)
	}
	if out.Model == "" {
		out.Model = req.Model
	}
	return &out, nil
}

// StreamChat sends a streaming chat completion request to Groq.
func (a *Adapter) StreamChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*providers.StreamResult, error) {
	providerReq := req
	providerReq.Model = target.Model
	providerReq.Stream = true

	baseURL := a.baseURL
	if target.BaseURL != "" {
		baseURL = target.BaseURL
	}

	body, err := json.Marshal(providerReq)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if key.Value != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key.Value)
	}

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", providers.ErrRetryable, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, providers.MaxProviderResponseSize))
		return nil, &providers.HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	chunks := make(chan openaischema.ChatCompletionStreamChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				return
			}

			var chunk openaischema.ChatCompletionStreamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				errs <- fmt.Errorf("decode stream chunk: %w", err)
				return
			}
			if chunk.Model == "" {
				chunk.Model = req.Model
			}

			select {
			case chunks <- chunk:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errs <- fmt.Errorf("read stream: %w", err)
		}
	}()

	return &providers.StreamResult{Chunks: chunks, Errors: errs}, nil
}
