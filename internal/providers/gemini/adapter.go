package gemini

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

// Adapter implements providers.Adapter for the Google Gemini API.
type Adapter struct {
	name       string
	baseURL    string
	modelMap   map[string]string
	httpClient *http.Client
}

// New creates a new Gemini adapter.
// baseURL defaults to "https://generativelanguage.googleapis.com" if empty.
// modelMap maps logical model names to Gemini model IDs; empty map = pass-through.
func New(name string, baseURL string, modelMap map[string]string) *Adapter {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	return &Adapter{
		name:       name,
		baseURL:    strings.TrimRight(baseURL, "/"),
		modelMap:   modelMap,
		httpClient: &http.Client{Timeout: 0},
	}
}

// Name returns the adapter name.
func (a *Adapter) Name() string {
	return a.name
}

// resolveModel maps a logical model name to the Gemini model ID.
// If the model is not in the map, it is used as-is (pass-through).
func (a *Adapter) resolveModel(model string) string {
	if a.modelMap == nil {
		return model
	}
	if mapped, ok := a.modelMap[model]; ok {
		return mapped
	}
	return model
}

// ---------------------------------------------------------------------------
// Gemini request/response types
// ---------------------------------------------------------------------------

type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	Tools             []geminiTool     `json:"tools,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text         string              `json:"text,omitempty"`
	FunctionCall *geminiFunctionCall `json:"functionCall,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

type geminiFunctionDecl struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type geminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

type geminiGenConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
	Error         *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
	Index        int           `json:"index,omitempty"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// ---------------------------------------------------------------------------
// Streaming types
// ---------------------------------------------------------------------------

type geminiStreamResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
	Error         *geminiError      `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// CompleteChat
// ---------------------------------------------------------------------------

func (a *Adapter) CompleteChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	geminiModel := a.resolveModel(target.Model)
	geminiReq := toGeminiRequest(req)

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini request: %w", err)
	}

	baseURL := a.baseURL
	if target.BaseURL != "" {
		baseURL = target.BaseURL
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", baseURL, geminiModel)

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
		return nil, fmt.Errorf("read gemini response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &providers.HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	var out geminiResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode gemini response after %s: %w", time.Since(start), err)
	}

	if out.Error != nil {
		return nil, fmt.Errorf("gemini error %d: %s", out.Error.Code, out.Error.Message)
	}

	return fromGeminiResponse(out, req.Model, geminiModel), nil
}

// ---------------------------------------------------------------------------
// StreamChat
// ---------------------------------------------------------------------------

func (a *Adapter) StreamChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*providers.StreamResult, error) {
	geminiModel := a.resolveModel(target.Model)
	geminiReq := toGeminiRequest(req)

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini request: %w", err)
	}

	baseURL := a.baseURL
	if target.BaseURL != "" {
		baseURL = target.BaseURL
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", baseURL, geminiModel)

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

		messageID := fmt.Sprintf("gemini-%d", time.Now().UnixNano())
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

			var event geminiStreamResponse
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				errs <- fmt.Errorf("decode gemini stream event: %w", err)
				return
			}

			if event.Error != nil {
				errs <- fmt.Errorf("gemini stream error %d: %s", event.Error.Code, event.Error.Message)
				return
			}

			chunk, ok := geminiEventToOpenAIChunk(event, messageID, model, created)
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
			errs <- fmt.Errorf("read gemini stream: %w", err)
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
		req.Header.Set("x-goog-api-key", key.Value)
	}
}

