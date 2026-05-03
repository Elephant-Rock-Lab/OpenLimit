package guardrails

import (
	"context"
	"testing"
)

// mockStage is a test guardrail stage with configurable behavior.
type mockStage struct {
	name        string
	inputResp   Result
	outputResp  Result
	inputErr    error
	outputErr   error
	inputCalls  int
	outputCalls int
}

func (m *mockStage) Name() string { return m.name }

func (m *mockStage) CheckInput(_ context.Context, messages []Message) (Result, error) {
	m.inputCalls++
	if m.inputErr != nil {
		return Result{}, m.inputErr
	}
	// For redact, return modified messages if RedactedMessages is set
	if m.inputResp.Action == Redact && m.inputResp.RedactedMessages != nil {
		return m.inputResp, nil
	}
	// Default redact behavior: replace content with "[REDACTED]"
	if m.inputResp.Action == Redact {
		redacted := make([]Message, len(messages))
		for i, msg := range messages {
			redacted[i] = Message{Role: msg.Role, Content: "[REDACTED]"}
		}
		return Result{
			Action:           Redact,
			StageName:        m.name,
			RedactedMessages: redacted,
		}, nil
	}
	return m.inputResp, nil
}

func (m *mockStage) CheckOutput(_ context.Context, content string) (Result, error) {
	m.outputCalls++
	if m.outputErr != nil {
		return Result{}, m.outputErr
	}
	if m.outputResp.Action == Redact {
		return Result{
			Action:    Redact,
			Message:   "[REDACTED]",
			StageName: m.name,
		}, nil
	}
	return m.outputResp, nil
}

func TestPipeline_EmptyInputStages(t *testing.T) {
	p := NewPipeline(nil, nil)
	msgs := []Message{{Role: "user", Content: "hello"}}

	result, err := p.CheckInput(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Pass {
		t.Errorf("expected Pass, got %v", result.Action)
	}
}

func TestPipeline_EmptyOutputStages(t *testing.T) {
	p := NewPipeline(nil, nil)

	result, err := p.CheckOutput(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Pass {
		t.Errorf("expected Pass, got %v", result.Action)
	}
}

func TestPipeline_SingleBlockStage(t *testing.T) {
	stage := &mockStage{
		name:      "blocker",
		inputResp: Result{Action: Block, Message: "blocked content"},
	}
	p := NewPipeline([]Stage{stage}, nil)

	msgs := []Message{{Role: "user", Content: "bad stuff"}}
	result, err := p.CheckInput(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Block {
		t.Errorf("expected Block, got %v", result.Action)
	}
	if result.Message != "blocked content" {
		t.Errorf("expected 'blocked content', got %q", result.Message)
	}
	if result.StageName != "blocker" {
		t.Errorf("expected stage name 'blocker', got %q", result.StageName)
	}
}

func TestPipeline_ShortCircuitOnBlock(t *testing.T) {
	blocker := &mockStage{
		name:      "blocker",
		inputResp: Result{Action: Block, Message: "blocked"},
	}
	passThrough := &mockStage{
		name:      "passthrough",
		inputResp: Result{Action: Pass},
	}
	p := NewPipeline([]Stage{blocker, passThrough}, nil)

	msgs := []Message{{Role: "user", Content: "test"}}
	result, err := p.CheckInput(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Block {
		t.Errorf("expected Block, got %v", result.Action)
	}
	if passThrough.inputCalls != 0 {
		t.Error("second stage should not have been called after block")
	}
}

func TestPipeline_RedactThenPass(t *testing.T) {
	redacter := &mockStage{
		name:       "redacter",
		inputResp:  Result{Action: Redact},
		inputCalls: 0,
	}
	observer := &mockStage{
		name:      "observer",
		inputResp: Result{Action: Pass},
	}
	p := NewPipeline([]Stage{redacter, observer}, nil)

	msgs := []Message{{Role: "user", Content: "my SSN is 123-45-6789"}}
	result, err := p.CheckInput(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Redact {
		t.Errorf("expected Redact, got %v", result.Action)
	}
	// Verify second stage was called
	if observer.inputCalls != 1 {
		t.Error("observer stage should have been called")
	}
	// Verify the final redacted messages
	if len(result.RedactedMessages) != 1 {
		t.Fatalf("expected 1 redacted message, got %d", len(result.RedactedMessages))
	}
	if result.RedactedMessages[0].Content != "[REDACTED]" {
		t.Errorf("expected redacted content, got %q", result.RedactedMessages[0].Content)
	}
}

func TestPipeline_OutputBlock(t *testing.T) {
	stage := &mockStage{
		name:       "output_blocker",
		outputResp: Result{Action: Block, Message: "inappropriate output"},
	}
	p := NewPipeline(nil, []Stage{stage})

	result, err := p.CheckOutput(context.Background(), "some output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Block {
		t.Errorf("expected Block, got %v", result.Action)
	}
	if result.Message != "inappropriate output" {
		t.Errorf("expected 'inappropriate output', got %q", result.Message)
	}
}

func TestPipeline_OutputRedact(t *testing.T) {
	stage := &mockStage{
		name:       "output_redacter",
		outputResp: Result{Action: Redact},
	}
	p := NewPipeline(nil, []Stage{stage})

	result, err := p.CheckOutput(context.Background(), "secret data here")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Redact {
		t.Errorf("expected Redact, got %v", result.Action)
	}
	if result.Message != "[REDACTED]" {
		t.Errorf("expected '[REDACTED]', got %q", result.Message)
	}
}

func TestPipeline_StageError(t *testing.T) {
	stage := &mockStage{
		name:     "failing",
		inputErr: context.DeadlineExceeded,
	}
	p := NewPipeline([]Stage{stage}, nil)

	_, err := p.CheckInput(context.Background(), []Message{{Role: "user", Content: "test"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPipeline_HasStages(t *testing.T) {
	empty := NewPipeline(nil, nil)
	if empty.HasInputStages() {
		t.Error("empty pipeline should not have input stages")
	}
	if empty.HasOutputStages() {
		t.Error("empty pipeline should not have output stages")
	}

	withInput := NewPipeline([]Stage{&mockStage{name: "s1", inputResp: Result{Action: Pass}}}, nil)
	if !withInput.HasInputStages() {
		t.Error("pipeline should have input stages")
	}
	if withInput.HasOutputStages() {
		t.Error("pipeline should not have output stages")
	}
}

func TestPipeline_StageNames(t *testing.T) {
	s1 := &mockStage{name: "pii", inputResp: Result{Action: Pass}}
	s2 := &mockStage{name: "keyword", inputResp: Result{Action: Pass}}
	p := NewPipeline([]Stage{s1, s2}, nil)

	names := p.InputStages()
	if len(names) != 2 || names[0] != "pii" || names[1] != "keyword" {
		t.Errorf("expected [pii, keyword], got %v", names)
	}
}

func TestPipeline_MultipleRedact(t *testing.T) {
	// Two stages that both redact — second should see first's output
	s1 := &mockStage{
		name:      "first",
		inputResp: Result{Action: Redact},
	}
	s2 := &mockStage{
		name:      "second",
		inputResp: Result{Action: Pass},
	}
	p := NewPipeline([]Stage{s1, s2}, nil)

	msgs := []Message{{Role: "user", Content: "original"}}
	result, err := p.CheckInput(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Pipeline should return Redact since content was modified
	if result.Action != Redact {
		t.Errorf("expected Redact, got %v", result.Action)
	}
}
