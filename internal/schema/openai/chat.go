package openai

import "encoding/json"

type ChatCompletionRequest struct {
	Model               string          `json:"model"`
	Messages            []ChatMessage   `json:"messages"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	N                   *int            `json:"n,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	Stop                json.RawMessage `json:"stop,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
	User                string          `json:"user,omitempty"`
	Tools               json.RawMessage `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat      json.RawMessage `json:"response_format,omitempty"`
}

type ChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type ChatCompletionResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             *Usage   `json:"usage,omitempty"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type ChatCompletionStreamChunk struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []StreamChoice `json:"choices"`
	Usage             *Usage         `json:"usage,omitempty"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
}

type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason,omitempty"`
}

type StreamDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Message         string         `json:"message"`
	Type            string         `json:"type"`
	Code            string         `json:"code,omitempty"`
	Details         map[string]any `json:"details,omitempty"`
	Stage           string         `json:"stage,omitempty"`
	RequestID       string         `json:"request_id,omitempty"`
	AvailableModels []string       `json:"available_models,omitempty"`
}
