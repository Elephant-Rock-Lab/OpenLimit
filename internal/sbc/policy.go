package sbc

import "fmt"

// PolicyMode controls how aggressively SBC prunes tools.
type PolicyMode string

const (
	// ModeOff means no pruning — all tools are sent (default).
	ModeOff PolicyMode = "off"
	// ModeHybrid ranks tools and keeps top-K based on pressure, but
	// preserves tools with high relevance scores.
	ModeHybrid PolicyMode = "hybrid"
	// ModeEvict aggressively prunes low-relevance tools under pressure.
	// Still respects MinTools floor.
	ModeEvict PolicyMode = "evict"
)

// ParsePolicyMode parses a policy mode string, returning ModeOff for unknown values.
func ParsePolicyMode(s string) PolicyMode {
	switch s {
	case "hybrid":
		return ModeHybrid
	case "evict":
		return ModeEvict
	case "off", "":
		return ModeOff
	default:
		return ModeOff
	}
}

// RuntimePolicy controls SBC behavior per-request.
type RuntimePolicy struct {
	Enabled  bool       // master switch (mirrors config sbc.enabled)
	Mode     PolicyMode // off, hybrid, or evict
	MinTools int        // never prune below this count (default 8)
}

// DefaultRuntimePolicy returns the default policy: off with MinTools=8.
func DefaultRuntimePolicy() RuntimePolicy {
	return RuntimePolicy{
		Enabled:  false,
		Mode:     ModeOff,
		MinTools: 8,
	}
}

// Validate checks the policy for consistency.
func (p RuntimePolicy) Validate() error {
	if p.MinTools < 0 {
		return fmt.Errorf("sbc: MinTools must be >= 0, got %d", p.MinTools)
	}
	switch p.Mode {
	case ModeOff, ModeHybrid, ModeEvict:
		return nil
	default:
		return fmt.Errorf("sbc: unknown policy mode %q", p.Mode)
	}
}

// ShouldPrune returns true if pruning should be attempted for the given tool count.
func (p RuntimePolicy) ShouldPrune(toolCount int) bool {
	return p.Enabled && p.Mode != ModeOff && toolCount > p.MinTools
}
