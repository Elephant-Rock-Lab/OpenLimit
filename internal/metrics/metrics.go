package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

const namespace = "gateway"

// Collector holds all Prometheus metrics for the gateway.
// When metrics are disabled, all Record* methods are no-ops.
type Collector struct {
	enabled bool

	requestsTotal        *prometheus.CounterVec
	requestDuration      *prometheus.HistogramVec
	providerCallDur      *prometheus.HistogramVec
	providerErrors       *prometheus.CounterVec
	tokensTotal          *prometheus.CounterVec
	costDollarsTotal     *prometheus.CounterVec
	cacheHits            *prometheus.CounterVec
	cacheMisses          *prometheus.CounterVec
	rateLimitRejects     *prometheus.CounterVec
	budgetRejects        *prometheus.CounterVec
	activeRequests       prometheus.Gauge
	retriesTotal         *prometheus.CounterVec
	fallbacksTotal       *prometheus.CounterVec
	guardrailBlocks      *prometheus.CounterVec
	guardrailRedactions  *prometheus.CounterVec
	guardrailDuration    *prometheus.HistogramVec
	mcpServerConnections *prometheus.GaugeVec
	mcpToolCallsTotal    *prometheus.CounterVec
	mcpToolCallDuration  *prometheus.HistogramVec
	mcpMaxRoundsExceeded *prometheus.CounterVec

	// Phase 6A metrics
	redisHealthy          prometheus.Gauge
	circuitBreakerRejects *prometheus.CounterVec

	// Phase 6B metrics
	auditEvents   *prometheus.CounterVec
	kmsOperations *prometheus.CounterVec

	// Phase 6C metrics
	oidcAuthTotal   *prometheus.CounterVec
	rbacChecksTotal *prometheus.CounterVec

	// Phase 6E metrics
	providerRegionDuration *prometheus.HistogramVec
	residencyFilterTotal   *prometheus.CounterVec

	// Phase 7C metrics
	gatewayErrorsTotal *prometheus.CounterVec
}

var latencyBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

