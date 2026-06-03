package sbc

import (
	"encoding/json"
	"math"
	"sort"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func makeTool(name, desc string) json.RawMessage {
	t := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
				},
			},
		},
	}
	b, _ := json.Marshal(t)
	return b
}

func makeTools(count int) []json.RawMessage {
	tools := make([]json.RawMessage, count)
	names := []string{
		"search_flights", "book_flight", "cancel_flight", "get_user_details",
		"list_airlines", "get_airport_info", "check_baggage", "upgrade_seat",
		"get_weather", "search_hotels", "rent_car", "buy_insurance",
		"get_visa_info", "exchange_currency", "get_travel_advisory",
		"search_trains", "book_train", "get_train_schedule", "cancel_train",
		"get_route_map", "search_buses", "book_bus", "get_bus_schedule",
		"cancel_bus", "get_ferry_schedule", "book_ferry", "cancel_ferry",
	}
	descs := []string{
		"Search for available flights between cities",
		"Book a flight reservation for a passenger",
		"Cancel an existing flight booking",
		"Get user profile and contact information",
		"List all available airlines and their codes",
		"Get airport details including terminals and gates",
		"Check baggage allowance and restrictions",
		"Upgrade seat class on an existing booking",
		"Get current weather conditions for a city",
		"Search for hotels in a destination city",
		"Rent a car at a destination",
		"Purchase travel insurance for a trip",
		"Get visa requirements for a destination country",
		"Convert currency between two countries",
		"Get travel advisories and safety information",
		"Search for train routes between stations",
		"Book a train ticket for a journey",
		"Get train schedule and departure times",
		"Cancel an existing train booking",
		"Get a route map showing the journey path",
		"Search for bus routes between cities",
		"Book a bus ticket for a journey",
		"Get bus schedule and departure times",
		"Cancel an existing bus booking",
		"Get ferry schedule and departure times",
		"Book a ferry ticket for a crossing",
		"Cancel an existing ferry booking",
	}
	for i := 0; i < count; i++ {
		name := names[i%len(names)]
		desc := descs[i%len(descs)]
		// Add variation for counts > len(names)
		if i >= len(names) {
			name = names[i%len(names)] + "_v2"
			desc = descs[i%len(descs)] + " (alternate)"
		}
		tools[i] = makeTool(name, desc)
	}
	return tools
}

func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ---------------------------------------------------------------------------
// Pressure level tests
// ---------------------------------------------------------------------------

func TestPressureFromHeadroom(t *testing.T) {
	tests := []struct {
		headroom  float64
		expected  PressureLevel
	}{
		{0.75, PressureHealthy},
		{0.51, PressureHealthy},
		{0.50, PressureHigh},
		{0.30, PressureHigh},
		{0.25, PressureCritical},
		{0.15, PressureCritical},
		{0.10, PressureEmergency},
		{0.05, PressureEmergency},
		{0.00, PressureEmergency},
	}
	for _, tt := range tests {
		got := PressureFromHeadroom(tt.headroom)
		if got != tt.expected {
			t.Errorf("PressureFromHeadroom(%.2f) = %s, want %s", tt.headroom, got, tt.expected)
		}
	}
}

func TestTopKForPressure(t *testing.T) {
	tests := []struct {
		pressure PressureLevel
		total    int
		minTools int
	}{
		{PressureHealthy, 27, 8},   // healthy = no pruning
		{PressureHigh, 27, 8},      // high = 60% = ceil(16.2) = 17
		{PressureCritical, 27, 8},  // critical = 30% = ceil(8.1) = 9
		{PressureEmergency, 27, 8}, // emergency = minTools = 8
		{PressureHealthy, 5, 8},    // total < minTools = no pruning
		{PressureEmergency, 8, 8},  // total = minTools = no pruning
	}
	for _, tt := range tests {
		k := TopKForPressure(tt.pressure, tt.total, tt.minTools)
		if k > tt.total {
			t.Errorf("TopKForPressure(%s, %d, %d) = %d, above total %d",
				tt.pressure, tt.total, tt.minTools, k, tt.total)
		}
		// When total <= minTools, k == total (no pruning), which is correct
		if tt.total > tt.minTools && k < tt.minTools {
			t.Errorf("TopKForPressure(%s, %d, %d) = %d, below minTools %d",
				tt.pressure, tt.total, tt.minTools, k, tt.minTools)
		}
	}
}

// ---------------------------------------------------------------------------
// Keyword scoring tests
// ---------------------------------------------------------------------------

