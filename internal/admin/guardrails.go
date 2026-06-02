package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"openlimit/internal/guardrails"
	"openlimit/internal/metrics"
)

// catalogEntry describes a single validator in the native guardrail catalog.
type catalogEntry struct {
	Type          string               `json:"type"`
	Name          string               `json:"name"`
	Description   string               `json:"description"`
	ConfigFields  []catalogConfigField `json:"config_fields"`
	ExampleConfig any                  `json:"example_config"`
	Testable      bool                 `json:"testable"`
}

type catalogConfigField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     string `json:"default"`
}

// guardrailCatalog is the static catalog of all 6 built-in validators.
var guardrailCatalog = []catalogEntry{
	{
		Type:        "pii",
		Name:        "PII Redaction",
		Description: "Detects and redacts personally identifiable information such as credit card numbers, SSNs, emails, phone numbers, and IP addresses.",
		ConfigFields: []catalogConfigField{
			{Name: "types", Type: "array", Description: "PII types to check (credit_card, ssn, email, phone, ip_address)", Default: "all"},
			{Name: "action", Type: "string", Description: "Action on detection: block or redact", Default: "redact"},
		},
		ExampleConfig: map[string]any{
			"types":  []string{"credit_card", "email"},
			"action": "redact",
		},
		Testable: true,
	},
	{
		Type:        "keyword",
		Name:        "Keyword Blocklist",
		Description: "Blocks or flags requests containing words from a configurable blocklist. Uses case-insensitive substring matching.",
		ConfigFields: []catalogConfigField{
			{Name: "blocklist", Type: "array", Description: "List of blocked keywords", Default: "[]"},
			{Name: "action", Type: "string", Description: "Action on match: block or log", Default: "block"},
			{Name: "block_message", Type: "string", Description: "Custom block message", Default: "request contains blocked content"},
		},
		ExampleConfig: map[string]any{
			"blocklist":     []string{"forbidden", "blocked"},
			"action":        "block",
			"block_message": "request contains blocked content",
		},
		Testable: true,
	},
	{
		Type:        "length",
		Name:        "Length Limit",
		Description: "Enforces minimum and maximum character length limits on input or output text.",
		ConfigFields: []catalogConfigField{
			{Name: "min_chars", Type: "number", Description: "Minimum character count (0 = no minimum)", Default: "0"},
			{Name: "max_chars", Type: "number", Description: "Maximum character count (0 = no maximum)", Default: "0"},
		},
		ExampleConfig: map[string]any{
			"min_chars": 0,
			"max_chars": 10000,
		},
		Testable: true,
	},
	{
		Type:        "regex",
		Name:        "Regex Pattern",
		Description: "Matches content against configurable regex patterns with block or redact actions.",
		ConfigFields: []catalogConfigField{
			{Name: "patterns", Type: "array", Description: "Array of {name, pattern, action, replacement} objects", Default: "[]"},
			{Name: "block_message", Type: "string", Description: "Custom block message", Default: "request contains prohibited content"},
		},
		ExampleConfig: map[string]any{
			"patterns": []map[string]string{
				{"name": "api_key", "pattern": "sk-[a-zA-Z0-9]{20,}", "action": "redact", "replacement": "[REDACTED_KEY]"},
			},
			"block_message": "request contains prohibited content",
		},
	},
	{
		Type:        "json_schema",
		Name:        "JSON Schema Validation",
		Description: "Validates JSON output against a JSON Schema definition. Blocks or passes based on strictness setting.",
		ConfigFields: []catalogConfigField{
			{Name: "schema", Type: "string", Description: "JSON Schema definition (stringified JSON)", Default: ""},
			{Name: "strict", Type: "boolean", Description: "If true, block on validation failure; if false, pass with warning", Default: "true"},
		},
		ExampleConfig: map[string]any{
			"schema": `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`,
			"strict": true,
		},
		Testable: true,
	},
	{
		Type:        "webhook",
		Name:        "Webhook Guardrail",
		Description: "Forwards content to an external HTTP endpoint for custom validation. Requires a live endpoint — not testable in the dashboard.",
		ConfigFields: []catalogConfigField{
			{Name: "url", Type: "string", Description: "HTTP(S) endpoint URL for validation", Default: ""},
			{Name: "timeout_ms", Type: "number", Description: "Request timeout in milliseconds", Default: "250"},
			{Name: "block_on_error", Type: "boolean", Description: "Block if the endpoint returns an error", Default: "true"},
			{Name: "block_on_timeout", Type: "boolean", Description: "Block if the endpoint times out", Default: "true"},
		},
		ExampleConfig: map[string]any{
			"url":              "https://example.com/guardrail",
			"timeout_ms":       250,
			"block_on_error":   true,
			"block_on_timeout": true,
		},
		Testable: false,
	},
}

// getGuardrailStats returns a snapshot of guardrail counters.
// GET /admin/guardrails/stats
func (h *Handler) getGuardrailStats(w http.ResponseWriter, r *http.Request) {
	if h.requireRole(w, r, "audit:read") == nil {
		return
	}
	if h.metrics == nil {
		writeAdminJSON(w, http.StatusOK, metrics.GuardrailStatsSnapshot{})
		return
	}
	stats := h.metrics.GetGuardrailStats()
	writeAdminJSON(w, http.StatusOK, stats)
}

