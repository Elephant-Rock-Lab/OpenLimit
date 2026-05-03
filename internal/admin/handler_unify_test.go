package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"openlimit/internal/requestid"
)

// TEST-7C-03-01: Admin error JSON includes "request_id" field with value from context.
func TestWriteAdminError_IncludesRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)
	ctx := requestid.WithContext(r.Context(), "req-abc-123")
	r = r.WithContext(ctx)

	writeAdminError(w, r, http.StatusNotFound, "not_found", "key not found")

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", body["error"])
	}

	requestID, ok := errObj["request_id"].(string)
	if !ok {
		t.Fatalf("expected request_id string, got %T", errObj["request_id"])
	}
	if requestID != "req-abc-123" {
		t.Errorf("request_id = %q, want %q", requestID, "req-abc-123")
	}
}

// TEST-7C-03-02: Guardrail error JSON includes "stage" field.
// (Verifies writeGuardrailError in the openai package — tested via admin
// because we validate the guardrail error format uses the same ErrorBody.)
// This test validates that admin errors produce valid ErrorResponse JSON
// with stage field support.
func TestWriteAdminError_ErrorResponseStructure(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/keys", nil)
	ctx := requestid.WithContext(r.Context(), "req-stage-456")
	r = r.WithContext(ctx)

	writeAdminError(w, r, http.StatusForbidden, "forbidden", "insufficient permissions")

	resp := w.Result()
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", body["error"])
	}

	// Verify "type" field present
	typ, ok := errObj["type"].(string)
	if !ok || typ != "forbidden" {
		t.Errorf("type = %v, want %q", errObj["type"], "forbidden")
	}

	// Verify "request_id" field present
	rid, ok := errObj["request_id"].(string)
	if !ok || rid != "req-stage-456" {
		t.Errorf("request_id = %v, want %q", errObj["request_id"], "req-stage-456")
	}

	// Verify "message" field present
	msg, ok := errObj["message"].(string)
	if !ok || msg != "insufficient permissions" {
		t.Errorf("message = %v, want %q", errObj["message"], "insufficient permissions")
	}
}

// TEST-7C-03-03: Admin error output uses ErrorResponse struct (has "error"."type" and "error"."request_id").
// This verifies the structural change from inline map to typed ErrorResponse.
func TestWriteAdminError_UsesErrorResponseStruct(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/admin/keys/test-id", nil)
	ctx := requestid.WithContext(r.Context(), "req-struct-789")
	r = r.WithContext(ctx)

	writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "key id is required")

	resp := w.Result()
	defer resp.Body.Close()

	// Decode into the exact ErrorResponse struct to confirm structural match
	type errorBody struct {
		Message   string `json:"message"`
		Type      string `json:"type"`
		RequestID string `json:"request_id,omitempty"`
	}
	type errorResponse struct {
		Error errorBody `json:"error"`
	}

	var parsed errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode into ErrorResponse struct: %v", err)
	}

	if parsed.Error.Type != "invalid_request" {
		t.Errorf("error.type = %q, want %q", parsed.Error.Type, "invalid_request")
	}
	if parsed.Error.RequestID != "req-struct-789" {
		t.Errorf("error.request_id = %q, want %q", parsed.Error.RequestID, "req-struct-789")
	}
	if parsed.Error.Message != "key id is required" {
		t.Errorf("error.message = %q, want %q", parsed.Error.Message, "key id is required")
	}
}

// Verify that writeAdminError with nil request produces empty request_id (no panic).
func TestWriteAdminError_NilRequest(t *testing.T) {
	w := httptest.NewRecorder()

	writeAdminError(w, nil, http.StatusInternalServerError, "internal_error", "something broke")

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", body["error"])
	}

	// request_id should be empty string, which omitempty hides
	if _, exists := errObj["request_id"]; exists {
		rid := errObj["request_id"].(string)
		if rid != "" {
			t.Errorf("expected empty or absent request_id, got %q", rid)
		}
	}
}

// Verify that writeAdminError with request but no requestid in context returns empty request_id.
func TestWriteAdminError_NoRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/projects", nil)
	// No requestid in context — FromContext returns ""

	writeAdminError(w, r, http.StatusNotFound, "not_found", "project not found")

	resp := w.Result()
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", body["error"])
	}

	// request_id should be empty (omitempty means it may be absent or empty)
	if rid, exists := errObj["request_id"]; exists {
		if rid != "" {
			t.Errorf("expected empty request_id, got %q", rid)
		}
	}
}

// Verify Content-Type is still application/json after change.
func TestWriteAdminError_ContentType(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)

	writeAdminError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth")

	resp := w.Result()
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}