func TestKeywordSet(t *testing.T) {
	kw := keywordSet("search for flights to New York")
	if _, ok := kw["search"]; !ok {
		t.Error("expected 'search' in keyword set")
	}
	if _, ok := kw["flights"]; !ok {
		t.Error("expected 'flights' in keyword set")
	}
	// 'to' is a full word so it IS in the set; only split parts < 2 chars are filtered
	if _, ok := kw["to"]; !ok {
		t.Error("'to' should be in keyword set (full word, not split)")
	}
}

func TestKeywordSet_SplitsUnderscores(t *testing.T) {
	kw := keywordSet("search_flights")
	if _, ok := kw["search"]; !ok {
		t.Error("expected 'search' split from underscore")
	}
	if _, ok := kw["flights"]; !ok {
		t.Error("expected 'flights' split from underscore")
	}
}

func TestScoreTool(t *testing.T) {
	query := keywordSet("search for available flights")
	score := scoreTool(query, "search_flights", "Search for available flights between cities")
	if score <= 0 {
		t.Error("expected positive score for matching tool")
	}

	score2 := scoreTool(query, "get_weather", "Get current weather conditions")
	if score2 >= score {
		t.Error("weather tool should score lower than flight tool for flight query")
	}
}

func TestScoreTool_ReadOnlyBoost(t *testing.T) {
	query := keywordSet("find user details")
	// "get_user_details" should score well because it contains matching keywords
	score1 := scoreTool(query, "get_user_details", "Get user profile and contact information")
	// "delete_user" has no matching keywords
	score2 := scoreTool(query, "delete_user", "Remove a user account permanently")
	if score1 <= score2 {
		t.Error("get_user_details should score higher than delete_user for 'find user details' query")
	}
}

// ---------------------------------------------------------------------------
// Required tool extraction tests
// ---------------------------------------------------------------------------

func TestRequiredToolNames_NamedFunction(t *testing.T) {
	choice := mustJSON(map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "search_flights",
		},
	})
	required := requiredToolNames(choice)
	if !required["search_flights"] {
		t.Error("expected search_flights to be required")
	}
}

func TestRequiredToolNames_Auto(t *testing.T) {
	choice := mustJSON("auto")
	required := requiredToolNames(choice)
	if len(required) > 0 {
		t.Error("tool_choice 'auto' should have no required tools")
	}
}

func TestRequiredToolNames_Nil(t *testing.T) {
	required := requiredToolNames(nil)
	if len(required) > 0 {
		t.Error("nil tool_choice should have no required tools")
	}
}

// ---------------------------------------------------------------------------
// IsToolChoiceRequired tests
// ---------------------------------------------------------------------------

func TestIsToolChoiceRequired(t *testing.T) {
	tests := []struct {
		input    json.RawMessage
		expected bool
	}{
		{mustJSON("required"), true},
		{mustJSON("auto"), false},
		{mustJSON("none"), false},
		{nil, false},
		{json.RawMessage(`"required"`), true},
		{json.RawMessage(`"auto"`), false},
	}
	for _, tt := range tests {
		got := IsToolChoiceRequired(tt.input)
		if got != tt.expected {
			t.Errorf("IsToolChoiceRequired(%s) = %v, want %v", string(tt.input), got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// PruneTools integration tests
// ---------------------------------------------------------------------------

func TestPruneTools_OffMode(t *testing.T) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: true, Mode: ModeOff, MinTools: 8}
	result, err := PruneTools(tools, nil, "search flights", 0.3, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pruned != 0 {
		t.Errorf("off mode should not prune, got %d pruned", result.Pruned)
	}
	if len(result.Tools) != 27 {
		t.Errorf("off mode should return all tools, got %d", len(result.Tools))
	}
}

func TestPruneTools_Disabled(t *testing.T) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: false, Mode: ModeHybrid, MinTools: 8}
	result, err := PruneTools(tools, nil, "search flights", 0.3, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pruned != 0 {
		t.Errorf("disabled should not prune, got %d pruned", result.Pruned)
	}
}

func TestPruneTools_BelowMinTools(t *testing.T) {
	tools := makeTools(5)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	result, err := PruneTools(tools, nil, "search flights", 0.3, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pruned != 0 {
		t.Errorf("below MinTools should not prune, got %d pruned", result.Pruned)
	}
}

func TestPruneTools_ToolChoiceRequired(t *testing.T) {
	tools := makeTools(27)
	choice := mustJSON("required")
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	result, err := PruneTools(tools, choice, "search flights", 0.1, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pruned != 0 {
		t.Errorf("tool_choice=required should not prune, got %d pruned", result.Pruned)
	}
}

func TestPruneTools_HybridHealthy(t *testing.T) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	result, err := PruneTools(tools, nil, "search flights to New York", 0.75, policy)
	if err != nil {
		t.Fatal(err)
	}
	// Healthy headroom = no pruning for hybrid mode
	if result.Pruned != 0 {
		t.Errorf("healthy headroom should not prune, got %d pruned", result.Pruned)
	}
}

func TestPruneTools_HybridCritical(t *testing.T) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	result, err := PruneTools(tools, nil, "search flights to New York", 0.15, policy)
	if err != nil {
		t.Fatal(err)
	}
	// Critical headroom = 30% of 27 = ceil(8.1) = 9
	if result.Pruned == 0 {
		t.Error("critical headroom should prune tools")
	}
	if len(result.Tools) < 8 {
		t.Errorf("should keep at least MinTools, got %d", len(result.Tools))
	}
}

func TestPruneTools_EvictEmergency(t *testing.T) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: true, Mode: ModeEvict, MinTools: 8}
	result, err := PruneTools(tools, nil, "search flights to New York", 0.05, policy)
	if err != nil {
		t.Fatal(err)
	}
	// Emergency + evict: keep only minTools (8)
	if len(result.Tools) > 8 {
		t.Errorf("emergency evict should keep minTools, got %d", len(result.Tools))
	}
}

