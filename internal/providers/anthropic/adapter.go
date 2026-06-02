package anthropicadapter

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

const anthropicVersion = "2023-06-01"

type Adapter struct {
	name       string
	baseURL    string
	httpClient *http.Client
}

type messageRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      any                `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type messageResponse struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Role         string                 `json:"role"`
	Model        string                 `json:"model"`
	Content      []contentBlock         `json:"content"`
	StopReason   string                 `json:"stop_reason"`
	StopSequence string                 `json:"stop_sequence"`
	Usage        anthropicUsage         `json:"usage"`
	Extra        map[string]interface{} `json:"-"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type streamEvent struct {
	Type         string           `json:"type"`
	Message      *messageResponse `json:"message,omitempty"`
	Index        int              `json:"index,omitempty"`
	Delta        streamDelta      `json:"delta,omitempty"`
	Usage        *anthropicUsage  `json:"usage,omitempty"`
	ContentBlock *contentBlock    `json:"content_block,omitempty"`
}

type streamDelta struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

func New(name string, baseURL string) *Adapter {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &Adapter{
		name:       name,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 0},
	}
}

func (a *Adapter) Name() string {
	return a.name
}

func (a *Adapter) CompleteChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	providerReq, err := toAnthropicRequest(req, target, false)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(providerReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	baseURL := a.baseURL
	if target.BaseURL != "" {
		baseURL = target.BaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setHeaders(httpReq, key)

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
		return nil, fmt.Errorf("read anthropic response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &providers.HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	var out messageResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode anthropic response after %s: %w", time.Since(start), err)
	}

	return fromAnthropicResponse(out, req.Model), nil
}

func (a *Adapter) StreamChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*providers.StreamResult, error) {
	providerReq, err := toAnthropicRequest(req, target, true)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(providerReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	baseURL := a.baseURL
	if target.BaseURL != "" {
		baseURL = target.BaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setHeaders(httpReq, key)
	httpReq.Header.Set("Accept", "text/event-stream")

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

		messageID := ""
		model := req.Model
		created := time.Now().Unix()
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				return
			}

			var event streamEvent
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				errs <- fmt.Errorf("decode anthropic stream event: %w", err)
				return
			}

			chunk, ok := anthropicEventToOpenAIChunk(event, &messageID, &model, &created)
			if !ok {
				continue
			}

			select {
			case chunks <- chunk:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errs <- fmt.Errorf("read anthropic stream: %w", err)
		}
	}()

	return &providers.StreamResult{Chunks: chunks, Errors: errs}, nil
}

func setHeaders(req *http.Request, key providers.ProviderKey) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", anthropicVersion)
	if key.Value != "" {
		req.Header.Set("x-api-key", key.Value)
	}
}

func toAnthropicRequest(req openaischema.ChatCompletionRequest, target providers.Target, stream bool) (messageRequest, error) {
	messages := make([]anthropicMessage, 0, len(req.Messages))
	var systemParts []any

	for _, msg := range req.Messages {
		content := rawContentToAnthropic(msg.Content)
		switch msg.Role {
		case "system":
			systemParts = append(systemParts, content)
		case "assistant", "user":
			messages = append(messages, anthropicMessage{Role: msg.Role, Content: content})
		case "tool":
			// Anthropic has a different tool result shape. Preserve tool content as user text for this MVP adapter.
			messages = append(messages, anthropicMessage{Role: "user", Content: content})
		default:
			messages = append(messages, anthropicMessage{Role: "user", Content: content})
		}
	}

	maxTokens := 1024
	if req.MaxCompletionTokens != nil && *req.MaxCompletionTokens > 0 {
		maxTokens = *req.MaxCompletionTokens
	} else if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}

	out := messageRequest{
		Model:       target.Model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      stream,
	}
	if len(systemParts) == 1 {
		out.System = systemParts[0]
	} else if len(systemParts) > 1 {
		out.System = systemParts
	}
	return out, nil
}

func rawContentToAnthropic(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}
	return string(raw)
}

func fromAnthropicResponse(resp messageResponse, logicalModel string) *openaischema.ChatCompletionResponse {
	text := strings.Builder{}
	for _, block := range resp.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	content, _ := json.Marshal(text.String())
	finishReason := mapStopReason(resp.StopReason)

	model := logicalModel
	if model == "" {
		model = resp.Model
	}

	return &openaischema.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openaischema.Choice{{
			Index: 0,
			Message: openaischema.ChatMessage{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: finishReason,
		}},
		Usage: &openaischema.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

func anthropicEventToOpenAIChunk(event streamEvent, messageID *string, model *string, created *int64) (openaischema.ChatCompletionStreamChunk, bool) {
	switch event.Type {
	case "message_start":
		if event.Message != nil {
			*messageID = event.Message.ID
			if event.Message.Model != "" {
				*model = event.Message.Model
			}
		}
		return openaischema.ChatCompletionStreamChunk{
			ID:      *messageID,
			Object:  "chat.completion.chunk",
			Created: *created,
			Model:   *model,
			Choices: []openaischema.StreamChoice{{Index: 0, Delta: openaischema.StreamDelta{Role: "assistant"}}},
		}, true

	case "content_block_delta":
		if event.Delta.Text == "" {
			return openaischema.ChatCompletionStreamChunk{}, false
		}
		content, _ := json.Marshal(event.Delta.Text)
		return openaischema.ChatCompletionStreamChunk{
			ID:      *messageID,
			Object:  "chat.completion.chunk",
			Created: *created,
			Model:   *model,
			Choices: []openaischema.StreamChoice{{Index: event.Index, Delta: openaischema.StreamDelta{Content: content}}},
		}, true

	case "message_delta":
		finishReason := mapStopReason(event.Delta.StopReason)
		return openaischema.ChatCompletionStreamChunk{
			ID:      *messageID,
			Object:  "chat.completion.chunk",
			Created: *created,
			Model:   *model,
			Choices: []openaischema.StreamChoice{{Index: 0, Delta: openaischema.StreamDelta{}, FinishReason: &finishReason}},
			Usage:   usageFromAnthropic(event.Usage),
		}, true

	default:
		return openaischema.ChatCompletionStreamChunk{}, false
	}
}

func usageFromAnthropic(usage *anthropicUsage) *openaischema.Usage {
	if usage == nil {
		return nil
	}
	return &openaischema.Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.InputTokens + usage.OutputTokens,
	}
}

func mapStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}
