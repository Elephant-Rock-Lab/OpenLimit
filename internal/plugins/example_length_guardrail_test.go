package plugins

import "testing"

// TEST-23-02-01: Init with default config (min=0, max=10000)
func TestLengthGuardrail_InitDefaults(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	if err := p.Init(map[string]any{}); err != nil {
		t.Fatalf("Init({}) error: %v", err)
	}
	if p.minLength != defaultMinLength {
		t.Errorf("minLength = %d, want %d", p.minLength, defaultMinLength)
	}
	if p.maxLength != defaultMaxLength {
		t.Errorf("maxLength = %d, want %d", p.maxLength, defaultMaxLength)
	}
}

// TEST-23-02-02: Init with custom min/max
func TestLengthGuardrail_InitCustom(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	if err := p.Init(map[string]any{
		"min_length": float64(10),
		"max_length": float64(5000),
	}); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if p.minLength != 10 {
		t.Errorf("minLength = %d, want 10", p.minLength)
	}
	if p.maxLength != 5000 {
		t.Errorf("maxLength = %d, want 5000", p.maxLength)
	}
}

// TEST-23-02-03: ProcessInput blocks short message when min_length=10
func TestLengthGuardrail_BlocksShortInput(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	_ = p.Init(map[string]any{"min_length": float64(10)})

	ctx := NewGuardrailContext("hi")
	result, err := p.ProcessInput(ctx)
	if err != nil {
		t.Fatalf("ProcessInput error: %v", err)
	}
	if !result.Blocked {
		t.Error("expected Blocked=true for short message")
	}
	if result.BlockReason == "" {
		t.Error("expected non-empty BlockReason")
	}
}

// TEST-23-02-04: ProcessInput passes valid message
func TestLengthGuardrail_PassesValidInput(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	_ = p.Init(map[string]any{
		"min_length": float64(5),
		"max_length": float64(100),
	})

	ctx := NewGuardrailContext("Hello, world!")
	result, err := p.ProcessInput(ctx)
	if err != nil {
		t.Fatalf("ProcessInput error: %v", err)
	}
	if result.Blocked {
		t.Errorf("expected Blocked=false for valid message, got Blocked=true: %s", result.BlockReason)
	}
}

// TEST-23-02-05: ProcessOutput blocks long message when max_length=50
func TestLengthGuardrail_BlocksLongOutput(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	_ = p.Init(map[string]any{"max_length": float64(50)})

	longMsg := ""
	for i := 0; i < 100; i++ {
		longMsg += "x"
	}

	ctx := NewGuardrailContext(longMsg)
	result, err := p.ProcessOutput(ctx)
	if err != nil {
		t.Fatalf("ProcessOutput error: %v", err)
	}
	if !result.Blocked {
		t.Error("expected Blocked=true for long message")
	}
}

// TEST-23-02-06: Init(nil) uses defaults — no panic
func TestLengthGuardrail_InitNil(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	if err := p.Init(nil); err != nil {
		t.Fatalf("Init(nil) error: %v", err)
	}
	if p.minLength != defaultMinLength {
		t.Errorf("minLength = %d, want %d", p.minLength, defaultMinLength)
	}
	if p.maxLength != defaultMaxLength {
		t.Errorf("maxLength = %d, want %d", p.maxLength, defaultMaxLength)
	}
}

// TEST-23-02-07: Init({}) uses defaults — no panic
func TestLengthGuardrail_InitEmpty(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	if err := p.Init(map[string]any{}); err != nil {
		t.Fatalf("Init({}) error: %v", err)
	}
	if p.minLength != defaultMinLength {
		t.Errorf("minLength = %d, want %d", p.minLength, defaultMinLength)
	}
}

// TEST-23-02-08: ProcessInput passes message at exactly min_length (boundary-inclusive)
func TestLengthGuardrail_PassesAtMinBoundary(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	_ = p.Init(map[string]any{"min_length": float64(10)})

	// Exactly 10 characters
	msg := "0123456789"
	if len(msg) != 10 {
		t.Fatalf("test setup error: message length = %d, want 10", len(msg))
	}

	ctx := NewGuardrailContext(msg)
	result, err := p.ProcessInput(ctx)
	if err != nil {
		t.Fatalf("ProcessInput error: %v", err)
	}
	if result.Blocked {
		t.Errorf("expected Blocked=false at exact min boundary, got: %s", result.BlockReason)
	}
}

// TEST-23-02-09: ProcessOutput passes message at exactly max_length (boundary-inclusive)
func TestLengthGuardrail_PassesAtMaxBoundary(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	_ = p.Init(map[string]any{"max_length": float64(50)})

	// Exactly 50 characters
	msg := ""
	for i := 0; i < 50; i++ {
		msg += "a"
	}

	ctx := NewGuardrailContext(msg)
	result, err := p.ProcessOutput(ctx)
	if err != nil {
		t.Fatalf("ProcessOutput error: %v", err)
	}
	if result.Blocked {
		t.Errorf("expected Blocked=false at exact max boundary, got: %s", result.BlockReason)
	}
}

// TEST-23-02-10: ProcessInput blocks empty message when min_length=1
func TestLengthGuardrail_BlocksEmptyMessage(t *testing.T) {
	p := &LengthGuardrailPlugin{}
	_ = p.Init(map[string]any{"min_length": float64(1)})

	ctx := NewGuardrailContext("")
	result, err := p.ProcessInput(ctx)
	if err != nil {
		t.Fatalf("ProcessInput error: %v", err)
	}
	if !result.Blocked {
		t.Error("expected Blocked=true for empty message when min_length=1")
	}
}