func toGeminiRequest(req openaischema.ChatCompletionRequest) geminiRequest {
	var contents []geminiContent
	var systemInstruction *geminiContent

	for _, msg := range req.Messages {
		text := contentToString(msg.Content)

		if msg.Role == "system" {
			systemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: text}},
			}
			continue
		}

		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: text}},
		})
	}

	gr := geminiRequest{
		Contents: contents,
	}

	if systemInstruction != nil {
		gr.SystemInstruction = systemInstruction
	}

	// Map tools if present
	if len(req.Tools) > 0 {
		gr.Tools = mapTools(req.Tools)
	}

	// Generation config
	gc := &geminiGenConfig{}
	hasConfig := false
	if req.Temperature != nil {
		gc.Temperature = req.Temperature
		hasConfig = true
	}
	if req.TopP != nil {
		gc.TopP = req.TopP
		hasConfig = true
	}
	if req.MaxCompletionTokens != nil && *req.MaxCompletionTokens > 0 {
		gc.MaxOutputTokens = req.MaxCompletionTokens
		hasConfig = true
	} else if req.MaxTokens != nil && *req.MaxTokens > 0 {
		gc.MaxOutputTokens = req.MaxTokens
		hasConfig = true
	}
	if hasConfig {
		gr.GenerationConfig = gc
	}

	return gr
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

func mapTools(toolsRaw json.RawMessage) []geminiTool {
	var openaiTools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description,omitempty"`
			Parameters  map[string]interface{} `json:"parameters,omitempty"`
		} `json:"function"`
	}
	if err := json.Unmarshal(toolsRaw, &openaiTools); err != nil {
		return nil
	}

	var declarations []geminiFunctionDecl
	for _, t := range openaiTools {
		if t.Type != "function" {
			continue
		}
		declarations = append(declarations, geminiFunctionDecl{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		})
	}

	if len(declarations) == 0 {
		return nil
	}
	return []geminiTool{{FunctionDeclarations: declarations}}
}

func fromGeminiResponse(resp geminiResponse, logicalModel string, geminiModel string) *openaischema.ChatCompletionResponse {
	content := ""
	finishReason := "stop"

	if len(resp.Candidates) > 0 {
		cand := resp.Candidates[0]
		for _, part := range cand.Content.Parts {
			content += part.Text
		}
		finishReason = mapFinishReason(cand.FinishReason)
	}

	contentJSON, _ := json.Marshal(content)
	model := logicalModel
	if model == "" {
		model = geminiModel
	}

	usage := &openaischema.Usage{}
	if resp.UsageMetadata != nil {
		usage.PromptTokens = resp.UsageMetadata.PromptTokenCount
		usage.CompletionTokens = resp.UsageMetadata.CandidatesTokenCount
		usage.TotalTokens = resp.UsageMetadata.TotalTokenCount
	}

	return &openaischema.ChatCompletionResponse{
		ID:      fmt.Sprintf("gemini-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openaischema.Choice{{
			Index: 0,
			Message: openaischema.ChatMessage{
				Role:    "assistant",
				Content: contentJSON,
			},
			FinishReason: finishReason,
		}},
		Usage: usage,
	}
}

func geminiEventToOpenAIChunk(event geminiStreamResponse, messageID string, model string, created int64) (openaischema.ChatCompletionStreamChunk, bool) {
	if len(event.Candidates) == 0 {
		return openaischema.ChatCompletionStreamChunk{}, false
	}

	cand := event.Candidates[0]
	text := ""
	for _, part := range cand.Content.Parts {
		text += part.Text
	}

	// If there's text content, emit a content chunk
	if text != "" {
		contentJSON, _ := json.Marshal(text)
		return openaischema.ChatCompletionStreamChunk{
			ID:      messageID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []openaischema.StreamChoice{{
				Index: cand.Index,
				Delta: openaischema.StreamDelta{Content: contentJSON},
			}},
		}, true
	}

	// If finish reason is set, emit a finish chunk
	if cand.FinishReason != "" {
		finishReason := mapFinishReason(cand.FinishReason)
		return openaischema.ChatCompletionStreamChunk{
			ID:      messageID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []openaischema.StreamChoice{{
				Index:        cand.Index,
				Delta:        openaischema.StreamDelta{},
				FinishReason: &finishReason,
			}},
		}, true
	}

	return openaischema.ChatCompletionStreamChunk{}, false
}

func mapFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}
