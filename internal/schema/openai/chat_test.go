package openai

import (
	"encoding/json"
	"testing"
)

// TEST-7C-01-01: ErrorBody with Details marshals correctly; without Details, key absent.
func TestErrorBody_Details_Omitempty(t *testing.T) {
	// With Details — "details" key must be present
	body := ErrorBody{
		Message: "test error",
		Type:    "test_type",
		Details: map[string]any{"retry_after": 30, "reason": "quota"},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal with Details: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	details, ok := parsed["details"]
	if !ok {
		t.Fatal("expected \"details\" key in JSON output, not found")
	}
	detailsMap, ok := details.(map[string]any)
	if !ok {
		t.Fatalf("expected details to be map, got %T", details)
	}
	if retryAfter, _ := detailsMap["retry_after"].(float64); retryAfter != 30 {
		t.Errorf("details.retry_after = %v, want 30", detailsMap["retry_after"])
	}
	if reason, _ := detailsMap["reason"].(string); reason != "quota" {
		t.Errorf("details.reason = %v, want \"quota\"", detailsMap["reason"])
	}

	// Without Details — "details" key must be absent (omitempty)
	bodyNoDetails := ErrorBody{
		Message: "test error",
		Type:    "test_type",
	}
	data2, err := json.Marshal(bodyNoDetails)
	if err != nil {
		t.Fatalf("marshal without Details: %v", err)
	}
	var parsed2 map[string]any
	if err := json.Unmarshal(data2, &parsed2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := parsed2["details"]; exists {
		t.Error("expected \"details\" key to be absent when nil, but it was present")
	}
}

// TEST-7C-01-02: ErrorBody with Stage marshals correctly; without Stage, key absent.
func TestErrorBody_Stage_Omitempty(t *testing.T) {
	// With Stage — "stage" key must be present
	body := ErrorBody{
		Message: "blocked by PII filter",
		Type:    "guardrail_block",
		Stage:   "input",
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal with Stage: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	stage, ok := parsed["stage"]
	if !ok {
		t.Fatal("expected \"stage\" key in JSON output, not found")
	}
	if stageStr, _ := stage.(string); stageStr != "input" {
		t.Errorf("stage = %v, want \"input\"", stage)
	}

	// Without Stage — "stage" key must be absent (omitempty)
	bodyNoStage := ErrorBody{
		Message: "test error",
		Type:    "test_type",
	}
	data2, err := json.Marshal(bodyNoStage)
	if err != nil {
		t.Fatalf("marshal without Stage: %v", err)
	}
	var parsed2 map[string]any
	if err := json.Unmarshal(data2, &parsed2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := parsed2["stage"]; exists {
		t.Error("expected \"stage\" key to be absent when empty, but it was present")
	}
}

// Verify existing ErrorBody without new fields produces identical JSON to before.
func TestErrorBody_BackwardsCompatible(t *testing.T) {
	body := ErrorBody{
		Message:   "model not found",
		Type:      "invalid_request",
		Code:      "model_not_found",
		RequestID: "req-123",
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify no new empty keys appear
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := parsed["details"]; ok {
		t.Error("\"details\" key should not appear in JSON for nil Details")
	}
	if _, ok := parsed["stage"]; ok {
		t.Error("\"stage\" key should not appear in JSON for empty Stage")
	}

	// Verify expected keys are present
	expectedKeys := []string{"message", "type", "code", "request_id"}
	for _, key := range expectedKeys {
		if _, ok := parsed[key]; !ok {
			t.Errorf("expected key %q in JSON, not found", key)
		}
	}
}
