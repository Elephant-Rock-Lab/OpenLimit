package plugins

import "fmt"

// LengthGuardrailPlugin is an example GuardrailPlugin that checks input/output
// message length against configurable min/max bounds.
//
// This plugin demonstrates the full GuardrailPlugin interface including:
//   - Init with config-driven defaults
//   - ProcessInput for pre-provider validation
//   - ProcessOutput for post-provider validation
//   - Blocking via GuardrailContext.Blocked + BlockReason
//
// Configuration:
//
//	plugins:
//	  - name: example-length-guardrail
//	    type: guardrail
//	    config:
//	      min_length: 10
//	      max_length: 5000
//
// Guardrail pipeline reference:
//
//	guardrails:
//	  input:
//	    - type: plugin
//	      config:
//	        name: example-length-guardrail
type LengthGuardrailPlugin struct {
	minLength int
	maxLength int
}

const (
	defaultMinLength = 0
	defaultMaxLength = 10000
)

func (p *LengthGuardrailPlugin) Name() string { return "example-length-guardrail" }
func (p *LengthGuardrailPlugin) Type() string { return "guardrail" }

func (p *LengthGuardrailPlugin) Init(config map[string]any) error {
	p.minLength = defaultMinLength
	p.maxLength = defaultMaxLength

	if config == nil {
		return nil
	}

	if v, ok := config["min_length"]; ok {
		if f, ok := v.(float64); ok {
			p.minLength = int(f)
		}
	}

	if v, ok := config["max_length"]; ok {
		if f, ok := v.(float64); ok {
			p.maxLength = int(f)
		}
	}

	return nil
}

func (p *LengthGuardrailPlugin) ProcessInput(ctx GuardrailContext) (GuardrailContext, error) {
	return p.checkLength(ctx, "input"), nil
}

func (p *LengthGuardrailPlugin) ProcessOutput(ctx GuardrailContext) (GuardrailContext, error) {
	return p.checkLength(ctx, "output"), nil
}

// checkLength validates message length against bounds and blocks if out of range.
// Messages at exactly min_length or exactly max_length are considered valid (boundary-inclusive).
func (p *LengthGuardrailPlugin) checkLength(ctx GuardrailContext, direction string) GuardrailContext {
	n := len(ctx.Message)

	if n < p.minLength {
		ctx.Blocked = true
		ctx.BlockReason = direction + " message too short: " + fmt.Sprintf("%d < %d", n, p.minLength)
		return ctx
	}

	if p.maxLength > 0 && n > p.maxLength {
		ctx.Blocked = true
		ctx.BlockReason = direction + " message too long: " + fmt.Sprintf("%d > %d", n, p.maxLength)
		return ctx
	}

	return ctx
}
