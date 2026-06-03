// Package sbc implements Schema-Budget Coupling for MCP tool explosion.
//
// When an AI gateway merges many MCP tools into a single request, the token
// budget can be consumed by tool definitions alone. SBC ranks tools by
// relevance to the current query and prunes low-value ones based on the
// remaining TPM headroom.
//
// Modes:
//   - off:     passthrough — send all tools (default)
//   - hybrid:  rank + top-k selection by pressure level
//   - evict:   aggressive ranking + top-k (still ≥ MinTools)
//
// Invariants:
//   - Never prune below MinTools (default 8)
//   - Never prune tools explicitly required by tool_choice
//   - Failure is always passthrough (send all tools)
//   - Zero external dependencies
package sbc

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"openlimit/internal/billing"
)

// ---------------------------------------------------------------------------
// Pressure levels
// ---------------------------------------------------------------------------

// PressureLevel represents how tight the token budget is.
type PressureLevel string

const (
	PressureHealthy  PressureLevel = "healthy"  // >50% headroom
	PressureHigh     PressureLevel = "high"     // 25–50% headroom
	PressureCritical PressureLevel = "critical" // 10–25% headroom
	PressureEmergency PressureLevel = "emergency" // <10% headroom
)

// PressureFromHeadroom maps a headroom fraction (0.0–1.0) to a pressure level.
// headroom = (limit - used) / limit
func PressureFromHeadroom(headroom float64) PressureLevel {
	switch {
	case headroom > 0.50:
		return PressureHealthy
	case headroom > 0.25:
		return PressureHigh
	case headroom > 0.10:
		return PressureCritical
	default:
		return PressureEmergency
	}
}

// TopKForPressure returns the target tool count for a given pressure level
// and total tool count. It never goes below minTools.
func TopKForPressure(pressure PressureLevel, totalTools, minTools int) int {
	if totalTools <= minTools {
		return totalTools // not worth pruning
	}

	var target int
	switch pressure {
	case PressureHealthy:
		target = totalTools // no pruning when healthy
	case PressureHigh:
		// Keep top 60% or minTools, whichever is larger
		target = int(math.Ceil(float64(totalTools) * 0.60))
	case PressureCritical:
		// Keep top 30% or minTools
		target = int(math.Ceil(float64(totalTools) * 0.30))
	case PressureEmergency:
		// Keep minTools only
		target = minTools
	default:
		target = totalTools
	}

	if target < minTools {
		target = minTools
	}
	if target > totalTools {
		target = totalTools
	}
	return target
}

// ---------------------------------------------------------------------------
// Tool scoring
// ---------------------------------------------------------------------------

// toolInfo is a minimal parsed tool used for scoring.
type toolInfo struct {
	raw    json.RawMessage
	name   string
	desc   string
	score  float64
	index  int // original position for stable sort
}

// parseTool extracts name and description from a JSON tool definition.
func parseTool(raw json.RawMessage) (name, desc string) {
	var t struct {
		Type     string `json:"type"`
		Function struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return "", ""
	}
	return t.Function.Name, t.Function.Description
}

// keywordSet splits text into lowercase tokens for overlap scoring.
func keywordSet(text string) map[string]struct{} {
	words := strings.Fields(strings.ToLower(text))
	// Also split on underscores and hyphens
	expanded := make(map[string]struct{}, len(words)*2)
	for _, w := range words {
		expanded[w] = struct{}{}
		for _, part := range strings.FieldsFunc(w, func(r rune) bool {
			return r == '_' || r == '-'
		}) {
			if len(part) > 1 {
				expanded[part] = struct{}{}
			}
		}
	}
	return expanded
}

// scoreTool computes keyword overlap between a query and a tool's name+description.
func scoreTool(queryKeywords map[string]struct{}, name, desc string) float64 {
	toolText := strings.ToLower(name + " " + desc)
	toolKeywords := keywordSet(toolText)

	if len(queryKeywords) == 0 {
		return 0
	}

	overlap := 0
	for kw := range queryKeywords {
		if _, ok := toolKeywords[kw]; ok {
			overlap++
		}
	}

	// Normalize by query length to get 0–1 score
	return float64(overlap) / float64(len(queryKeywords))
}