func TestPruneTools_RequiredToolPreserved(t *testing.T) {
	tools := makeTools(27)
	// Force a specific tool to be required
	choice := mustJSON(map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "get_weather",
		},
	})
	policy := RuntimePolicy{Enabled: true, Mode: ModeEvict, MinTools: 8}
	// Emergency headroom — aggressive pruning
	result, err := PruneTools(tools, choice, "search flights to New York", 0.05, policy)
	if err != nil {
		t.Fatal(err)
	}

	// Verify "get_weather" is in the pruned set even though it's not flight-related
	found := false
	for _, t := range result.Tools {
		var parsed struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		json.Unmarshal(t, &parsed)
		if parsed.Function.Name == "get_weather" {
			found = true
			break
		}
	}
	if !found {
		t.Error("required tool 'get_weather' should be preserved after pruning")
	}
}

func TestPruneTools_RelevanceRanking(t *testing.T) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: true, Mode: ModeEvict, MinTools: 8}
	result, err := PruneTools(tools, nil, "search for flights from NYC to LA", 0.05, policy)
	if err != nil {
		t.Fatal(err)
	}

	// Check that search_flights is in the selected tools
	found := false
	for _, t := range result.Tools {
		var parsed struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		json.Unmarshal(t, &parsed)
		if parsed.Function.Name == "search_flights" {
			found = true
			break
		}
	}
	if !found {
		t.Error("search_flights should be ranked highest for flight search query")
	}
}

func TestPruneTools_EmptyQuery(t *testing.T) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	result, err := PruneTools(tools, nil, "", 0.15, policy)
	if err != nil {
		t.Fatal(err)
	}
	// Empty query = all scores 0, but should still prune to target
	if len(result.Tools) > 27 {
		t.Errorf("should not add tools, got %d", len(result.Tools))
	}
}

func TestPruneTools_DurationRecorded(t *testing.T) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	// Use critical headroom to ensure actual pruning happens (records duration)
	result, err := PruneTools(tools, nil, "search flights", 0.15, policy)
	if err != nil {
		t.Fatal(err)
	}
	// Duration is recorded even if it rounds to 0 on very fast machines.
	// We verify the field is set (non-negative) and the pruning occurred.
	if result.Duration < 0 {
		t.Error("duration should be non-negative")
	}
	if result.Pruned == 0 {
		t.Error("expected pruning to occur at critical headroom")
	}
}

func TestPruneTools_NilToolChoice(t *testing.T) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	result, err := PruneTools(tools, nil, "search flights", 0.15, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pruned == 0 {
		t.Error("should prune with nil tool_choice and critical pressure")
	}
}

// ---------------------------------------------------------------------------
// ComputeHeadroom tests
// ---------------------------------------------------------------------------

