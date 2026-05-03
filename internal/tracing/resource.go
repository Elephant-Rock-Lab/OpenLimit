package tracing

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"openlimit/pkg/version"
)

var buildVersion = "dev"

func init() {
	buildVersion = version.Version
}

func newResource(serviceName string) *resource.Resource {
	r, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(buildVersion),
			attribute.String("gateway", "openlimit"),
		),
	)
	return r
}

func toOTelAttrs(attrs []Attr) []attribute.KeyValue {
	result := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		k := attribute.Key(a.Key)
		switch v := a.Value.(type) {
		case string:
			result = append(result, k.String(v))
		case int:
			result = append(result, k.Int(v))
		case int64:
			result = append(result, k.Int64(v))
		case float64:
			result = append(result, k.Float64(v))
		case bool:
			result = append(result, k.Bool(v))
		}
	}
	return result
}
