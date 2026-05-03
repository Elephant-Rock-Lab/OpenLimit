package guardrails

import (
	"fmt"
	"strings"

	"openlimit/internal/config"
)

// BuildPipeline constructs a guardrail pipeline from config.
// Returns a Pipeline and any configuration errors.
func BuildPipeline(cfg config.GuardrailsConfig) (*Pipeline, error) {
	if !cfg.Enabled {
		return NewPipeline(nil, nil), nil
	}

	inputStages, err := buildStages(cfg.Input, "input")
	if err != nil {
		return nil, fmt.Errorf("input guardrails: %w", err)
	}

	outputStages, err := buildStages(cfg.Output, "output")
	if err != nil {
		return nil, fmt.Errorf("output guardrails: %w", err)
	}

	return NewPipeline(inputStages, outputStages), nil
}

func buildStages(stageConfigs []config.GuardrailStageConfig, direction string) ([]Stage, error) {
	var stages []Stage
	for i, sc := range stageConfigs {
		stage, err := buildStage(sc, direction)
		if err != nil {
			return nil, fmt.Errorf("stage %d (%s): %w", i, sc.Type, err)
		}
		if stage != nil {
			stages = append(stages, stage)
		}
	}
	return stages, nil
}

func buildStage(sc config.GuardrailStageConfig, direction string) (Stage, error) {
	switch strings.ToLower(sc.Type) {
	case "pii":
		return buildPIIStage(sc.Config)
	case "regex":
		return buildRegexStage(sc.Config)
	case "keyword":
		return buildKeywordStage(sc.Config)
	case "length":
		return buildLengthStage(sc.Config, direction)
	case "webhook":
		return buildWebhookStage(sc.Config)
	case "json_schema":
		return buildJSONSchemaStage(sc.Config)
	default:
		return nil, fmt.Errorf("unknown guardrail type %q", sc.Type)
	}
}

func buildPIIStage(cfg map[string]any) (Stage, error) {
	var types []string
	if v, ok := cfg["types"]; ok {
		if arr, ok := v.([]any); ok {
			for _, a := range arr {
				if s, ok := a.(string); ok {
					types = append(types, s)
				}
			}
		}
	}
	action := getStringDefault(cfg, "action", "redact")
	return NewPIIStage(types, action)
}

func buildRegexStage(cfg map[string]any) (Stage, error) {
	var rules []RegexRule
	if v, ok := cfg["patterns"]; ok {
		if arr, ok := v.([]any); ok {
			for _, a := range arr {
				if m, ok := a.(map[string]any); ok {
					rules = append(rules, RegexRule{
						Name:        getString(m, "name"),
						Pattern:     getString(m, "pattern"),
						Action:      getStringDefault(m, "action", "block"),
						Replacement: getStringDefault(m, "replacement", "[REDACTED]"),
					})
				}
			}
		}
	}
	blockMsg := getStringDefault(cfg, "block_message", "")
	return NewRegexStage(rules, blockMsg)
}

func buildKeywordStage(cfg map[string]any) (Stage, error) {
	var blocklist []string
	if v, ok := cfg["blocklist"]; ok {
		if arr, ok := v.([]any); ok {
			for _, a := range arr {
				if s, ok := a.(string); ok {
					blocklist = append(blocklist, s)
				}
			}
		}
	}
	action := getStringDefault(cfg, "action", "block")
	blockMsg := getStringDefault(cfg, "block_message", "")
	return NewKeywordStage(blocklist, action, blockMsg), nil
}

func buildLengthStage(cfg map[string]any, direction string) (Stage, error) {
	minChars := getIntDefault(cfg, "min_chars", 0)
	maxChars := getIntDefault(cfg, "max_chars", 0)
	return NewLengthStage(minChars, maxChars, direction), nil
}

func buildWebhookStage(cfg map[string]any) (Stage, error) {
	url := getString(cfg, "url")
	if url == "" {
		return nil, fmt.Errorf("webhook requires url")
	}
	timeoutMS := getIntDefault(cfg, "timeout_ms", 250)
	blockOnError := getBoolDefault(cfg, "block_on_error", true)
	blockOnTimeout := getBoolDefault(cfg, "block_on_timeout", true)
	return NewWebhookStage(url, timeoutMS, blockOnError, blockOnTimeout), nil
}

func buildJSONSchemaStage(cfg map[string]any) (Stage, error) {
	schema := getString(cfg, "schema")
	if schema == "" {
		return nil, fmt.Errorf("json_schema requires schema")
	}
	strict := getBoolDefault(cfg, "strict", true)
	return NewJSONSchemaStage(schema, strict)
}

// Helper functions for extracting values from map[string]any config.

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getStringDefault(m map[string]any, key, def string) string {
	s := getString(m, key)
	if s == "" {
		return def
	}
	return s
}

func getIntDefault(m map[string]any, key string, def int) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		}
	}
	return def
}

func getBoolDefault(m map[string]any, key string, def bool) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}