// NewCollector creates and registers all gateway metrics.
// If enabled is false, all methods become no-ops and /metrics returns 404.
func NewCollector(enabled bool) *Collector {
	c := &Collector{enabled: enabled}
	if !enabled {
		return c
	}

	c.requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "requests_total",
		Help:      "Total requests processed",
	}, []string{"model", "provider", "status", "stream"})

	c.requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "request_duration_seconds",
		Help:      "End-to-end request latency in seconds",
		Buckets:   latencyBuckets,
	}, []string{"model", "provider", "stream"})

	c.providerCallDur = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "provider_call_duration_seconds",
		Help:      "Per-provider call latency in seconds",
		Buckets:   latencyBuckets,
	}, []string{"provider", "model"})

	c.providerErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "provider_errors_total",
		Help:      "Provider call failures",
	}, []string{"provider", "model", "error_type"})

	c.tokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tokens_total",
		Help:      "Token usage by direction (prompt/completion)",
	}, []string{"model", "provider", "direction"})

	c.costDollarsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "cost_dollars_total",
		Help:      "Cumulative cost in USD",
	}, []string{"model", "provider"})

	c.cacheHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "cache_hits_total",
		Help:      "Cache hit count",
	}, []string{"model"})

	c.cacheMisses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "cache_misses_total",
		Help:      "Cache miss count",
	}, []string{"model"})

	c.rateLimitRejects = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "rate_limit_rejections_total",
		Help:      "Rate limit rejections",
	}, []string{"key_prefix", "project_id"})

	c.budgetRejects = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "budget_rejections_total",
		Help:      "Budget limit rejections",
	}, []string{"key_prefix", "project_id"})

	c.activeRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "active_requests",
		Help:      "Currently in-flight requests",
	})

	c.retriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "retries_total",
		Help:      "Retry attempts",
	}, []string{"provider", "model"})

	c.fallbacksTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "fallbacks_total",
		Help:      "Fallback invocations",
	}, []string{"from_provider", "to_provider"})

	c.guardrailBlocks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "guardrail_blocks_total",
		Help:      "Guardrail blocks",
	}, []string{"stage", "direction", "model"})

	c.guardrailRedactions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "guardrail_redactions_total",
		Help:      "Content redactions by guardrails",
	}, []string{"stage", "direction", "model"})

	c.guardrailDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "guardrail_duration_seconds",
		Help:      "Guardrail check latency",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"stage", "direction"})

	c.mcpServerConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "mcp_server_connections",
		Help:      "MCP server connection status",
	}, []string{"server", "status"})

	c.mcpToolCallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "mcp_tool_calls_total",
		Help:      "MCP tool invocations",
	}, []string{"server", "tool", "status"})

	c.mcpToolCallDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "mcp_tool_call_duration_seconds",
		Help:      "MCP tool execution latency",
		Buckets:   latencyBuckets,
	}, []string{"server", "tool"})

	c.mcpMaxRoundsExceeded = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "mcp_max_rounds_exceeded_total",
		Help:      "Times the max tool execution rounds limit was reached",
	}, []string{"model"})

	c.redisHealthy = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "redis_healthy",
		Help:      "Whether Redis is reachable (1=healthy, 0=degraded)",
	})

	c.circuitBreakerRejects = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "circuit_breaker_rejections_total",
		Help:      "Requests rejected by circuit breaker",
	}, []string{"provider", "model"})

	c.auditEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "audit_events_total",
		Help:      "Audit events recorded",
	}, []string{"event_type", "outcome"})

	c.kmsOperations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "kms_operations_total",
		Help:      "KMS operations performed",
	}, []string{"operation", "status"})

	c.oidcAuthTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "oidc_auth_total",
		Help:      "OIDC authentication attempts",
	}, []string{"status"})

	c.rbacChecksTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "rbac_checks_total",
		Help:      "RBAC permission checks",
	}, []string{"role", "action", "result"})

	c.providerRegionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "provider_region_duration_seconds",
		Help:      "Per-region provider call latency in seconds",
		Buckets:   latencyBuckets,
	}, []string{"provider", "model", "region"})

	c.residencyFilterTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "residency_filter_total",
		Help:      "Data residency filter decisions",
	}, []string{"result"})

	c.gatewayErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "errors_total",
		Help:      "Total gateway errors by type and API source",
	}, []string{"type", "source"})

	prometheus.MustRegister(
		c.requestsTotal,
		c.requestDuration,
		c.providerCallDur,
		c.providerErrors,
		c.tokensTotal,
		c.costDollarsTotal,
		c.cacheHits,
		c.cacheMisses,
		c.rateLimitRejects,
		c.budgetRejects,
		c.activeRequests,
		c.retriesTotal,
		c.fallbacksTotal,
		c.guardrailBlocks,
		c.guardrailRedactions,
		c.guardrailDuration,
		c.mcpServerConnections,
		c.mcpToolCallsTotal,
		c.mcpToolCallDuration,
		c.mcpMaxRoundsExceeded,
		c.redisHealthy,
		c.circuitBreakerRejects,
		c.auditEvents,
		c.kmsOperations,
		c.oidcAuthTotal,
		c.rbacChecksTotal,
		c.providerRegionDuration,
		c.residencyFilterTotal,
		c.gatewayErrorsTotal,
	)

	return c
}

// MetricsHandler returns an http.Handler for the Prometheus metrics endpoint.
// If metrics are disabled, returns a handler that responds 404.
func (c *Collector) MetricsHandler() http.Handler {
	if !c.enabled {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
	}
	return promhttp.Handler()
}

// Enabled returns whether metrics collection is active.
func (c *Collector) Enabled() bool { return c.enabled }

// --- Record methods (no-ops when disabled) ---

func (c *Collector) RecordRequest(model, provider, status string, stream bool, duration time.Duration) {
	if !c.enabled {
		return
	}
	streamStr := "false"
	if stream {
		streamStr = "true"
	}
	c.requestsTotal.WithLabelValues(model, provider, status, streamStr).Inc()
	c.requestDuration.WithLabelValues(model, provider, streamStr).Observe(duration.Seconds())
}

func (c *Collector) RecordProviderCall(provider, model string, duration time.Duration, errType string) {
	if !c.enabled {
		return
	}
	c.providerCallDur.WithLabelValues(provider, model).Observe(duration.Seconds())
	if errType != "" {
		c.providerErrors.WithLabelValues(provider, model, errType).Inc()
	}
}

func (c *Collector) RecordCacheHit(model string) {
	if !c.enabled {
		return
	}
	c.cacheHits.WithLabelValues(model).Inc()
}