// listGuardrailCatalog returns the static catalog of all built-in guardrail validators.
// GET /admin/guardrails/catalog
func (h *Handler) listGuardrailCatalog(w http.ResponseWriter, r *http.Request) {
	if h.requireRole(w, r, "audit:read") == nil {
		return
	}
	writeAdminJSON(w, http.StatusOK, guardrailCatalog)
}

// testGuardrail evaluates sample text against a chosen guardrail stage.
// POST /admin/guardrails/test
func (h *Handler) testGuardrail(w http.ResponseWriter, r *http.Request) {
	if h.requireRole(w, r, "key:create") == nil {
		return
	}

	var req struct {
		Type      string         `json:"type"`
		Config    map[string]any `json:"config"`
		Text      string         `json:"text"`
		Direction string         `json:"direction"`
	}
	if err := readJSON(r, &req); err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.Type == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "type is required")
		return
	}
	if req.Text == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "text is required")
		return
	}

	if req.Config == nil {
		req.Config = map[string]any{}
	}

	reqType := strings.ToLower(req.Type)
	if req.Direction == "" {
		req.Direction = "input"
	}

	// AR-04: Reject webhook type — it requires a live endpoint
	if reqType == "webhook" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "webhook requires live endpoint and cannot be tested in-process")
		return
	}

	// Construct the appropriate stage
	var stage guardrails.Stage
	var err error

	switch reqType {
	case "pii":
		types := cfgStringSlice(req.Config["types"])
		action := cfgStringDefault(req.Config, "action", "redact")
		stage, err = guardrails.NewPIIStage(types, action)
	case "keyword":
		blocklist := cfgStringSlice(req.Config["blocklist"])
		action := cfgStringDefault(req.Config, "action", "block")
		blockMsg := cfgStringDefault(req.Config, "block_message", "request contains blocked content")
		stage = guardrails.NewKeywordStage(blocklist, action, blockMsg)
	case "length":
		minChars := cfgIntDefault(req.Config, "min_chars", 0)
		maxChars := cfgIntDefault(req.Config, "max_chars", 0)
		stage = guardrails.NewLengthStage(minChars, maxChars, req.Direction)
	case "regex":
		rules := cfgRegexRules(req.Config["patterns"])
		blockMsg := cfgStringDefault(req.Config, "block_message", "request contains prohibited content")
		stage, err = guardrails.NewRegexStage(rules, blockMsg)
	case "json_schema":
		schema := cfgStringDefault(req.Config, "schema", "")
		if schema == "" {
			writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "json_schema requires schema field")
			return
		}
		strict := cfgBoolDefault(req.Config, "strict", true)
		stage, err = guardrails.NewJSONSchemaStage(schema, strict)
	default:
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request",
			"unknown guardrail type "+req.Type+"; valid types: pii, keyword, length, regex, json_schema, webhook")
		return
	}

	if err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}

	// Evaluate the stage
	ctx := context.Background()
	var result guardrails.Result

	if req.Direction == "output" {
		result, err = stage.CheckOutput(ctx, req.Text)
	} else {
		result, err = stage.CheckInput(ctx, []guardrails.Message{{Role: "user", Content: req.Text}})
	}

	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Build response
	actionStr := "pass"
	switch result.Action {
	case guardrails.Block:
		actionStr = "block"
	case guardrails.Redact:
		actionStr = "redact"
	}

	// Determine redacted text
	redactedText := req.Text
	if result.Action == guardrails.Redact {
		if result.Message != "" {
			redactedText = result.Message
		} else if len(result.RedactedMessages) > 0 {
			// Concatenate redacted messages
			var parts []string
			for _, m := range result.RedactedMessages {
				parts = append(parts, m.Content)
			}
			redactedText = strings.Join(parts, " ")
		}
	}

	resp := map[string]any{
		"action":        actionStr,
		"message":       result.Message,
		"stage_name":    result.StageName,
		"redacted_text": redactedText,
	}
	if resp["stage_name"] == "" {
		resp["stage_name"] = stage.Name()
	}

	writeAdminJSON(w, http.StatusOK, resp)
}

// --- Helper functions for config map extraction ---

func cfgStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, a := range arr {
		if s, ok := a.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func cfgStringDefault(m map[string]any, key, def string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return def
}

func cfgIntDefault(m map[string]any, key string, def int) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return def
}

func cfgBoolDefault(m map[string]any, key string, def bool) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

func cfgRegexRules(v any) []guardrails.RegexRule {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	rules := make([]guardrails.RegexRule, 0, len(arr))
	for _, a := range arr {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		rules = append(rules, guardrails.RegexRule{
			Name:        cfgStrVal(m["name"]),
			Pattern:     cfgStrVal(m["pattern"]),
			Action:      cfgStringDefault(m, "action", "block"),
			Replacement: cfgStringDefault(m, "replacement", "[REDACTED]"),
		})
	}
	return rules
}

func cfgStrVal(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
