package routing

import "sort"

// CostEntry holds pricing data for a model at a specific provider.
type CostEntry struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	InputPer1M  float64 `json:"input_per_1m"`  // USD per 1M input tokens
	OutputPer1M float64 `json:"output_per_1m"` // USD per 1M output tokens
}

// CostCatalog is the embedded pricing data for popular models.
// Prices are approximate as of 2026-05.
var CostCatalog = []CostEntry{
	// DeepSeek
	{Provider: "deepseek", Model: "deepseek-chat", InputPer1M: 0.27, OutputPer1M: 1.10},
	{Provider: "deepseek", Model: "deepseek-reasoner", InputPer1M: 0.55, OutputPer1M: 2.19},
	// OpenAI
	{Provider: "openai", Model: "gpt-4o", InputPer1M: 2.50, OutputPer1M: 10.00},
	{Provider: "openai", Model: "gpt-4o-mini", InputPer1M: 0.15, OutputPer1M: 0.60},
	{Provider: "openai", Model: "gpt-4.1", InputPer1M: 2.00, OutputPer1M: 8.00},
	{Provider: "openai", Model: "gpt-4.1-mini", InputPer1M: 0.40, OutputPer1M: 1.60},
	{Provider: "openai", Model: "gpt-4.1-nano", InputPer1M: 0.10, OutputPer1M: 0.40},
	{Provider: "openai", Model: "o3", InputPer1M: 10.00, OutputPer1M: 40.00},
	{Provider: "openai", Model: "o4-mini", InputPer1M: 1.10, OutputPer1M: 4.40},
	// Anthropic
	{Provider: "anthropic", Model: "claude-sonnet-4-20250514", InputPer1M: 3.00, OutputPer1M: 15.00},
	{Provider: "anthropic", Model: "claude-3.5-haiku-20241022", InputPer1M: 0.80, OutputPer1M: 4.00},
	// Together AI
	{Provider: "together_ai", Model: "meta-llama/Llama-3.3-70B-Instruct-Turbo", InputPer1M: 0.88, OutputPer1M: 0.88},
	{Provider: "together_ai", Model: "mistralai/Mixtral-8x7B-Instruct-v0.1", InputPer1M: 0.60, OutputPer1M: 0.60},
	// xAI/Grok
	{Provider: "grok", Model: "grok-3", InputPer1M: 3.00, OutputPer1M: 15.00},
	{Provider: "grok", Model: "grok-3-mini", InputPer1M: 0.30, OutputPer1M: 0.50},
	{Provider: "xai", Model: "grok-3", InputPer1M: 3.00, OutputPer1M: 15.00},
	// Fireworks AI
	{Provider: "fireworks_ai", Model: "accounts/fireworks/models/llama-v3p1-70b-instruct", InputPer1M: 0.90, OutputPer1M: 0.90},
	// Perplexity
	{Provider: "perplexity", Model: "sonar", InputPer1M: 1.00, OutputPer1M: 1.00},
	{Provider: "perplexity", Model: "sonar-pro", InputPer1M: 3.00, OutputPer1M: 15.00},
	// Google
	{Provider: "google", Model: "gemini-2.0-flash", InputPer1M: 0.10, OutputPer1M: 0.40},
	{Provider: "google", Model: "gemini-2.5-pro-preview-05-06", InputPer1M: 1.25, OutputPer1M: 10.00},
}

// LookupCost returns the cost entry for a provider+model pair, or nil if not found.
func LookupCost(provider, model string) *CostEntry {
	for i := range CostCatalog {
		if CostCatalog[i].Provider == provider && CostCatalog[i].Model == model {
			return &CostCatalog[i]
		}
	}
	return nil
}

// avgCost returns (inputPer1M + outputPer1M) / 2 for comparison purposes.
func (e CostEntry) avgCost() float64 {
	return (e.InputPer1M + e.OutputPer1M) / 2.0
}

// medianCost returns the median avgCost from the given entries.
func medianCost(entries []CostEntry) float64 {
	if len(entries) == 0 {
		return 0
	}
	vals := make([]float64, len(entries))
	for i, e := range entries {
		vals[i] = e.avgCost()
	}
	sort.Float64s(vals)
	mid := len(vals) / 2
	if len(vals)%2 == 0 {
		return (vals[mid-1] + vals[mid]) / 2.0
	}
	return vals[mid]
}
