package main

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// BATCH-43 TASK-01/02: Benchmark tool tests
// ---------------------------------------------------------------------------

func TestPercentile_KnownDataset(t *testing.T) {
	// TEST-43-02-01: Percentile calculation for known dataset is exact
	sorted := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
		60 * time.Millisecond,
		70 * time.Millisecond,
		80 * time.Millisecond,
		90 * time.Millisecond,
		100 * time.Millisecond,
	}

	p50 := percentile(sorted, 0.50)
	p95 := percentile(sorted, 0.95)
	p99 := percentile(sorted, 0.99)

	// Nearest-rank: idx = (10-1) * p
	// p50: idx=4 → 50ms, p95: idx=8 → 90ms, p99: idx=8 → 90ms
	if p50 != 50*time.Millisecond {
		t.Errorf("P50 = %v, want 50ms", p50)
	}
	if p95 != 90*time.Millisecond {
		t.Errorf("P95 = %v, want 90ms", p95)
	}
	// P99 with nearest-rank on 10 elements: idx = 9*0.99 = 8.91 → 8 → 90ms
	if p99 != 90*time.Millisecond {
		t.Errorf("P99 = %v, want 90ms", p99)
	}
}

func TestPercentile_EmptySlice(t *testing.T) {
	// Empty slice returns 0
	result := percentile([]time.Duration{}, 0.5)
	if result != 0 {
		t.Errorf("empty slice should return 0, got %v", result)
	}
}

func TestPercentile_SingleElement(t *testing.T) {
	// Single element always returns that element
	sorted := []time.Duration{42 * time.Millisecond}
	result := percentile(sorted, 0.99)
	if result != 42*time.Millisecond {
		t.Errorf("single element should return 42ms, got %v", result)
	}
}

func TestComputeResult_RequestPerSec(t *testing.T) {
	// TEST-43-02-02: RequestsPerSec with known values
	latencies := make([]time.Duration, 1000)
	for i := range latencies {
		latencies[i] = time.Millisecond // 1ms each
	}

	result := computeResult(1000, 10, 1*time.Second, latencies, 0)

	if result.RequestsPerSec < 900 || result.RequestsPerSec > 1100 {
		t.Errorf("RPS = %.1f, want ~1000", result.RequestsPerSec)
	}
	if result.Errors != 0 {
		t.Errorf("Errors = %d, want 0", result.Errors)
	}
}

func TestComputeResult_P50LessThanP99(t *testing.T) {
	// TEST-43-01-01: P50 < P95 < P99 for non-constant data
	latencies := make([]time.Duration, 100)
	for i := range latencies {
		latencies[i] = time.Duration(i+1) * time.Millisecond
	}

	result := computeResult(100, 10, 100*time.Millisecond, latencies, 0)

	if result.P50 >= result.P95 {
		t.Errorf("P50 (%v) should be < P95 (%v)", result.P50, result.P95)
	}
	if result.P95 >= result.P99 {
		t.Errorf("P95 (%v) should be < P99 (%v)", result.P95, result.P99)
	}
}

func TestComputeResult_ErrorCounting(t *testing.T) {
	// TEST-43-01-03: Error count matches failed requests
	latencies := make([]time.Duration, 10)
	// First 7 have real latencies, last 3 are zero (error cases)
	for i := 0; i < 7; i++ {
		latencies[i] = time.Millisecond
	}
	// latencies[7], [8], [9] are 0 (errors)

	result := computeResult(10, 1, 10*time.Millisecond, latencies, 3)

	if result.Errors != 3 {
		t.Errorf("Errors = %d, want 3", result.Errors)
	}
}

func TestFormatResult_ContainsPercentiles(t *testing.T) {
	// TEST-43-01-04: FormatResults produces table with percentile labels
	result := &BenchmarkResult{
		TotalRequests:  100,
		Concurrency:   10,
		Duration:      1 * time.Second,
		P50:           5 * time.Millisecond,
		P95:           10 * time.Millisecond,
		P99:           15 * time.Millisecond,
		Min:           2 * time.Millisecond,
		Max:           20 * time.Millisecond,
		RequestsPerSec: 100,
		Errors:        0,
	}

	output := formatResult(result)

	for _, want := range []string{"P50", "P95", "P99", "Req/sec", "Errors"} {
		if !contains(output, want) {
			t.Errorf("output missing %q\n%s", want, output)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
