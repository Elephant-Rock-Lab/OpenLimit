package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"openlimit/internal/replay"
)

// ---------------------------------------------------------------------------
// TEST-55-02-01: Returns replay results
// ---------------------------------------------------------------------------
func TestReplayHandler_ReturnsResults(t *testing.T) {
	handler := ReplayHandler(func() replay.ReplaySummary {
		return replay.ReplaySummary{
			Results: []replay.ReplayResult{
				{
					ID:               "rp_abc123",
					Model:            "gpt-4o",
					PrimaryProvider:  "openai",
					ShadowProvider:   "deepseek",
					ShadowModel:      "deepseek-chat",
					PrimaryLatencyMS: 450,
					ShadowLatencyMS:  320,
					ShadowStatus:     200,
					ShadowTokensIn:   150,
					ShadowTokensOut:  80,
				},
			},
			Total: 1,
		}
	})

	req := httptest.NewRequest("GET", "/admin/routing/replay", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatal("results field missing or not array")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	first := results[0].(map[string]any)
	if first["primary_provider"] != "openai" {
		t.Errorf("expected primary_provider=openai, got %v", first["primary_provider"])
	}
	if first["shadow_provider"] != "deepseek" {
		t.Errorf("expected shadow_provider=deepseek, got %v", first["shadow_provider"])
	}
}

// ---------------------------------------------------------------------------
// TEST-55-02-02: Returns summary statistics
// ---------------------------------------------------------------------------
func TestReplayHandler_ReturnsStatistics(t *testing.T) {
	handler := ReplayHandler(func() replay.ReplaySummary {
		return replay.ReplaySummary{
			Results:         []replay.ReplayResult{},
			Total:           0,
			AvgPrimaryMS:    450.5,
			AvgShadowMS:     320.3,
			ShadowErrorRate: 0.02,
		}
	})

	req := httptest.NewRequest("GET", "/admin/routing/replay", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	avgPrimary, _ := resp["avg_primary_ms"].(float64)
	if avgPrimary != 450.5 {
		t.Errorf("expected avg_primary_ms=450.5, got %v", avgPrimary)
	}
	avgShadow, _ := resp["avg_shadow_ms"].(float64)
	if avgShadow != 320.3 {
		t.Errorf("expected avg_shadow_ms=320.3, got %v", avgShadow)
	}
	errorRate, _ := resp["shadow_error_rate"].(float64)
	if errorRate != 0.02 {
		t.Errorf("expected shadow_error_rate=0.02, got %v", errorRate)
	}
}

// ---------------------------------------------------------------------------
// TEST-55-02-03: Empty when no replays
// ---------------------------------------------------------------------------
func TestReplayHandler_EmptyResults(t *testing.T) {
	handler := ReplayHandler(func() replay.ReplaySummary {
		return replay.ReplaySummary{
			Results: []replay.ReplayResult{},
		}
	})

	req := httptest.NewRequest("GET", "/admin/routing/replay", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	results, ok := resp["results"].([]any)
	if !ok || results == nil {
		t.Fatal("results field missing or nil")
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// TEST-55-02-04: Method not allowed for POST
// ---------------------------------------------------------------------------
func TestReplayHandler_MethodNotAllowed(t *testing.T) {
	handler := ReplayHandler(func() replay.ReplaySummary {
		return replay.ReplaySummary{}
	})

	req := httptest.NewRequest("POST", "/admin/routing/replay", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
