package billing

import (
	"openlimit/internal/config"
)

// PriceTable maps provider+model to per-token pricing.
type PriceTable struct {
	entries map[string]PriceEntry
}

// PriceEntry holds pricing for a specific provider model.
type PriceEntry struct {
	PromptPer1M     float64
	CompletionPer1M float64
}

// NewPriceTable builds a lookup table from config entries.
func NewPriceTable(prices []config.PriceEntry) *PriceTable {
	entries := make(map[string]PriceEntry, len(prices))
	for _, p := range prices {
		key := p.Provider + "/" + p.Model
		entries[key] = PriceEntry{
			PromptPer1M:     p.PromptPer1M,
			CompletionPer1M: p.CompletionPer1M,
		}
	}
	return &PriceTable{entries: entries}
}

// CalculateCost returns the cost in USD for the given token usage.
func (t *PriceTable) CalculateCost(provider, model string, promptTokens, completionTokens int) float64 {
	if t == nil {
		return 0
	}

	key := provider + "/" + model
	entry, ok := t.entries[key]
	if !ok {
		return 0
	}

	promptCost := float64(promptTokens) / 1_000_000 * entry.PromptPer1M
	completionCost := float64(completionTokens) / 1_000_000 * entry.CompletionPer1M
	return promptCost + completionCost
}
