package sbc

import "encoding/json"

// SchemaTier represents how much of a tool's schema to expose.
type SchemaTier string

const (
	// TierFull passes through the tool definition unchanged.
	TierFull SchemaTier = "FULL"
	// TierCompact strips descriptions from parameters and removes examples.
	TierCompact SchemaTier = "COMPACT"
	// TierHidden removes the tool entirely from the request.
	TierHidden SchemaTier = "HIDDEN"
)

// CompileToolSchema applies a schema tier to a tool's JSON definition.
// It returns the modified JSON or the original on any error.
//
// FULL: passthrough
// COMPACT: strip parameter descriptions, remove examples
// HIDDEN: return nil (tool is removed)
func CompileToolSchema(tool json.RawMessage, tier SchemaTier) json.RawMessage {
	switch tier {
	case TierFull:
		return tool
	case TierHidden:
		return nil
	case TierCompact:
		return compactTool(tool)
	default:
		return tool
	}
}

// compactTool strips verbose fields from a tool definition to save tokens.
// It removes:
//   - "description" from individual parameter properties
//   - "examples" field
//   - "default" values
//
// It preserves: tool name, tool description (needed for model selection),
// required fields, parameter names and types.
func compactTool(raw json.RawMessage) json.RawMessage {
	var tool map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tool); err != nil {
		return raw // passthrough on error
	}

	// Get the function block
	fnRaw, ok := tool["function"]
	if !ok {
		return raw
	}

	var fn map[string]json.RawMessage
	if err := json.Unmarshal(fnRaw, &fn); err != nil {
		return raw
	}

	// Strip examples
	delete(fn, "examples")

	// Strip descriptions from parameter properties
	if paramsRaw, ok := fn["parameters"]; ok {
		var params map[string]json.RawMessage
		if err := json.Unmarshal(paramsRaw, &params); err == nil {
			if propsRaw, ok := params["properties"]; ok {
				compacted := stripPropertyDescriptions(propsRaw)
				if compacted != nil {
					params["properties"] = compacted
					if updated, err := json.Marshal(params); err == nil {
						fn["parameters"] = updated
					}
				}
			}
		}
	}

	// Re-serialize
	fnUpdated, err := json.Marshal(fn)
	if err != nil {
		return raw
	}
	tool["function"] = fnUpdated

	result, err := json.Marshal(tool)
	if err != nil {
		return raw
	}
	return result
}

// stripPropertyDescriptions removes "description" and "default" from each
// property in a JSON schema properties block.
func stripPropertyDescriptions(propsRaw json.RawMessage) json.RawMessage {
	var props map[string]json.RawMessage
	if err := json.Unmarshal(propsRaw, &props); err != nil {
		return nil
	}

	modified := false
	for name, propRaw := range props {
		var prop map[string]json.RawMessage
		if err := json.Unmarshal(propRaw, &prop); err != nil {
			continue
		}
		if _, has := prop["description"]; has {
			delete(prop, "description")
			modified = true
		}
		if _, has := prop["default"]; has {
			delete(prop, "default")
			modified = true
		}
		if modified {
			updated, err := json.Marshal(prop)
			if err == nil {
				props[name] = updated
			}
		}
	}

	if !modified {
		return nil
	}

	result, err := json.Marshal(props)
	if err != nil {
		return nil
	}
	return result
}

// TierForPressure selects a schema tier based on pressure level.
// Currently always returns TierFull (schema compilation is a future extension point).
// The SBC implementation focuses on tool pruning rather than schema compression.
func TierForPressure(pressure PressureLevel) SchemaTier {
	switch pressure {
	case PressureHealthy:
		return TierFull
	case PressureHigh:
		return TierFull // keep full schemas, just prune tools
	case PressureCritical:
		return TierCompact
	case PressureEmergency:
		return TierCompact
	default:
		return TierFull
	}
}