// ---------------------------------------------------------------------------
// Required tool extraction
// ---------------------------------------------------------------------------

// requiredToolNames extracts tool names from a tool_choice that specifies
// a particular function: {"type": "function", "function": {"name": "x"}}
func requiredToolNames(toolChoice json.RawMessage) map[string]bool {
	if len(toolChoice) == 0 {
		return nil
	}

	// Try object form first
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(toolChoice, &obj); err == nil {
		if obj.Type == "function" && obj.Function.Name != "" {
			return map[string]bool{obj.Function.Name: true}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// PruneTools — the main entry point
// ---------------------------------------------------------------------------

// PruneResult holds the outcome of tool pruning.
type PruneResult struct {
	Tools     []json.RawMessage // pruned tool list
	Original  int               // original tool count
	Pruned    int               // number of tools removed
	Pressure  PressureLevel     // pressure level that drove pruning
	Mode      PolicyMode        // policy mode used
	TokenSave int               // approximate bytes saved
	Duration  time.Duration     // pruning latency
}

// IsToolChoiceRequired checks if tool_choice is set to "required",
// meaning the model MUST call a tool. In this case, we should not prune.
func IsToolChoiceRequired(toolChoice json.RawMessage) bool {
	if len(toolChoice) == 0 {
		return false
	}
	var s string
	if err := json.Unmarshal(toolChoice, &s); err == nil {
		return s == "required"
	}
	return false
}

// PruneTools ranks tools by relevance to the query and selects the top-k
// based on pressure level and policy mode.
//
// Parameters:
//   - tools: merged tool list ([]json.RawMessage)
//   - toolChoice: the tool_choice from the request (may be nil)
//   - query: the last user message text for relevance scoring
//   - headroom: TPM headroom fraction (0.0–1.0)
//   - policy: the runtime policy controlling pruning behavior
//
// On any error, returns the original tools unchanged (failure = passthrough).
func PruneTools(
	tools []json.RawMessage,
	toolChoice json.RawMessage,
	query string,
	headroom float64,
	policy RuntimePolicy,
) (*PruneResult, error) {
	start := time.Now()

	original := len(tools)

	// Off mode: passthrough
	if policy.Mode == ModeOff || !policy.Enabled {
		return &PruneResult{
			Tools:    tools,
			Original: original,
			Pruned:   0,
			Pressure: PressureFromHeadroom(headroom),
			Mode:     policy.Mode,
			Duration: time.Since(start),
		}, nil
	}

	// Never prune when tool_choice is "required" — model MUST call a tool
	if IsToolChoiceRequired(toolChoice) {
		return &PruneResult{
			Tools:    tools,
			Original: original,
			Pruned:   0,
			Pressure: PressureFromHeadroom(headroom),
			Mode:     policy.Mode,
			Duration: time.Since(start),
		}, nil
	}

	// Not enough tools to prune
	if original <= policy.MinTools {
		return &PruneResult{
			Tools:    tools,
			Original: original,
			Pruned:   0,
			Pressure: PressureFromHeadroom(headroom),
			Mode:     policy.Mode,
			Duration: time.Since(start),
		}, nil
	}

	// Compute pressure and target count
	pressure := PressureFromHeadroom(headroom)
	topK := TopKForPressure(pressure, original, policy.MinTools)

	// For evict mode, be more aggressive
	if policy.Mode == ModeEvict && topK > policy.MinTools {
		// Evict reduces to 40% of what hybrid would keep
		evictK := int(math.Ceil(float64(topK) * 0.40))
		if evictK < policy.MinTools {
			evictK = policy.MinTools
		}
		topK = evictK
	}

	// If no pruning needed, passthrough
	if topK >= original {
		return &PruneResult{
			Tools:    tools,
			Original: original,
			Pruned:   0,
			Pressure: pressure,
			Mode:     policy.Mode,
			Duration: time.Since(start),
		}, nil
	}

	// Parse and score all tools
	infos := make([]toolInfo, len(tools))
	queryKeywords := keywordSet(query)

	var totalBytes int
	for i, raw := range tools {
		name, desc := parseTool(raw)
		infos[i] = toolInfo{
			raw:   raw,
			name:  name,
			desc:  desc,
			score: scoreTool(queryKeywords, name, desc),
			index: i,
		}
		totalBytes += len(raw)
	}

	// Identify required tools (must not be pruned)
	required := requiredToolNames(toolChoice)

	// Sort by score descending (stable by original index)
	sort.SliceStable(infos, func(i, j int) bool {
		if infos[i].score != infos[j].score {
			return infos[i].score > infos[j].score
		}
		return infos[i].index < infos[j].index
	})

	// Select top-K, preserving required tools regardless of score
	selected := make([]json.RawMessage, 0, topK)
	selectedNames := make(map[string]bool)
	var savedBytes int

	for _, info := range infos {
		if len(selected) >= topK && !required[info.name] {
			savedBytes += len(info.raw)
			continue
		}
		// Always include required tools
		if required[info.name] {
			selected = append(selected, info.raw)
			selectedNames[info.name] = true
			continue
		}
		if len(selected) < topK {
			selected = append(selected, info.raw)
			selectedNames[info.name] = true
		} else {
			savedBytes += len(info.raw)
		}
	}

	// If we have fewer than MinTools after required-only, pad with top-scored
	if len(selected) < policy.MinTools {
		for _, info := range infos {
			if len(selected) >= policy.MinTools {
				break
			}
			if !selectedNames[info.name] {
				selected = append(selected, info.raw)
				selectedNames[info.name] = true
				savedBytes -= len(info.raw)
			}
		}
	}

	pruned := original - len(selected)

	return &PruneResult{
		Tools:     selected,
		Original:  original,
		Pruned:    pruned,
		Pressure:  pressure,
		Mode:      policy.Mode,
		TokenSave: savedBytes,
		Duration:  time.Since(start),
	}, nil
}

// ---------------------------------------------------------------------------
// EstimateTokenCount gives a rough token estimate for a tool list.
// Uses ~4 chars per token as a conservative estimate.
// ---------------------------------------------------------------------------

// EstimateTokenCount estimates the number of tokens in a tool list.
func EstimateTokenCount(tools []json.RawMessage) int {
	totalBytes := 0
	for _, t := range tools {
		totalBytes += len(t)
	}
	// Rough: 1 token ≈ 4 bytes for JSON
	return totalBytes / 4
}

// ---------------------------------------------------------------------------
// Metrics recording (optional integration point)
// ---------------------------------------------------------------------------

// MetricsRecorder is an optional interface for recording SBC metrics.
// The metrics.Collector can implement this to get SBC-specific telemetry.
type MetricsRecorder interface {
	RecordSBCPruning(mode string, pressure string, original, pruned, tokenSave int, duration time.Duration)
}

// DefaultMetrics is a no-op metrics recorder.
type DefaultMetrics struct{}

func (d *DefaultMetrics) RecordSBCPruning(mode string, pressure string, original, pruned, tokenSave int, duration time.Duration) {}

// globalMetrics holds the optional metrics recorder.
var (
	globalMetrics MetricsRecorder = &DefaultMetrics{}
	metricsMu     sync.Mutex
)

// SetMetricsRecorder sets the global metrics recorder.
func SetMetricsRecorder(m MetricsRecorder) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	if m != nil {
		globalMetrics = m
	}
}

// RecordPruning records pruning metrics via the global recorder.
func RecordPruning(result *PruneResult) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	globalMetrics.RecordSBCPruning(
		string(result.Mode),
		string(result.Pressure),
		result.Original,
		result.Pruned,
		result.TokenSave,
		result.Duration,
	)
}

// ---------------------------------------------------------------------------
// Headroom computation helper
// ---------------------------------------------------------------------------

// ComputeHeadroom computes TPM headroom from limit and used values.
// Returns 1.0 when limit is 0 (unlimited).
func ComputeHeadroom(tpmLimit, tpmUsed int) float64 {
	if tpmLimit <= 0 {
		return 1.0 // unlimited = full headroom
	}
	if tpmUsed >= tpmLimit {
		return 0.0
	}
	return float64(tpmLimit-tpmUsed) / float64(tpmLimit)
}

// ExtractLastUserMessage extracts the text of the last user message from a
// ChatCompletionRequest's messages field. Returns empty string if none found.
func ExtractLastUserMessage(messages json.RawMessage) string {
	if len(messages) == 0 {
		return ""
	}

	// Try array of messages
	var msgs []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messages, &msgs); err != nil {
		return ""
	}

	// Find last user message
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			content := string(msgs[i].Content)
			// Strip JSON quotes if present
			if len(content) >= 2 && content[0] == '"' && content[len(content)-1] == '"' {
				content = content[1 : len(content)-1]
			}
			return content
		}
	}
	return ""
}

