package azure

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

// Adapter implements providers.Adapter for Azure OpenAI.
// Azure uses the same request/response shapes as OpenAI but differs in
// URL construction (deployment-based) and authentication (api-key header).
type Adapter struct {
	name       string
	resource   string // Azure resource name (e.g. "my-account")
	apiVersion string // Azure API version (defaults to "2025-06-01")
	httpClient *http.Client
}

// New creates a new Azure OpenAI adapter.
//   - name: logical adapter name
//   - resource: Azure resource name ({resource}.openai.azure.com)
//   - apiVersion: Azure API version; empty string defaults to "2025-06-01"
func New(name, resource, apiVersion string) *Adapter {
	if apiVersion == "" {
		apiVersion = "2025-06-01"
	}
	return &Adapter{
		name:       name,
		resource:   resource,
		apiVersion: apiVersion,
		httpClient: &http.Client{Timeout: 0},
	}
}

// Name returns the adapter's logical name.
func (a *Adapter) Name() string {
	return a.name
}

// buildURL constructs the Azure OpenAI endpoint URL.
// Format: https://{resource}.openai.azure.com/openai/deployments/{deployment}/chat/completions?api-version={version}
func (a *Adapter) buildURL(deployment string) string {
	return fmt.Sprintf(
		"https://%s.openai.azure.com/openai/deployments/%s/chat/completions?api-version=%s",
		a.resource, deployment, a.apiVersion,
	)
}

// CompleteChat sends a non-streaming chat completion request to Azure OpenAI.
func (a *Adapter) CompleteChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	providerReq := req
	providerReq.Model = target.Model // target.Model = deployment name
	providerReq.Stream = false

	body, err := json.Marshal(providerReq)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}

	url := a.buildURL(target.Model)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key.Value != "" {
		httpReq.Header.Set("api-key", key.Value)
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

	data, err := io.ReadAll(resp.Body)
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

// StreamChat sends a streaming chat completion request to Azure OpenAI.
func (a *Adapter) StreamChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*providers.StreamResult, error) {
	providerReq := req
	providerReq.Model = target.Model
	providerReq.Stream = true

	body, err := json.Marshal(providerReq)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}

	url := a.buildURL(target.Model)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if key.Value != "" {
		httpReq.Header.Set("api-key", key.Value)
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
		data, _ := io.ReadAll(resp.Body)
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