func (c *Collector) RecordCacheMiss(model string) {
	if !c.enabled {
		return
	}
	c.cacheMisses.WithLabelValues(model).Inc()
}

func (c *Collector) RecordTokens(model, provider string, promptTokens, completionTokens int) {
	if !c.enabled {
		return
	}
	c.tokensTotal.WithLabelValues(model, provider, "prompt").Add(float64(promptTokens))
	c.tokensTotal.WithLabelValues(model, provider, "completion").Add(float64(completionTokens))
}

func (c *Collector) RecordCost(model, provider string, costUSD float64) {
	if !c.enabled {
		return
	}
	c.costDollarsTotal.WithLabelValues(model, provider).Add(costUSD)
}

func (c *Collector) RecordRateLimitRejection(keyPrefix, projectID string) {
	if !c.enabled {
		return
	}
	c.rateLimitRejects.WithLabelValues(keyPrefix, projectID).Inc()
}

func (c *Collector) RecordBudgetRejection(keyPrefix, projectID string) {
	if !c.enabled {
		return
	}
	c.budgetRejects.WithLabelValues(keyPrefix, projectID).Inc()
}

func (c *Collector) RecordRetry(provider, model string) {
	if !c.enabled {
		return
	}
	c.retriesTotal.WithLabelValues(provider, model).Inc()
}

func (c *Collector) RecordFallback(fromProvider, toProvider string) {
	if !c.enabled {
		return
	}
	c.fallbacksTotal.WithLabelValues(fromProvider, toProvider).Inc()
}

func (c *Collector) ActiveRequestsInc() {
	if !c.enabled {
		return
	}
	c.activeRequests.Inc()
}

func (c *Collector) ActiveRequestsDec() {
	if !c.enabled {
		return
	}
	c.activeRequests.Dec()
}

func (c *Collector) RecordGuardrailBlock(stage, direction, model string) {
	if !c.enabled {
		return
	}
	c.guardrailBlocks.WithLabelValues(stage, direction, model).Inc()
}

func (c *Collector) RecordGuardrailRedaction(stage, direction, model string) {
	if !c.enabled {
		return
	}
	c.guardrailRedactions.WithLabelValues(stage, direction, model).Inc()
}

func (c *Collector) RecordGuardrailDuration(stage, direction string, duration time.Duration) {
	if !c.enabled {
		return
	}
	c.guardrailDuration.WithLabelValues(stage, direction).Observe(duration.Seconds())
}

func (c *Collector) RecordMCPServerConnection(server, status string) {
	if !c.enabled {
		return
	}
	c.mcpServerConnections.WithLabelValues(server, status).Set(1)
}

func (c *Collector) RecordMCPToolCall(server, tool, status string, duration time.Duration) {
	if !c.enabled {
		return
	}
	c.mcpToolCallsTotal.WithLabelValues(server, tool, status).Inc()
	c.mcpToolCallDuration.WithLabelValues(server, tool).Observe(duration.Seconds())
}

func (c *Collector) RecordMCPMaxRoundsExceeded(model string) {
	if !c.enabled {
		return
	}
	c.mcpMaxRoundsExceeded.WithLabelValues(model).Inc()
}

// SetRedisHealthy updates the Redis health gauge.
func (c *Collector) SetRedisHealthy(healthy bool) {
	if !c.enabled {
		return
	}
	val := float64(0)
	if healthy {
		val = 1
	}
	c.redisHealthy.Set(val)
}

// RecordCircuitBreakerRejection records a rejected request due to open circuit breaker.
func (c *Collector) RecordCircuitBreakerRejection(provider, model string) {
	if !c.enabled {
		return
	}
	c.circuitBreakerRejects.WithLabelValues(provider, model).Inc()
}

func (c *Collector) RecordAuditEvent(eventType, outcome string) {
	if !c.enabled {
		return
	}
	c.auditEvents.WithLabelValues(eventType, outcome).Inc()
}

func (c *Collector) RecordKMSOperation(operation, status string) {
	if !c.enabled {
		return
	}
	c.kmsOperations.WithLabelValues(operation, status).Inc()
}

func (c *Collector) RecordOIDCAuth(status string) {
	if !c.enabled {
		return
	}
	c.oidcAuthTotal.WithLabelValues(status).Inc()
}

