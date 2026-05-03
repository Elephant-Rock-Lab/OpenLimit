package tracing

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Tracer provides OpenTelemetry tracing for the gateway.
// When disabled, all span creation is no-op.
type Tracer struct {
	enabled    bool
	tp         *sdktrace.TracerProvider
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

// NewTracer initializes the OpenTelemetry tracing pipeline.
// If enabled is false, returns a no-op tracer.
func NewTracer(enabled bool, endpoint, serviceName string, sampleRate float64, logger *slog.Logger) (*Tracer, error) {
	if !enabled {
		return &Tracer{enabled: false}, nil
	}

	if serviceName == "" {
		serviceName = "openlimit"
	}
	if endpoint == "" {
		endpoint = "localhost:4317"
	}
	if sampleRate <= 0 {
		sampleRate = 0.1
	}

	ctx := context.Background()

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP exporter: %w", err)
	}

	sampler := sdktrace.TraceIDRatioBased(sampleRate)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(newResource(serviceName)),
	)

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagator)

	logger.Info("tracing enabled",
		"endpoint", endpoint,
		"service_name", serviceName,
		"sample_rate", sampleRate,
	)

	return &Tracer{
		enabled:    true,
		tp:         tp,
		tracer:     tp.Tracer("openlimit"),
		propagator: propagator,
	}, nil
}

// Shutdown gracefully shuts down the trace provider.
func (t *Tracer) Shutdown(ctx context.Context) error {
	if !t.enabled || t.tp == nil {
		return nil
	}
	return t.tp.Shutdown(ctx)
}

// Enabled returns whether tracing is active.
func (t *Tracer) Enabled() bool { return t.enabled }

// StartSpan starts a new span. Returns context with the span and an end function.
// When disabled, returns the original context and a no-op function.
func (t *Tracer) StartSpan(ctx context.Context, name string, attrs ...Attr) (context.Context, func()) {
	if !t.enabled {
		return ctx, func() {}
	}
	opts := trace.WithAttributes(toOTelAttrs(attrs)...)
	ctx, span := t.tracer.Start(ctx, name, opts)
	endFn := func() { span.End() }
	return ctx, endFn
}

// Attr is a key-value attribute for spans.
type Attr struct {
	Key   string
	Value any
}

// StringAttr creates a string attribute.
func StringAttr(key, value string) Attr {
	return Attr{Key: key, Value: value}
}

// IntAttr creates an int attribute.
func IntAttr(key string, value int) Attr {
	return Attr{Key: key, Value: value}
}

// FloatAttr creates a float64 attribute.
func FloatAttr(key string, value float64) Attr {
	return Attr{Key: key, Value: value}
}

// BoolAttr creates a bool attribute.
func BoolAttr(key string, value bool) Attr {
	return Attr{Key: key, Value: value}
}

// SpanFromContext returns the current span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddSpanEvents adds events to the current span.
func AddSpanEvent(ctx context.Context, name string, attrs ...Attr) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.AddEvent(name, trace.WithAttributes(toOTelAttrs(attrs)...))
	}
}

// RecordSpanError records an error on the current span.
func RecordSpanError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.RecordError(err)
	}
}
