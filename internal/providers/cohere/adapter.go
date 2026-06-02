package cohere

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

const defaultBaseURL = "https://api.cohere.com/v2"

// Adapter implements providers.Adapter for the Cohere v2 Chat API.
type Adapter struct {
	baseURL    string
	httpClient *http.Client
}

// ---------- Cohere v2 request/response types ----------

type cohereRequest struct {
	Model       string          `json:"model"`
	Message     string          `json:"message"`
	ChatHistory []cohereMessage `json:"chat_history,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type cohereMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type cohereResponse struct {
	ID      string        `json:"id"`
	Message cohereRespMsg `json:"message"`
	Usage   cohereUsage   `json:"usage,omitempty"`
}

type cohereRespMsg struct {
	Content []cohereContentBlock `json:"content"`
}

type cohereContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type cohereUsage struct {
	Tokens cohereTokens `json:"tokens"`
}

type cohereTokens struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// ---------- SSE stream types ----------

type cohereStreamEvent struct {
	Type   string              `json:"type"`
	Delta  *cohereStreamDelta  `json:"delta,omitempty"`
	Finish *cohereStreamFinish `json:"finish_reason,omitempty"`
}

type cohereStreamDelta struct {
	Content *cohereDeltaContent `json:"content,omitempty"`
}

type cohereDeltaContent struct {
	Text string `json:"text"`
}

type cohereStreamFinish struct {
	Reason string `json:"reason"`
}

// ---------- Constructor ----------

func New(baseURL string) *Adapter {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Adapter{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 0},
	}
}

// ---------- Interface implementation ----------

// Compile-time check that Adapter implements providers.Adapter.
var _ providers.Adapter = (*Adapter)(nil)

func (a *Adapter) Name() string {
	return "cohere"
}

func (a *Adapter) CompleteChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	cohereReq := toCohereRequest(req, target, false)

	body, err := json.Marshal(cohereReq)
	if err != nil {
		return nil, fmt.Errorf("marshal cohere request: %w", err)
	}

	baseURL := a.baseURL
	if target.BaseURL != "" {
		baseURL = target.BaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat", bytes.NewReader(body))
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
		return nil, fmt.Errorf("read cohere response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &providers.HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	var out cohereResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode cohere response after %s: %w", time.Since(start), err)
	}

	return fromCohereResponse(out, req.Model), nil
}

func (a *Adapter) StreamChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*providers.StreamResult, error) {
	cohereReq := toCohereRequest(req, target, true)

	body, err := json.Marshal(cohereReq)
	if err != nil {
		return nil, fmt.Errorf("marshal cohere request: %w", err)
	}

	baseURL := a.baseURL
	if target.BaseURL != "" {
		baseURL = target.BaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat", bytes.NewReader(body))
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
				// Send a final chunk with finish_reason "stop"
				fr := "stop"
				select {
				case chunks <- openaischema.ChatCompletionStreamChunk{
					ID:      messageID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []openaischema.StreamChoice{{
						Index:        0,
						Delta:        openaischema.StreamDelta{},
						FinishReason: &fr,
					}},
				}:
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				}
				return
			}

			var event cohereStreamEvent
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				errs <- fmt.Errorf("decode cohere stream event: %w", err)
				return
			}

			chunk, ok := cohereEventToOpenAIChunk(event, &messageID, &model, &created)
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
			errs <- fmt.Errorf("read cohere stream: %w", err)
		}
	}()

	return &providers.StreamResult{Chunks: chunks, Errors: errs}, nil
}

// ---------- Helpers ----------

func setHeaders(req *http.Request, key providers.ProviderKey) {
	req.Header.Set("Content-Type", "application/json")
	if key.Value != "" {
		req.Header.Set("Authorization", "Bearer "+key.Value)
	}
}

func toCohereRequest(req openaischema.ChatCompletionRequest, target providers.Target, stream bool) cohereRequest {
	// Extract text from OpenAI messages.
	// Cohere v2: last user message goes in "message" field, prior messages go in "chat_history".
	var history []cohereMessage
	var lastUserMsg string

	for _, msg := range req.Messages {
		text := extractText(msg.Content)
		role := msg.Role
		// Cohere uses "USER" and "CHATBOT" for chat_history roles
		switch role {
		case "user":
			history = append(history, cohereMessage{Role: "USER", Content: text})
			lastUserMsg = text
		case "assistant":
			history = append(history, cohereMessage{Role: "CHATBOT", Content: text})
		case "system":
			history = append(history, cohereMessage{Role: "SYSTEM", Content: text})
		default:
			history = append(history, cohereMessage{Role: "USER", Content: text})
			lastUserMsg = text
		}
	}

	// Remove last user message from history (it becomes the top-level "message")
	if len(history) > 0 {
		// Find the last USER entry
		lastUserIdx := -1
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == "USER" {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx >= 0 {
			history = append(history[:lastUserIdx], history[lastUserIdx+1:]...)
		}
	}

	model := target.Model
	if model == "" {
		model = req.Model
	}

	return cohereRequest{
		Model:       model,
		Message:     lastUserMsg,
		ChatHistory: history,
		Stream:      stream,
	}
}

func extractText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func fromCohereResponse(resp cohereResponse, logicalModel string) *openaischema.ChatCompletionResponse {
	text := strings.Builder{}
	for _, block := range resp.Message.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	content, _ := json.Marshal(text.String())

	model := logicalModel

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
			FinishReason: "stop",
		}},
		Usage: &openaischema.Usage{
			PromptTokens:     resp.Usage.Tokens.Input,
			CompletionTokens: resp.Usage.Tokens.Output,
			TotalTokens:      resp.Usage.Tokens.Input + resp.Usage.Tokens.Output,
		},
	}
}

func cohereEventToOpenAIChunk(event cohereStreamEvent, messageID *string, model *string, created *int64) (openaischema.ChatCompletionStreamChunk, bool) {
	switch event.Type {
	case "message-start":
		// First event — set up the role
		return openaischema.ChatCompletionStreamChunk{
			ID:      *messageID,
			Object:  "chat.completion.chunk",
			Created: *created,
			Model:   *model,
			Choices: []openaischema.StreamChoice{{
				Index: 0,
				Delta: openaischema.StreamDelta{Role: "assistant"},
			}},
		}, true

	case "content-delta":
		if event.Delta == nil || event.Delta.Content == nil || event.Delta.Content.Text == "" {
			return openaischema.ChatCompletionStreamChunk{}, false
		}
		content, _ := json.Marshal(event.Delta.Content.Text)
		return openaischema.ChatCompletionStreamChunk{
			ID:      *messageID,
			Object:  "chat.completion.chunk",
			Created: *created,
			Model:   *model,
			Choices: []openaischema.StreamChoice{{
				Index: 0,
				Delta: openaischema.StreamDelta{Content: content},
			}},
		}, true

	default:
		return openaischema.ChatCompletionStreamChunk{}, false
	}
}