func (c *Collector) RecordRBACCheck(role, action, result string) {
	if !c.enabled {
		return
	}
	c.rbacChecksTotal.WithLabelValues(role, action, result).Inc()
}

// RecordProviderRegionCall records per-region provider call latency.
// When region is empty, it is recorded as "default".
func (c *Collector) RecordProviderRegionCall(provider, model, region string, duration time.Duration) {
	if !c.enabled {
		return
	}
	if region == "" {
		region = "default"
	}
	c.providerRegionDuration.WithLabelValues(provider, model, region).Observe(duration.Seconds())
}

// RecordGatewayError increments the gateway error counter by type and source.
// Source values: "direct", "admin", "mcp", "a2a".
// Type values: error types from writeError/writeAdminError (rate_limit_exceeded,
// budget_exceeded, guardrail_block, invalid_request, etc.).
func (c *Collector) RecordGatewayError(errorType, source string) {
	if !c.enabled {
		return
	}
	c.gatewayErrorsTotal.WithLabelValues(errorType, source).Inc()
}

// RecordResidencyFilter records a data residency filter decision.
func (c *Collector) RecordResidencyFilter(result string) {
	if !c.enabled {
		return
	}
	c.residencyFilterTotal.WithLabelValues(result).Inc()
}

// RegionLatency returns the estimated p50 latency for a provider+model+region.
// Used by the router's latency-based strategy. Returns (p50, false) when unavailable.
func (c *Collector) RegionLatency(provider, model, region string) (time.Duration, bool) {
	if !c.enabled {
		return 0, false
	}
	if region == "" {
		region = "default"
	}
	// Collect the histogram metric and estimate p50 from its buckets.
	ch := make(chan prometheus.Metric)
	go func() {
		c.providerRegionDuration.Collect(ch)
		close(ch)
	}()

	for metric := range ch {
		var dm dto.Metric
		if err := metric.Write(&dm); err != nil {
			continue
		}
		// Check if this metric matches our labels
		h := dm.GetHistogram()
		if h == nil {
			continue
		}
		// Check label values match
		labelMatch := false
		matchCount := 0
		for _, lp := range dm.GetLabel() {
			switch lp.GetName() {
			case "provider":
				if lp.GetValue() == provider {
					matchCount++
				}
			case "model":
				if lp.GetValue() == model {
					matchCount++
				}
			case "region":
				if lp.GetValue() == region {
					matchCount++
				}
			}
		}
		if matchCount == 3 {
			labelMatch = true
		}
		if !labelMatch {
			continue
		}

		count := h.GetSampleCount()
		if count < 10 {
			return 0, false
		}
		target := count / 2
		for _, b := range h.GetBucket() {
			if b.GetCumulativeCount() >= target {
				return time.Duration(b.GetUpperBound() * float64(time.Second)), true
			}
		}
		// Fallback: use sum/count
		return time.Duration(h.GetSampleSum() / float64(count) * float64(time.Second)), true
	}
	return 0, false
}

// A2A metrics.
var (
	a2aTasksCreated    prometheus.Counter
	a2aTaskCompletions *prometheus.CounterVec
	a2aTaskDuration    *prometheus.HistogramVec
)

func init() {
	a2aTasksCreated = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "a2a_tasks_created_total",
		Help:      "Total number of A2A tasks created.",
	})
	a2aTaskCompletions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "a2a_task_completions_total",
		Help:      "Total number of A2A tasks completed by final status.",
	}, []string{"status"})
	a2aTaskDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gateway",
		Name:      "a2a_task_duration_seconds",
		Help:      "Duration of A2A task execution in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"status", "model"})
	prometheus.MustRegister(a2aTasksCreated, a2aTaskCompletions, a2aTaskDuration)
}

// RecordA2ATaskCreated increments the task creation counter.
func (c *Collector) RecordA2ATaskCreated() {
	if c == nil || !c.enabled {
		return
	}
	a2aTasksCreated.Inc()
}

// RecordA2ATaskCompletion records a task completion with status and duration.
func (c *Collector) RecordA2ATaskCompletion(status, model string, duration time.Duration) {
	if c == nil || !c.enabled {
		return
	}
	a2aTaskCompletions.WithLabelValues(status).Inc()
	a2aTaskDuration.WithLabelValues(status, model).Observe(duration.Seconds())
}