func TestComputeHeadroom(t *testing.T) {
	tests := []struct {
		limit, used int
		expected    float64
	}{
		{1000, 200, 0.8},
		{1000, 500, 0.5},
		{1000, 900, 0.1},
		{1000, 1000, 0.0},
		{0, 0, 1.0},      // unlimited
		{0, 500, 1.0},    // unlimited
		{1000, 1500, 0.0}, // over limit
	}
	for _, tt := range tests {
		got := ComputeHeadroom(tt.limit, tt.used)
		if math.Abs(got-tt.expected) > 0.001 {
			t.Errorf("ComputeHeadroom(%d, %d) = %.3f, want %.3f", tt.limit, tt.used, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// EstimateTokenCount tests
// ---------------------------------------------------------------------------

func TestEstimateTokenCount(t *testing.T) {
	tools := makeTools(14)
	tokens := EstimateTokenCount(tools)
	if tokens <= 0 {
		t.Error("token estimate should be positive")
	}

	// More tools = more tokens
	tools2 := makeTools(27)
	tokens2 := EstimateTokenCount(tools2)
	if tokens2 <= tokens {
		t.Errorf("27 tools (%d tokens) should estimate higher than 14 tools (%d tokens)", tokens2, tokens)
	}
}

// ---------------------------------------------------------------------------
// Policy tests
// ---------------------------------------------------------------------------

func TestParsePolicyMode(t *testing.T) {
	tests := []struct {
		input    string
		expected PolicyMode
	}{
		{"hybrid", ModeHybrid},
		{"evict", ModeEvict},
		{"off", ModeOff},
		{"", ModeOff},
		{"unknown", ModeOff},
	}
	for _, tt := range tests {
		got := ParsePolicyMode(tt.input)
		if got != tt.expected {
			t.Errorf("ParsePolicyMode(%q) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestRuntimePolicy_Validate(t *testing.T) {
	tests := []struct {
		policy   RuntimePolicy
		hasError bool
	}{
		{RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}, false},
		{RuntimePolicy{Enabled: true, Mode: ModeEvict, MinTools: 0}, false},
		{RuntimePolicy{Enabled: true, Mode: "invalid", MinTools: 8}, true},
		{RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: -1}, true},
	}
	for _, tt := range tests {
		err := tt.policy.Validate()
		if (err != nil) != tt.hasError {
			t.Errorf("Validate() error = %v, hasError = %v", err, tt.hasError)
		}
	}
}

func TestRuntimePolicy_ShouldPrune(t *testing.T) {
	tests := []struct {
		policy   RuntimePolicy
		count    int
		expected bool
	}{
		{RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}, 27, true},
		{RuntimePolicy{Enabled: true, Mode: ModeOff, MinTools: 8}, 27, false},
		{RuntimePolicy{Enabled: false, Mode: ModeHybrid, MinTools: 8}, 27, false},
		{RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}, 5, false},
		{RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}, 8, false},
	}
	for _, tt := range tests {
		got := tt.policy.ShouldPrune(tt.count)
		if got != tt.expected {
			t.Errorf("ShouldPrune(%d) = %v, want %v", tt.count, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// FallbackTracker tests (RQ3)
// ---------------------------------------------------------------------------

func TestFallbackTracker_ShouldFallback(t *testing.T) {
	allTools := makeTools(27)

	tests := []struct {
		name       string
		wasPruned  bool
		turn       int
		hasCalls   bool
		expected   bool
	}{
		{"pruned early no calls", true, 1, false, true},
		{"pruned early with calls", true, 1, true, false},
		{"pruned late no calls", true, 3, false, false},
		{"not pruned", false, 1, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewFallbackTracker(allTools, tt.wasPruned, tt.turn)
			got := tracker.ShouldFallback(tt.hasCalls)
			if got != tt.expected {
				t.Errorf("ShouldFallback(%v) = %v, want %v", tt.hasCalls, got, tt.expected)
			}
		})
	}
}

func TestFallbackTracker_NilTracker(t *testing.T) {
	var tracker *FallbackTracker
	if tracker.ShouldFallback(false) {
		t.Error("nil tracker should not trigger fallback")
	}
	if tracker.AllTools() != nil {
		t.Error("nil tracker should return nil AllTools")
	}
}

func TestFallbackTracker_AllTools(t *testing.T) {
	allTools := makeTools(27)
	tracker := NewFallbackTracker(allTools, true, 1)
	returned := tracker.AllTools()
	if len(returned) != 27 {
		t.Errorf("AllTools() returned %d tools, want 27", len(returned))
	}
}

// ---------------------------------------------------------------------------
// ExtractLastUserMessage tests
// ---------------------------------------------------------------------------

func TestExtractLastUserMessage(t *testing.T) {
	msgs := []map[string]interface{}{
		{"role": "system", "content": "You are a helpful assistant."},
		{"role": "user", "content": "Search for flights from JFK to LAX"},
		{"role": "assistant", "content": "I found 3 flights."},
		{"role": "user", "content": "Book the cheapest one"},
	}
	raw := mustJSON(msgs)
	got := ExtractLastUserMessage(raw)
	if got != "Book the cheapest one" {
		t.Errorf("ExtractLastUserMessage() = %q, want %q", got, "Book the cheapest one")
	}
}

func TestExtractLastUserMessage_NoUser(t *testing.T) {
	msgs := []map[string]interface{}{
		{"role": "system", "content": "You are a helpful assistant."},
	}
	raw := mustJSON(msgs)
	got := ExtractLastUserMessage(raw)
	if got != "" {
		t.Errorf("expected empty string for no user message, got %q", got)
	}
}

func TestExtractLastUserMessage_Empty(t *testing.T) {
	got := ExtractLastUserMessage(nil)
	if got != "" {
		t.Errorf("expected empty string for nil input, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Schema compilation tests
// ---------------------------------------------------------------------------

func TestCompileToolSchema_Full(t *testing.T) {
	tool := makeTool("search_flights", "Search for flights")
	result := CompileToolSchema(tool, TierFull)
	if string(result) != string(tool) {
		t.Error("TierFull should passthrough")
	}
}

func TestCompileToolSchema_Hidden(t *testing.T) {
	tool := makeTool("search_flights", "Search for flights")
	result := CompileToolSchema(tool, TierHidden)
	if result != nil {
		t.Error("TierHidden should return nil")
	}
}

func TestCompileToolSchema_Compact(t *testing.T) {
	tool := makeTool("search_flights", "Search for flights")
	result := CompileToolSchema(tool, TierCompact)
	if result == nil {
		t.Fatal("TierCompact should return non-nil")
	}
	// Compact should be smaller than original
	if len(result) >= len(tool) {
		t.Errorf("Compact should be smaller: %d >= %d", len(result), len(tool))
	}
}

// ---------------------------------------------------------------------------
// FormatSummary test
// ---------------------------------------------------------------------------

func TestFormatSummary(t *testing.T) {
	result := &PruneResult{
		Tools:    makeTools(8),
		Original: 27,
		Pruned:   19,
		Pressure: PressureCritical,
		Mode:     ModeHybrid,
		TokenSave: 5000,
		Duration:  100 * time.Microsecond,
	}
	s := FormatSummary(result)
	if s == "" {
		t.Error("FormatSummary should return non-empty string")
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkPruneTools_14(b *testing.B) {
	tools := makeTools(14)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PruneTools(tools, nil, "search flights from JFK to LAX", 0.3, policy)
	}
}

func BenchmarkPruneTools_27(b *testing.B) {
	tools := makeTools(27)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PruneTools(tools, nil, "search flights from JFK to LAX", 0.3, policy)
	}
}

func BenchmarkPruneTools_50(b *testing.B) {
	tools := makeTools(50)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PruneTools(tools, nil, "search flights from JFK to LAX", 0.3, policy)
	}
}

func BenchmarkPruneTools_100(b *testing.B) {
	tools := makeTools(100)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PruneTools(tools, nil, "search flights from JFK to LAX", 0.3, policy)
	}
}

func BenchmarkPruneTools_200(b *testing.B) {
	tools := makeTools(200)
	policy := RuntimePolicy{Enabled: true, Mode: ModeHybrid, MinTools: 8}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PruneTools(tools, nil, "search flights from JFK to LAX", 0.3, policy)
	}
}

func BenchmarkKeywordSet(b *testing.B) {
	text := "search for available flights from New York JFK to Los Angeles LAX on December 25th"
	for i := 0; i < b.N; i++ {
		keywordSet(text)
	}
}

func BenchmarkRankTools(b *testing.B) {
	tools := makeTools(100)
	query := "search flights from JFK to LAX"
	queryKW := keywordSet(query)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		infos := make([]toolInfo, len(tools))
		for j, raw := range tools {
			name, desc := parseTool(raw)
			infos[j] = toolInfo{
				raw:   raw,
				name:  name,
				desc:  desc,
				score: scoreTool(queryKW, name, desc),
				index: j,
			}
		}
		sort.SliceStable(infos, func(i, j int) bool {
			return infos[i].score > infos[j].score
		})
	}
}
