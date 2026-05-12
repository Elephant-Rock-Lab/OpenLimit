package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"openlimit/internal/config"
)

// ---------------------------------------------------------------------------
// BATCH-49 TASK-01: Guardrail Catalog + Test Endpoint Tests
// All tests run WITHOUT a database (static catalog + in-process evaluation).
// ---------------------------------------------------------------------------

// newNoDBHandler creates a Handler with nil DB (suitable for catalog/test endpoints).
func newNoDBHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(nil, config.Default(), nil, nil)
}

// TEST-49-01-01: Catalog returns 6 validators
func TestCatalog_Returns6Validators(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/guardrails/catalog", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var entries []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(entries) != 6 {
		t.Errorf("expected 6 catalog entries, got %d", len(entries))
	}
}

// TEST-49-01-02: Each entry has required fields (name, description, config_fields)
func TestCatalog_EachEntryHasRequiredFields(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/guardrails/catalog", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var entries []map[string]any
	json.Unmarshal(w.Body.Bytes(), &entries)

	for i, entry := range entries {
		for _, field := range []string{"type", "name", "description", "config_fields"} {
			if _, ok := entry[field]; !ok {
				t.Errorf("entry[%d] missing field %q", i, field)
			}
		}
	}
}

// TEST-49-01-03: Each entry has example_config
func TestCatalog_EachEntryHasExampleConfig(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/guardrails/catalog", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var entries []map[string]any
	json.Unmarshal(w.Body.Bytes(), &entries)

	for i, entry := range entries {
		ec, ok := entry["example_config"]
		if !ok {
			t.Errorf("entry[%d] missing example_config", i)
			continue
		}
		if ec == nil {
			t.Errorf("entry[%d] has nil example_config", i)
		}
	}
}

// TEST-49-01-04: Test PII type — redacts email
func TestGuardrailTest_PIIRedactsEmail(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"type":"pii","config":{"types":["email"],"action":"redact"},"text":"Contact me at john@example.com please","direction":"input"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/guardrails/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["action"] != "redact" {
		t.Errorf("action = %v, want redact", resp["action"])
	}
	redacted, _ := resp["redacted_text"].(string)
	if redacted == "" {
		t.Error("redacted_text is empty")
	}
	if !contains(redacted, "[REDACTED_EMAIL]") {
		t.Errorf("redacted_text = %q, expected [REDACTED_EMAIL]", redacted)
	}
	if contains(redacted, "john@example.com") {
		t.Error("redacted_text still contains original email")
	}
}

// TEST-49-01-05: Test keyword type — blocks word
func TestGuardrailTest_KeywordBlocksWord(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"type":"keyword","config":{"blocklist":["forbidden","banned"]},"text":"This is a forbidden word","direction":"input"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/guardrails/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["action"] != "block" {
		t.Errorf("action = %v, want block", resp["action"])
	}
}

// TEST-49-01-06: Test length type — blocks long text
func TestGuardrailTest_LengthBlocksLongText(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Generate text longer than 10 chars
	longText := "This text is definitely longer than ten characters"
	body := `{"type":"length","config":{"max_chars":10},"text":"` + longText + `","direction":"input"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/guardrails/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["action"] != "block" {
		t.Errorf("action = %v, want block", resp["action"])
	}
}

// TEST-49-01-07: Test regex type — redacts pattern
func TestGuardrailTest_RegexRedactsPattern(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"type":"regex","config":{"patterns":[{"name":"api_key","pattern":"sk-[a-zA-Z0-9]{10,}","action":"redact","replacement":"[REDACTED_KEY]"}]},"text":"My key is sk-abcdefghij123456","direction":"input"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/guardrails/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["action"] != "redact" {
		t.Errorf("action = %v, want redact", resp["action"])
	}
	redacted, _ := resp["redacted_text"].(string)
	if !contains(redacted, "[REDACTED_KEY]") {
		t.Errorf("redacted_text = %q, expected [REDACTED_KEY]", redacted)
	}
}

// TEST-49-01-08: Invalid type returns 400
func TestGuardrailTest_InvalidTypeReturns400(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"type":"nonexistent","config":{},"text":"hello","direction":"input"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/guardrails/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var errResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &errResp)
	errObj, _ := errResp["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if !contains(msg, "valid types") {
		t.Errorf("error message = %q, expected to contain 'valid types'", msg)
	}
}

// TEST-49-01-09: Clean text returns pass
func TestGuardrailTest_CleanTextReturnsPass(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"type":"pii","config":{"types":["email"],"action":"redact"},"text":"This is clean text with no PII","direction":"input"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/guardrails/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["action"] != "pass" {
		t.Errorf("action = %v, want pass", resp["action"])
	}
}

// TEST-49-01-10: Direction parameter works (input/output)
func TestGuardrailTest_DirectionParameterWorks(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Test "output" direction with keyword blocklist
	body := `{"type":"keyword","config":{"blocklist":["bad"]},"text":"This is bad output","direction":"output"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/guardrails/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["action"] != "block" {
		t.Errorf("action = %v, want block (output direction)", resp["action"])
	}
	if resp["stage_name"] != "keyword" {
		t.Errorf("stage_name = %v, want keyword", resp["stage_name"])
	}
}

// TEST-49-01-11: Test json_schema type — validates JSON
func TestGuardrailTest_JSONSchemaValidates(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	schema := `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`
	invalidJSON := `{"age": 30}`
	body := `{"type":"json_schema","config":{"schema":` + jsonEscape(schema) + `,"strict":true},"text":` + jsonEscape(invalidJSON) + `,"direction":"output"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/guardrails/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Should block because "name" is required but missing
	if resp["action"] != "block" {
		t.Errorf("action = %v, want block (missing required field)", resp["action"])
	}
}

// TEST-49-01-12: Webhook type rejected with 400
func TestGuardrailTest_WebhookRejectedWith400(t *testing.T) {
	h := newNoDBHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"type":"webhook","config":{"url":"https://example.com"},"text":"hello","direction":"input"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/guardrails/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var errResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &errResp)
	errObj, _ := errResp["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if !contains(msg, "webhook requires live endpoint") {
		t.Errorf("error message = %q, expected to mention webhook requires live endpoint", msg)
	}
}

// --- Test helpers ---

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// jsonEscape produces a JSON-escaped string literal (with surrounding quotes).
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