// FormatSummary returns a human-readable summary of a PruneResult.
func FormatSummary(r *PruneResult) string {
	return fmt.Sprintf("SBC: %d→%d tools (%d pruned, pressure=%s, mode=%s, saved=%d bytes, %v)",
		r.Original, len(r.Tools), r.Pruned, r.Pressure, r.Mode, r.TokenSave, r.Duration.Round(time.Microsecond))
}

// ---------------------------------------------------------------------------
// Cost estimation (RQ2: dollar-cost projection)
// ---------------------------------------------------------------------------

// CostEstimate holds dollar-cost savings from SBC pruning.
type CostEstimate struct {
	OriginalTokens  int     // estimated tokens before pruning
	PrunedTokens    int     // estimated tokens after pruning
	TokensSaved     int     // OriginalTokens - PrunedTokens
	CostWithoutSBC  float64 // USD without SBC (all tools)
	CostWithSBC     float64 // USD with SBC (pruned tools)
	SavingsUSD      float64 // CostWithoutSBC - CostWithSBC
	SavingsPercent  float64 // (SavingsUSD / CostWithoutSBC) * 100
}

// EstimateCost computes dollar-cost savings from pruning using a PriceTable.
// It estimates the prompt-token cost of tool schemas only (not the full prompt).
func EstimateCost(result *PruneResult, allTools []json.RawMessage, prices *billing.PriceTable, provider, model string) *CostEstimate {
	if result == nil || prices == nil {
		return nil
	}

	originalTokens := EstimateTokenCount(allTools)
	prunedTokens := EstimateTokenCount(result.Tools)
	saved := originalTokens - prunedTokens

	costWithout := prices.CalculateCost(provider, model, originalTokens, 0)
	costWith := prices.CalculateCost(provider, model, prunedTokens, 0)

	savingsUSD := costWithout - costWith
	var savingsPct float64
	if costWithout > 0 {
		savingsPct = (savingsUSD / costWithout) * 100
	}

	return &CostEstimate{
		OriginalTokens:  originalTokens,
		PrunedTokens:    prunedTokens,
		TokensSaved:     saved,
		CostWithoutSBC:  costWithout,
		CostWithSBC:     costWith,
		SavingsUSD:      savingsUSD,
		SavingsPercent:  savingsPct,
	}
}

