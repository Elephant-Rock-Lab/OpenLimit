package guardrails

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

// JSONSchemaStage validates JSON output against a JSON Schema.
// Only applies when the response contains JSON content.
type JSONSchemaStage struct {
	schema    string // raw JSON schema string
	validator *gojsonschema.Schema
	strict    bool // if true, block on validation failure; if false, just log
}

// NewJSONSchemaStage creates a JSON schema validation stage.
func NewJSONSchemaStage(schema string, strict bool) (*JSONSchemaStage, error) {
	loader := gojsonschema.NewStringLoader(schema)
	validator, err := gojsonschema.NewSchema(loader)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON schema: %w", err)
	}
	return &JSONSchemaStage{
		schema:    schema,
		validator: validator,
		strict:    strict,
	}, nil
}

func (s *JSONSchemaStage) Name() string { return "json_schema" }

func (s *JSONSchemaStage) CheckInput(_ context.Context, messages []Message) (Result, error) {
	// Input validation not applicable for JSON schema stage
	return Result{Action: Pass}, nil
}

func (s *JSONSchemaStage) CheckOutput(_ context.Context, content string) (Result, error) {
	// Only validate if content looks like JSON
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return Result{Action: Pass}, nil
	}

	loader := gojsonschema.NewStringLoader(content)
	result, err := s.validator.Validate(loader)
	if err != nil {
		// Can't validate — pass through
		return Result{Action: Pass}, nil
	}

	if result.Valid() {
		return Result{Action: Pass}, nil
	}

	// Build error message
	var errs []string
	for _, desc := range result.Errors() {
		errs = append(errs, desc.String())
	}
	msg := fmt.Sprintf("JSON schema validation failed: %s", strings.Join(errs, "; "))

	if s.strict {
		return Result{Action: Block, Message: msg, StageName: s.Name()}, nil
	}
	// Non-strict: pass but the message is available for logging
	return Result{Action: Pass}, nil
}

// ResponseFormatStage checks if the request specified JSON response format.
// This is a helper used by the handler to decide whether to run JSON schema validation.
type ResponseFormatStage struct {
	schemas map[string]*gojsonschema.Schema // model -> schema
	strict  bool
}

// NewResponseFormatStage creates a stage that validates JSON response format.
func NewResponseFormatStage(schemas map[string]string, strict bool) (*ResponseFormatStage, error) {
	r := &ResponseFormatStage{schemas: make(map[string]*gojsonschema.Schema), strict: strict}
	for model, schema := range schemas {
		loader := gojsonschema.NewStringLoader(schema)
		validator, err := gojsonschema.NewSchema(loader)
		if err != nil {
			return nil, fmt.Errorf("invalid JSON schema for model %q: %w", model, err)
		}
		r.schemas[model] = validator
	}
	return r, nil
}

func (s *ResponseFormatStage) Name() string { return "response_format" }

func (s *ResponseFormatStage) CheckInput(_ context.Context, messages []Message) (Result, error) {
	return Result{Action: Pass}, nil
}

func (s *ResponseFormatStage) CheckOutput(_ context.Context, content string) (Result, error) {
	// This stage is a no-op on its own; the handler invokes ValidateJSON directly
	return Result{Action: Pass}, nil
}

// ValidateJSON validates content against a JSON schema.
// Returns true if valid, false otherwise, with error descriptions.
func ValidateJSON(schemaStr, content string) (bool, string) {
	loader := gojsonschema.NewStringLoader(schemaStr)
	schema, err := gojsonschema.NewSchema(loader)
	if err != nil {
		return false, fmt.Sprintf("invalid schema: %v", err)
	}

	doc := gojsonschema.NewStringLoader(content)
	result, err := schema.Validate(doc)
	if err != nil {
		return false, fmt.Sprintf("validation error: %v", err)
	}

	if result.Valid() {
		return true, ""
	}

	var errs []string
	for _, desc := range result.Errors() {
		errs = append(errs, desc.String())
	}
	return false, strings.Join(errs, "; ")
}

// ExtractJSONContent extracts JSON from a response message content field.
// The content may be a raw JSON string or wrapped in markdown code blocks.
func ExtractJSONContent(content string) string {
	trimmed := strings.TrimSpace(content)

	// Try to extract from markdown code block
	if strings.Contains(trimmed, "```json") {
		start := strings.Index(trimmed, "```json") + 7
		end := strings.Index(trimmed[start:], "```")
		if end > 0 {
			return strings.TrimSpace(trimmed[start : start+end])
		}
	}
	if strings.Contains(trimmed, "```") {
		start := strings.Index(trimmed, "```") + 3
		// Skip optional newline
		if start < len(trimmed) && trimmed[start] == '\n' {
			start++
		}
		end := strings.Index(trimmed[start:], "```")
		if end > 0 {
			return strings.TrimSpace(trimmed[start : start+end])
		}
	}

	// Try raw JSON parse
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &raw); err == nil {
		return trimmed
	}

	return ""
}
