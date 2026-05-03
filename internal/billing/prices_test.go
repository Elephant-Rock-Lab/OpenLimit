package billing

import (
	"math"
	"testing"

	"openlimit/internal/config"
)

func TestCalculateCost(t *testing.T) {
	table := NewPriceTable([]config.PriceEntry{
		{Provider: "openai", Model: "gpt-4o-mini", PromptPer1M: 0.15, CompletionPer1M: 0.60},
		{Provider: "openai", Model: "gpt-4o", PromptPer1M: 2.50, CompletionPer1M: 10.00},
	})

	tests := []struct {
		name             string
		provider         string
		model            string
		promptTokens     int
		completionTokens int
		wantCost         float64
	}{
		{
			name:             "gpt-4o-mini basic",
			provider:         "openai",
			model:            "gpt-4o-mini",
			promptTokens:     1000,
			completionTokens: 1000,
			wantCost:         0.00015 + 0.0006, // 0.00075
		},
		{
			name:             "gpt-4o basic",
			provider:         "openai",
			model:            "gpt-4o",
			promptTokens:     1_000_000,
			completionTokens: 1_000_000,
			wantCost:         2.50 + 10.00, // 12.50
		},
		{
			name:             "unknown model",
			provider:         "unknown",
			model:            "unknown-model",
			promptTokens:     1000,
			completionTokens: 1000,
			wantCost:         0,
		},
		{
			name:             "zero tokens",
			provider:         "openai",
			model:            "gpt-4o-mini",
			promptTokens:     0,
			completionTokens: 0,
			wantCost:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := table.CalculateCost(tt.provider, tt.model, tt.promptTokens, tt.completionTokens)
			if math.Abs(got-tt.wantCost) > 1e-10 {
				t.Errorf("CalculateCost() = %v, want %v", got, tt.wantCost)
			}
		})
	}
}

func TestNilPriceTable(t *testing.T) {
	var table *PriceTable
	cost := table.CalculateCost("openai", "gpt-4o", 1000, 1000)
	if cost != 0 {
		t.Errorf("expected 0 cost for nil table, got %v", cost)
	}
}

func TestEmptyPriceTable(t *testing.T) {
	table := NewPriceTable(nil)
	cost := table.CalculateCost("openai", "gpt-4o", 1000, 1000)
	if cost != 0 {
		t.Errorf("expected 0 cost for empty table, got %v", cost)
	}
}
