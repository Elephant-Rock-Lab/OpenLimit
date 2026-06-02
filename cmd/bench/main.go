package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/server"
)

// BenchmarkResult holds the results of a benchmark run.
type BenchmarkResult struct {
	TotalRequests  int
	Concurrency    int
	Duration       time.Duration
	P50            time.Duration
	P95            time.Duration
	P99            time.Duration
	Min            time.Duration
	Max            time.Duration
	RequestsPerSec float64
	Errors         int
}

func main() {
	reqCount := flag.Int("n", 1000, "total number of requests")
	concurrency := flag.Int("c", 10, "concurrency level")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))

	result, err := runBenchmark(logger, *reqCount, *concurrency)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(formatResult(result))
}

func runBenchmark(logger *slog.Logger, totalRequests, concurrency int) (*BenchmarkResult, error) {
	if totalRequests <= 0 {
		return nil, fmt.Errorf("request count must be > 0, got %d", totalRequests)
	}
	if concurrency <= 0 {
		return nil, fmt.Errorf("concurrency must be > 0, got %d", concurrency)
	}

	// 1. Start mock provider
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":      "bench-response",
			"object":  "chat.completion",
			"created": float64(time.Now().Unix()),
			"model":   "bench-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "benchmark reply"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockProvider.Close()

	// 2. Build gateway config pointing to mock provider
	cfg := config.Default()
	cfg.Server.Port = 0 // random port
	cfg.Logging.Level = "error"
	cfg.Auth.Enabled = false        // no auth in benchmark
	cfg.Server.MaxBodySizeKB = 1024 // 1MB max body for benchmark
	cfg.Providers = map[string]config.ProviderConfig{
		"bench_provider": {
			Type:    "openai-compatible",
			BaseURL: mockProvider.URL,
			Keys:    []config.ProviderKeyConfig{{ID: "bench", Value: "bench-key", Weight: 100}},
		},
	}
	cfg.Models = map[string]config.ModelConfig{
		"bench-model": {Routes: []config.ModelRoute{
			{Provider: "bench_provider", Model: "bench-model", Weight: 100},
		}},
	}

	// 3. Start gateway on a random port
	runtime := server.NewRuntime(cfg, logger, nil)
	gwListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	gwServer := &http.Server{Handler: runtime.Server.Handler}
	go func() {
		gwServer.Serve(gwListener)
	}()
	defer gwServer.Shutdown(context.Background())

	gwAddr := fmt.Sprintf("http://%s", gwListener.Addr())

	// 4. Run benchmark
	latencies := make([]time.Duration, totalRequests)
	var errors atomic.Int32
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	start := time.Now()
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			reqStart := time.Now()
			reqBody := strings.NewReader(`{"model":"bench-model","messages":[{"role":"user","content":"hello"}]}`)
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
				gwAddr+"/v1/chat/completions", reqBody)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer bench-key")
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			latencies[idx] = time.Since(reqStart)
			if err != nil {
				errors.Add(1)
				return
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
			if resp.StatusCode != http.StatusOK {
				errors.Add(1)
			}
		}(i)
	}
	wg.Wait()
	duration := time.Since(start)

	// 5. Compute results
	return computeResult(totalRequests, concurrency, duration, latencies, int(errors.Load())), nil
}

func computeResult(totalRequests, concurrency int, duration time.Duration, latencies []time.Duration, errors int) *BenchmarkResult {
	// Filter out zero latencies from error cases
	var valid []time.Duration
	for _, l := range latencies {
		if l > 0 {
			valid = append(valid, l)
		}
	}

	result := &BenchmarkResult{
		TotalRequests: totalRequests,
		Concurrency:   concurrency,
		Duration:      duration,
		Errors:        errors,
	}

	if len(valid) == 0 {
		return result
	}

	sort.Slice(valid, func(i, j int) bool { return valid[i] < valid[j] })

	result.Min = valid[0]
	result.Max = valid[len(valid)-1]
	result.P50 = percentile(valid, 0.50)
	result.P95 = percentile(valid, 0.95)
	result.P99 = percentile(valid, 0.99)

	if duration.Seconds() > 0 {
		result.RequestsPerSec = float64(totalRequests-errors) / duration.Seconds()
	}

	return result
}

// percentile returns the value at the given percentile from a sorted slice.
// Uses nearest-rank method.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// formatResult produces a human-readable benchmark report.
func formatResult(r *BenchmarkResult) string {
	return fmt.Sprintf(`OpenLimit Benchmark Results
═══════════════════════════
  Requests:      %d
  Concurrency:   %d
  Duration:      %v
  Errors:        %d
  Req/sec:       %.1f

  Latency:
    Min:  %v
    P50:  %v
    P95:  %v
    P99:  %v
    Max:  %v
`, r.TotalRequests, r.Concurrency, r.Duration.Round(time.Microsecond),
		r.Errors, r.RequestsPerSec,
		r.Min, r.P50, r.P95, r.P99, r.Max)
}