// ---------------------------------------------------------------------------
// Fallback heuristic (RQ3: accuracy recovery)
// ---------------------------------------------------------------------------

// FallbackTracker tracks whether SBC should fall back to all tools.
// The fallback heuristic: if the model responds with text-only (no tool_calls)
// on an early turn (≤2) and tools were pruned, retry with all tools.
type FallbackTracker struct {
	allTools []json.RawMessage
	pruned   bool
	turn     int
}

// NewFallbackTracker creates a tracker that remembers the original tool set.
func NewFallbackTracker(allTools []json.RawMessage, wasPruned bool, turn int) *FallbackTracker {
	return &FallbackTracker{
		allTools: allTools,
		pruned:   wasPruned,
		turn:     turn,
	}
}

// ShouldFallback returns true if the fallback heuristic should trigger:
// tools were pruned AND the response has no tool calls AND turn ≤ 2.
func (f *FallbackTracker) ShouldFallback(responseHasToolCalls bool) bool {
	if f == nil || !f.pruned {
		return false
	}
	return !responseHasToolCalls && f.turn <= 2
}

// AllTools returns the original un-pruned tool set for fallback.
func (f *FallbackTracker) AllTools() []json.RawMessage {
	if f == nil {
		return nil
	}
	return f.allTools
}
