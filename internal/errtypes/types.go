package errtypes

import "strconv"

// GatewayError is a structured error type for gateway responses. It carries
// enough information to produce an OpenAI-compatible error JSON with optional
// Details and Stage fields.
type GatewayError struct {
	StatusCode int
	Type       string
	Message    string
	Details    map[string]any
	Stage      string
}

// Error implements the error interface.
func (e *GatewayError) Error() string { return e.Message }

// ParseHeaderDetail attempts to parse a string as a float64 for structured
// details. Returns the float if parseable, otherwise the original string.
func ParseHeaderDetail(v string) any {
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}
	return v
}
