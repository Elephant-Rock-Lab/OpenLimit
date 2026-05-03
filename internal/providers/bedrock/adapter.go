package bedrock

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

// Adapter implements providers.Adapter for AWS Bedrock using the Converse API.
type Adapter struct {
	name       string
	baseURL    string
	httpClient *http.Client
}

// New creates a new Bedrock adapter.
// baseURL defaults to "https://bedrock-runtime.us-east-1.amazonaws.com" if empty.
func New(name string, baseURL string) *Adapter {
	if baseURL == "" {
		baseURL = "https://bedrock-runtime.us-east-1.amazonaws.com"
	}
	return &Adapter{
		name:       name,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 0},
	}
}

// Name returns the adapter name.
func (a *Adapter) Name() string {
	return a.name
}

// ---------------------------------------------------------------------------
// Bedrock Converse API request types
// ---------------------------------------------------------------------------

type converseRequest struct {
	Messages        []bedrockMessage   `json:"messages"`
	System          []bedrockSystemMsg `json:"system,omitempty"`
	InferenceConfig *inferenceConfig   `json:"inferenceConfig,omitempty"`
}

type bedrockMessage struct {
	Role    string           `json:"role"`
	Content []bedrockContent `json:"content"`
}

type bedrockContent struct {
	Text string `json:"text,omitempty"`
}

type bedrockSystemMsg struct {
	Text string `json:"text"`
}

type inferenceConfig struct {
	MaxTokens   int      `json:"maxTokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"topP,omitempty"`
}

// ---------------------------------------------------------------------------
// Bedrock Converse API response types
// ---------------------------------------------------------------------------

type converseResponse struct {
	Output struct {
		Message bedrockMessage `json:"message"`
	} `json:"output"`
	StopReason string       `json:"stopReason"`
	Usage      bedrockUsage `json:"usage"`
}

type bedrockUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// ---------------------------------------------------------------------------
// Bedrock ConverseStream event types (SSE-delimited for testability)
// ---------------------------------------------------------------------------

type streamEvent struct {
	Type              string        `json:"type"`
	Role              string        `json:"role,omitempty"`
	ContentBlockIndex int           `json:"contentBlockIndex,omitempty"`
	Delta             *streamDelta  `json:"delta,omitempty"`
	StopReason        string        `json:"stopReason,omitempty"`
	Usage             *bedrockUsage `json:"usage,omitempty"`
}

type streamDelta struct {
	Text string `json:"text,omitempty"`
}

// ---------------------------------------------------------------------------
// CompleteChat
// ---------------------------------------------------------------------------

func (a *Adapter) CompleteChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	bedrockReq := toBedrockRequest(req)

	body, err := json.Marshal(bedrockReq)
	if err != nil {
		return nil, fmt.Errorf("marshal bedrock request: %w", err)
	}

	base := a.baseURL
	if target.BaseURL != "" {
		base = target.BaseURL
	}

	endpoint := fmt.Sprintf("%s/model/%s/converse", base, target.Model)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read bedrock response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &providers.HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	var out converseResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode bedrock response after %s: %w", time.Since(start), err)
	}

	return fromBedrockResponse(out, req.Model), nil
}

// ---------------------------------------------------------------------------
// StreamChat
// ---------------------------------------------------------------------------

func (a *Adapter) StreamChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*providers.StreamResult, error) {
	bedrockReq := toBedrockRequest(req)

	body, err := json.Marshal(bedrockReq)
	if err != nil {
		return nil, fmt.Errorf("marshal bedrock request: %w", err)
	}

	base := a.baseURL
	if target.BaseURL != "" {
		base = target.BaseURL
	}

	endpoint := fmt.Sprintf("%s/model/%s/converse-stream", base, target.Model)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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

		messageID := fmt.Sprintf("bedrock-%d", time.Now().UnixNano())
		model := req.Model
		created := time.Now().Unix()

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

			var event streamEvent
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				errs <- fmt.Errorf("decode bedrock stream event: %w", err)
				return
			}

			chunk, ok := bedrockEventToOpenAIChunk(event, messageID, model, created)
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
			errs <- fmt.Errorf("read bedrock stream: %w", err)
		}
	}()

	return &providers.StreamResult{Chunks: chunks, Errors: errs}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func setHeaders(req *http.Request, key providers.ProviderKey) {
	req.Header.Set("Content-Type", "application/json")
	if key.Value != "" {
		req.Header.Set("x-api-key", key.Value)
	}
}

func toBedrockRequest(req openaischema.ChatCompletionRequest) converseRequest {
	var messages []bedrockMessage
	var system []bedrockSystemMsg

	for _, msg := range req.Messages {
		text := contentToString(msg.Content)

		if msg.Role == "system" {
			system = append(system, bedrockSystemMsg{Text: text})
			continue
		}

		role := msg.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}

		messages = append(messages, bedrockMessage{
			Role:    role,
			Content: []bedrockContent{{Text: text}},
		})
	}

	out := converseRequest{
		Messages: messages,
	}
	if len(system) > 0 {
		out.System = system
	}

	maxTokens := 0
	if req.MaxCompletionTokens != nil && *req.MaxCompletionTokens > 0 {
		maxTokens = *req.MaxCompletionTokens
	} else if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}

	if maxTokens > 0 || req.Temperature != nil || req.TopP != nil {
		out.InferenceConfig = &inferenceConfig{
			MaxTokens:   maxTokens,
			Temperature: req.Temperature,
			TopP:        req.TopP,
		}
	}

	return out
}

func contentToString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func fromBedrockResponse(resp converseResponse, logicalModel string) *openaischema.ChatCompletionResponse {
	text := ""
	for _, block := range resp.Output.Message.Content {
		text += block.Text
	}
	content, _ := json.Marshal(text)
	finishReason := mapStopReason(resp.StopReason)

	model := logicalModel
	if model == "" {
		model = "bedrock"
	}

	return &openaischema.ChatCompletionResponse{
		ID:      fmt.Sprintf("bedrock-%d", time.Now().UnixNano()),
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

func bedrockEventToOpenAIChunk(event streamEvent, messageID string, model string, created int64) (openaischema.ChatCompletionStreamChunk, bool) {
	switch event.Type {
	case "messageStart":
		return openaischema.ChatCompletionStreamChunk{
			ID:      messageID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []openaischema.StreamChoice{{Index: 0, Delta: openaischema.StreamDelta{Role: "assistant"}}},
		}, true

	case "contentBlockDelta":
		if event.Delta == nil || event.Delta.Text == "" {
			return openaischema.ChatCompletionStreamChunk{}, false
		}
		content, _ := json.Marshal(event.Delta.Text)
		return openaischema.ChatCompletionStreamChunk{
			ID:      messageID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []openaischema.StreamChoice{{
				Index: event.ContentBlockIndex,
				Delta: openaischema.StreamDelta{Content: content},
			}},
		}, true

	case "messageStop":
		finishReason := mapStopReason(event.StopReason)
		return openaischema.ChatCompletionStreamChunk{
			ID:      messageID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []openaischema.StreamChoice{{
				Index:        0,
				Delta:        openaischema.StreamDelta{},
				FinishReason: &finishReason,
			}},
			Usage: usageFromBedrock(event.Usage),
		}, true

	default:
		return openaischema.ChatCompletionStreamChunk{}, false
	}
}

func usageFromBedrock(usage *bedrockUsage) *openaischema.Usage {
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
	case "guardrail_intervened", "content_filtered":
		return "content_filter"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}

// Compile-time interface compliance check.
var _ providers.Adapter = (*Adapter)(nil)
