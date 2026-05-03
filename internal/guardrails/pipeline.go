package guardrails

import (
	"context"
	"fmt"

	"openlimit/internal/audit"
)

// Message represents a chat message for guardrail inspection.
type Message struct {
	Role    string
	Content string
}

// ResultAction indicates what a guardrail stage decided.
type ResultAction int

const (
	Pass   ResultAction = iota // Content is clean, proceed
	Block                      // Content is prohibited, reject request
	Redact                     // Content was cleaned, use RedactedMessages
)

// Result is the outcome of a guardrail stage check.
type Result struct {
	Action           ResultAction
	Message          string    // Human-readable block reason (shown to caller)
	StageName        string    // Name of the stage that produced this result
	RedactedMessages []Message // Cleaned messages (when Action == Redact)
}

// Stage is a single guardrail check in the pipeline.
type Stage interface {
	Name() string
	CheckInput(ctx context.Context, messages []Message) (Result, error)
	CheckOutput(ctx context.Context, content string) (Result, error)
}

// Pipeline runs an ordered chain of guardrail stages.
// It short-circuits on the first Block.
// Redact results accumulate: each subsequent stage sees the redacted output from prior stages.
type Pipeline struct {
	inputStages  []Stage
	outputStages []Stage
	audit        *audit.Logger
}

// NewPipeline creates a guardrail pipeline with separate input and output stages.
func NewPipeline(inputStages, outputStages []Stage) *Pipeline {
	return &Pipeline{
		inputStages:  inputStages,
		outputStages: outputStages,
	}
}

// CheckInput runs all input stages against the request messages.
// Returns the final result. If any stage redacts, subsequent stages see the redacted version.
// Short-circuits on first Block.
func (p *Pipeline) CheckInput(ctx context.Context, messages []Message) (Result, error) {
	if len(p.inputStages) == 0 {
		return Result{Action: Pass}, nil
	}

	current := messages
	for _, stage := range p.inputStages {
		result, err := stage.CheckInput(ctx, current)
		if err != nil {
			return Result{}, fmt.Errorf("guardrail stage %q failed: %w", stage.Name(), err)
		}
		result.StageName = stage.Name()

		switch result.Action {
		case Block:
			p.emitAudit("input", result)
			return result, nil
		case Redact:
			current = result.RedactedMessages
		}
	}

	// If any stage redacted, return the final redacted messages
	if len(current) > 0 && messagesDiffer(messages, current) {
		return Result{
			Action:           Redact,
			RedactedMessages: current,
			StageName:        "pipeline",
		}, nil
	}

	return Result{Action: Pass}, nil
}

// CheckOutput runs all output stages against the provider response content.
// Returns the final result. Short-circuits on first Block.
func (p *Pipeline) CheckOutput(ctx context.Context, content string) (Result, error) {
	if len(p.outputStages) == 0 {
		return Result{Action: Pass}, nil
	}

	current := content
	for _, stage := range p.outputStages {
		result, err := stage.CheckOutput(ctx, current)
		if err != nil {
			return Result{}, fmt.Errorf("guardrail stage %q failed: %w", stage.Name(), err)
		}
		result.StageName = stage.Name()

		switch result.Action {
		case Block:
			p.emitAudit("output", result)
			return result, nil
		case Redact:
			current = result.Message // For output, redacted content is in Message field
		}
	}

	if current != content {
		return Result{
			Action:    Redact,
			Message:   current,
			StageName: "pipeline",
		}, nil
	}

	return Result{Action: Pass}, nil
}

// SetAuditLogger sets the audit logger for guardrail block events.
// If nil, no audit events are emitted.
func (p *Pipeline) SetAuditLogger(l *audit.Logger) {
	p.audit = l
}

// HasInputStages returns true if any input stages are configured.
func (p *Pipeline) HasInputStages() bool {
	return len(p.inputStages) > 0
}

// HasOutputStages returns true if any output stages are configured.
func (p *Pipeline) HasOutputStages() bool {
	return len(p.outputStages) > 0
}

// InputStages returns the list of input stage names.
func (p *Pipeline) InputStages() []string {
	names := make([]string, len(p.inputStages))
	for i, s := range p.inputStages {
		names[i] = s.Name()
	}
	return names
}

// OutputStages returns the list of output stage names.
func (p *Pipeline) OutputStages() []string {
	names := make([]string, len(p.outputStages))
	for i, s := range p.outputStages {
		names[i] = s.Name()
	}
	return names
}

// messagesDiffer returns true if two message slices have different content.
func messagesDiffer(a, b []Message) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content {
			return true
		}
	}
	return false
}

func (p *Pipeline) emitAudit(direction string, result Result) {
	if p.audit == nil {
		return
	}
	p.audit.Record(audit.Event{
		EventType: audit.EventGuardrailBlock,
		Actor:     "system",
		Action:    "block",
		Resource:  result.StageName,
		Outcome:   "blocked",
		Metadata:  map[string]any{"direction": direction, "reason": result.Message},
	})
}
