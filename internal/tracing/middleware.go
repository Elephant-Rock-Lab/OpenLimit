package tracing

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware creates a root span for each request and injects traceparent
// into the response headers for trace correlation.
func (t *Tracer) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !t.enabled {
			next.ServeHTTP(w, r)
			return
		}

		spanName := r.Method + " " + r.URL.Path
		ctx, span := t.tracer.Start(r.Context(), spanName)
		defer span.End()

		r = r.WithContext(ctx)

		// Wrap response writer to capture status
		rec := &statusCapture{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		// Set attributes on the span
		span.SetAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.Path),
			attribute.Int("http.status_code", rec.status),
		)

		// Inject traceparent into response for client correlation
		spanCtx := span.SpanContext()
		if spanCtx.IsValid() {
			w.Header().Set("traceparent", formatTraceparent(spanCtx))
		}
	})
}

type statusCapture struct {
	http.ResponseWriter
	status int
}

func (s *statusCapture) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusCapture) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func formatTraceparent(sc trace.SpanContext) string {
	flags := "01"
	if !sc.IsSampled() {
		flags = "00"
	}
	return "00-" + sc.TraceID().String() + "-" + sc.SpanID().String() + "-" + flags
}

// StartChatSpan creates a chat_completions span with standard attributes.
func (t *Tracer) StartChatSpan(ctx context.Context, model string, stream bool) (context.Context, func()) {
	if !t.enabled {
		return ctx, func() {}
	}
	streamStr := "false"
	if stream {
		streamStr = "true"
	}
	ctx, span := t.tracer.Start(ctx, "chat_completions",
		trace.WithAttributes(
			attribute.String("model", model),
			attribute.String("stream", streamStr),
		),
	)
	endFn := func() { span.End() }
	return ctx, endFn
}

// StartProviderSpan creates a child span for a provider call.
func (t *Tracer) StartProviderSpan(ctx context.Context, provider, model string) (context.Context, func(), time.Time) {
	start := time.Now()
	if !t.enabled {
		return ctx, func() {}, start
	}
	ctx, span := t.tracer.Start(ctx, "provider_call",
		trace.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("model", model),
		),
	)
	endFn := func() { span.End() }
	return ctx, endFn, start
}

// SetSpanResult sets result attributes on the current span.
func SetSpanResult(ctx context.Context, provider, providerModel string, promptTokens, completionTokens int, costUSD float64, durationMS int64) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(
		attribute.String("provider", provider),
		attribute.String("provider_model", providerModel),
		attribute.Int("prompt_tokens", promptTokens),
		attribute.Int("completion_tokens", completionTokens),
		attribute.Int("duration_ms", int(durationMS)),
	)
	if costUSD > 0 {
		span.SetAttributes(attribute.Float64("cost_usd", costUSD))
	}
}
