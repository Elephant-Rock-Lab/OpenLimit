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
// TEST-48-01-01: Spend endpoint returns 200 with valid JSON structure
// ---------------------------------------------------------------------------

func TestSpend_Returns200WithValidStructure(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/spend", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify top-level structure
	keys, ok := resp["keys"].([]any)
	if !ok {
		t.Fatal("response missing 'keys' array")
	}

	if _, ok := resp["period"]; !ok {
		t.Fatal("response missing 'period' field")
	}

	if _, ok := resp["total_spend_usd"]; !ok {
		t.Fatal("response missing 'total_spend_usd' field")
	}

	if _, ok := resp["total_budget_usd"]; !ok {
		t.Fatal("response missing 'total_budget_usd' field")
	}

	// If keys exist, check their structure
	for i, k := range keys {
		entry, ok := k.(map[string]any)
		if !ok {
			t.Fatalf("keys[%d] is not an object", i)
		}
		for _, field := range []string{"key_id", "key_name", "key_prefix", "spend_usd", "budget_limit_usd", "utilization_pct", "status"} {
			if _, exists := entry[field]; !exists {
				t.Errorf("keys[%d] missing field %q", i, field)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TEST-48-01-02: Spend endpoint works with nil db (returns empty array)
// ---------------------------------------------------------------------------

func TestSpend_NilDB_ReturnsEmptyArray(t *testing.T) {
	h := NewHandler(nil, config.Default(), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/spend", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	keys, ok := resp["keys"].([]any)
	if !ok {
		t.Fatal("response missing 'keys' array")
	}
	if len(keys) != 0 {
		t.Fatalf("expected empty keys array with nil db, got %d keys", len(keys))
	}

	spend, _ := resp["total_spend_usd"].(float64)
	if spend != 0 {
		t.Errorf("expected total_spend_usd=0, got %v", resp["total_spend_usd"])
	}
}

// ---------------------------------------------------------------------------
// TEST-48-01-03: Spend endpoint requires auth (returns 401 without)
// ---------------------------------------------------------------------------

func TestSpend_NoAuth_Returns401(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /admin/usage/spend", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without auth")
	})

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, inner))

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/spend", nil)
	// No Authorization header
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// TEST-48-01-04: Period parameter accepted (daily vs monthly)
// ---------------------------------------------------------------------------

func TestSpend_PeriodParameter(t *testing.T) {
	// Test with nil DB — period should still be reflected in response
	h := NewHandler(nil, config.Default(), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	for _, period := range []string{"daily", "monthly"} {
		req := httptest.NewRequest(http.MethodGet, "/admin/usage/spend?period="+period, nil)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		authed.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("period=%s: expected 200, got %d; body: %s", period, w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("period=%s: failed to parse response: %v", period, err)
		}

		if resp["period"] != period {
			t.Errorf("period=%s: response period = %v, want %s", period, resp["period"], period)
		}
	}
}

// ---------------------------------------------------------------------------
// TEST-48-01-05: Response has total_spend_usd field
// ---------------------------------------------------------------------------

func TestSpend_ResponseHasTotalSpend(t *testing.T) {
	h := NewHandler(nil, config.Default(), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/spend", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	authed.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, ok := resp["total_spend_usd"]; !ok {
		t.Fatal("response missing total_spend_usd field")
	}

	if _, ok := resp["total_budget_usd"]; !ok {
		t.Fatal("response missing total_budget_usd field")
	}
}

// ---------------------------------------------------------------------------
// TEST-48-01-06: Status computed correctly (healthy/warning/critical)
// ---------------------------------------------------------------------------

func TestComputeKeyStatus_Thresholds(t *testing.T) {
	tests := []struct {
		name        string
		spend       float64
		budgetLimit float64
		want        string
	}{
		{"under 75%", 50, 100, "healthy"},
		{"exactly 74%", 74, 100, "healthy"},
		{"at 75%", 75, 100, "warning"},
		{"at 80%", 80, 100, "warning"},
		{"at 95%", 95, 100, "warning"},
		{"above 95%", 96, 100, "critical"},
		{"at 100%", 100, 100, "critical"},
		{"over budget", 150, 100, "critical"},
		{"zero spend", 0, 100, "healthy"},
		{"tiny spend", 0.01, 100, "healthy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeKeyStatus(tt.spend, tt.budgetLimit)
			if got != tt.want {
				t.Errorf("computeKeyStatus(%v, %v) = %q, want %q", tt.spend, tt.budgetLimit, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TEST-48-01-07: Zero budget → status "unlimited"
// ---------------------------------------------------------------------------

func TestComputeKeyStatus_ZeroBudget_Unlimited(t *testing.T) {
	tests := []struct {
		name        string
		spend       float64
		budgetLimit float64
	}{
		{"zero budget zero spend", 0, 0},
		{"zero budget with spend", 50, 0},
		{"negative budget", 10, -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeKeyStatus(tt.spend, tt.budgetLimit)
			if got != "unlimited" {
				t.Errorf("computeKeyStatus(%v, %v) = %q, want %q", tt.spend, tt.budgetLimit, got, "unlimited")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TEST-48-01-03b: Spend endpoint with auth but no body (GET request)
// ---------------------------------------------------------------------------

func TestSpend_WithAuthAndDB_Returns200(t *testing.T) {
	h, db := newTestHandler(t)
	defer db.Close()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("test-admin-token", nil, nil, nil, mux))

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/spend?period=monthly", bytes.NewReader(nil))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify period is reflected
	if resp["period"] != "monthly" {
		t.Errorf("period = %v, want monthly", resp["period"])
	}

	// Verify keys is an array (may be empty)
	keys, ok := resp["keys"].([]any)
	if !ok {
		t.Fatal("keys should be an array")
	}
	_ = keys
}
