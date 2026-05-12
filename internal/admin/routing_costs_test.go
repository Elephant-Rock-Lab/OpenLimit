package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// TEST-54-02-01: Returns cost catalog entries
// ---------------------------------------------------------------------------
func TestRoutingCosts_ReturnsCatalog(t *testing.T) {
	handler := RoutingCostsHandler(func() RoutingCostsResponse {
		return RoutingCostsResponse{
			Models: []CostEntryJSON{
				{Provider: "deepseek", Model: "deepseek-chat", InputPer1M: 0.27, OutputPer1M: 1.10},
				{Provider: "openai", Model: "gpt-4o", InputPer1M: 2.50, OutputPer1M: 10.00},
				{Provider: "anthropic", Model: "claude-sonnet-4-20250514", InputPer1M: 3.00, OutputPer1M: 15.00},
			},
			Strategy: "smart",
			Weights:  CostWeightsJSON{Cost: 0.4, Latency: 0.4, Health: 0.2},
		}
	})

	req := httptest.NewRequest("GET", "/admin/routing/costs", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	models, ok := resp["models"].([]any)
	if !ok {
		t.Fatal("models field missing or not array")
	}
	if len(models) != 3 {
		t.Errorf("expected 3 models, got %d", len(models))
	}

	// Verify first entry
	first := models[0].(map[string]any)
	if first["provider"] != "deepseek" {
		t.Errorf("expected provider deepseek, got %v", first["provider"])
	}
}

// ---------------------------------------------------------------------------
// TEST-54-02-02: Returns current strategy
// ---------------------------------------------------------------------------
func TestRoutingCosts_ReturnsStrategy(t *testing.T) {
	handler := RoutingCostsHandler(func() RoutingCostsResponse {
		return RoutingCostsResponse{
			Models:   []CostEntryJSON{},
			Strategy: "cost",
			Weights:  CostWeightsJSON{Cost: 0.5, Latency: 0.3, Health: 0.2},
		}
	})

	req := httptest.NewRequest("GET", "/admin/routing/costs", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	strategy, _ := resp["strategy"].(string)
	if strategy != "cost" {
		t.Errorf("expected strategy cost, got %v", strategy)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-02-03: Returns smart weights
// ---------------------------------------------------------------------------
func TestRoutingCosts_ReturnsWeights(t *testing.T) {
	handler := RoutingCostsHandler(func() RoutingCostsResponse {
		return RoutingCostsResponse{
			Models:   []CostEntryJSON{},
			Strategy: "smart",
			Weights:  CostWeightsJSON{Cost: 0.4, Latency: 0.4, Health: 0.2},
		}
	})

	req := httptest.NewRequest("GET", "/admin/routing/costs", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	weights, ok := resp["weights"].(map[string]any)
	if !ok {
		t.Fatal("weights field missing or not object")
	}
	costW, _ := weights["cost"].(float64)
	latW, _ := weights["latency"].(float64)
	healthW, _ := weights["health"].(float64)

	if costW != 0.4 {
		t.Errorf("expected cost weight 0.4, got %v", costW)
	}
	if latW != 0.4 {
		t.Errorf("expected latency weight 0.4, got %v", latW)
	}
	if healthW != 0.2 {
		t.Errorf("expected health weight 0.2, got %v", healthW)
	}
}

// ---------------------------------------------------------------------------
// TEST-54-02-04: Method not allowed
// ---------------------------------------------------------------------------
func TestRoutingCosts_MethodNotAllowed(t *testing.T) {
	handler := RoutingCostsHandler(func() RoutingCostsResponse {
		return RoutingCostsResponse{}
	})

	req := httptest.NewRequest("POST", "/admin/routing/costs", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
