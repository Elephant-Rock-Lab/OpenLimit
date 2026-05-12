// Package vertex implements the providers.Adapter interface for Google Vertex AI.
//
// Vertex AI uses the same Gemini generateContent API surface but with a different
// endpoint structure and OAuth2 Bearer token authentication instead of API keys.
package vertex

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

// Adapter implements providers.Adapter for Google Vertex AI.
type Adapter struct {
	name       string
	project    string
	region     string
	publisher  string
	httpClient *http.Client
}

// New creates a new Vertex AI adapter.
//   - project: GCP project ID
//   - region: Vertex AI region (e.g. "us-central1")
//   - publisher: model publisher (defaults to "google" if empty)
func New(name, project, region, publisher string) *Adapter {
	if publisher == "" {
		publisher = "google"
	}
	return &Adapter{
		name:       name,
		project:    project,
		region:     region,
		publisher:  publisher,
		httpClient: &http.Client{Timeout: 0},
	}
}

// Name returns the adapter name.
func (a *Adapter) Name() string {
	return a.name
}

// ---------------------------------------------------------------------------
// Vertex AI (Gemini-compatible) request/response types
// ---------------------------------------------------------------------------

type vertexRequest struct {
	Contents          []vertexContent  `json:"contents"`
	SystemInstruction *vertexContent   `json:"systemInstruction,omitempty"`
	Tools             []vertexTool     `json:"tools,omitempty"`
	GenerationConfig  *vertexGenConfig `json:"generationConfig,omitempty"`
}

type vertexContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []vertexPart `json:"parts"`
}

type vertexPart struct {
	Text         string              `json:"text,omitempty"`
	FunctionCall *vertexFunctionCall `json:"functionCall,omitempty"`
}

type vertexTool struct {
	FunctionDeclarations []vertexFunctionDecl `json:"functionDeclarations"`
}

type vertexFunctionDecl struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type vertexFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

type vertexGenConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type vertexResponse struct {
	Candidates    []vertexCandidate `json:"candidates"`
	UsageMetadata *vertexUsage      `json:"usageMetadata,omitempty"`
	Error         *vertexError      `json:"error,omitempty"`
}

type vertexCandidate struct {
	Content      vertexContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
	Index        int           `json:"index,omitempty"`
}

type vertexUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type vertexError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// vertexStreamResponse is identical to vertexResponse but kept separate for clarity.
type vertexStreamResponse struct {
	Candidates    []vertexCandidate `json:"candidates"`
	UsageMetadata *vertexUsage      `json:"usageMetadata,omitempty"`
	Error         *vertexError      `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// Endpoint helpers
// ---------------------------------------------------------------------------

// buildEndpoint constructs the Vertex AI endpoint URL.
// format: https://{region}-aiplatform.googleapis.com/v1/projects/{project}/locations/{region}/publishers/{publisher}/models/{model}:{action}
func (a *Adapter) buildEndpoint(model, action string) string {
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/%s/models/%s:%s",
		a.region, a.project, a.region, a.publisher, model, action,
	)
}

// resolveModel extracts the model name from target.Model which may be a full
// publisher path or just the model name.
func resolveModel(model string) string {
	// If the model contains slashes (e.g. "publishers/google/models/gemini-pro"),
	// extract just the model name.
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

// ---------------------------------------------------------------------------
// CompleteChat
// ---------------------------------------------------------------------------

func (a *Adapter) CompleteChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*openaischema.ChatCompletionResponse, error) {
	vertexModel := resolveModel(target.Model)
	vertexReq := toVertexRequest(req)

	body, err := json.Marshal(vertexReq)
	if err != nil {
		return nil, fmt.Errorf("marshal vertex request: %w", err)
	}

	endpoint := a.buildEndpoint(vertexModel, "generateContent")

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

	data, err := io.ReadAll(io.LimitReader(resp.Body, providers.MaxProviderResponseSize))
	if err != nil {
		return nil, fmt.Errorf("read vertex response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &providers.HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	var out vertexResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode vertex response after %s: %w", time.Since(start), err)
	}

	if out.Error != nil {
		return nil, fmt.Errorf("vertex error %d: %s", out.Error.Code, out.Error.Message)
	}

	return fromVertexResponse(out, req.Model, vertexModel), nil
}

// ---------------------------------------------------------------------------
// StreamChat
// ---------------------------------------------------------------------------

func (a *Adapter) StreamChat(ctx context.Context, req openaischema.ChatCompletionRequest, target providers.Target, key providers.ProviderKey) (*providers.StreamResult, error) {
	vertexModel := resolveModel(target.Model)
	vertexReq := toVertexRequest(req)

	body, err := json.Marshal(vertexReq)
	if err != nil {
		return nil, fmt.Errorf("marshal vertex request: %w", err)
	}

	endpoint := a.buildEndpoint(vertexModel, "streamGenerateContent") + "?alt=sse"

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

		messageID := fmt.Sprintf("vertex-%d", time.Now().UnixNano())
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

			var event vertexStreamResponse
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				errs <- fmt.Errorf("decode vertex stream event: %w", err)
				return
			}

			if event.Error != nil {
				errs <- fmt.Errorf("vertex stream error %d: %s", event.Error.Code, event.Error.Message)
				return
			}

			chunk, ok := vertexEventToOpenAIChunk(event, messageID, model, created)
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
			errs <- fmt.Errorf("read vertex stream: %w", err)
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
		req.Header.Set("Authorization", "Bearer "+key.Value)
	}
}

func toVertexRequest(req openaischema.ChatCompletionRequest) vertexRequest {
	var contents []vertexContent
	var systemInstruction *vertexContent

	for _, msg := range req.Messages {
		text := contentToString(msg.Content)

		if msg.Role == "system" {
			systemInstruction = &vertexContent{
				Parts: []vertexPart{{Text: text}},
			}
			continue
		}

		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		contents = append(contents, vertexContent{
			Role:  role,
			Parts: []vertexPart{{Text: text}},
		})
	}

	vr := vertexRequest{
		Contents: contents,
	}

	if systemInstruction != nil {
		vr.SystemInstruction = systemInstruction
	}

	// Map tools if present
	if len(req.Tools) > 0 {
		vr.Tools = mapTools(req.Tools)
	}

	// Generation config
	gc := &vertexGenConfig{}
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
		vr.GenerationConfig = gc
	}

	return vr
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

func mapTools(toolsRaw json.RawMessage) []vertexTool {
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

	var declarations []vertexFunctionDecl
	for _, t := range openaiTools {
		if t.Type != "function" {
			continue
		}
		declarations = append(declarations, vertexFunctionDecl{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		})
	}

	if len(declarations) == 0 {
		return nil
	}
	return []vertexTool{{FunctionDeclarations: declarations}}
}

func fromVertexResponse(resp vertexResponse, logicalModel string, vertexModel string) *openaischema.ChatCompletionResponse {
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
		model = vertexModel
	}

	usage := &openaischema.Usage{}
	if resp.UsageMetadata != nil {
		usage.PromptTokens = resp.UsageMetadata.PromptTokenCount
		usage.CompletionTokens = resp.UsageMetadata.CandidatesTokenCount
		usage.TotalTokens = resp.UsageMetadata.TotalTokenCount
	}

	return &openaischema.ChatCompletionResponse{
		ID:      fmt.Sprintf("vertex-%d", time.Now().UnixNano()),
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

func vertexEventToOpenAIChunk(event vertexStreamResponse, messageID string, model string, created int64) (openaischema.ChatCompletionStreamChunk, bool) {
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

// Compile-time verification that Adapter implements providers.Adapter.
var _ providers.Adapter = (*Adapter)(nil)
