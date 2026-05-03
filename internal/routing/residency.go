package routing

import (
	"strings"

	"openlimit/internal/providers"
)

// FilterByResidency removes targets that don't match the required data residency.
// If residency is empty, all targets pass through.
// Matching: target's DataResidency tag or region name prefix matches the requirement.
func FilterByResidency(targets []providers.Target, residency string) []providers.Target {
	if residency == "" {
		return targets
	}

	residencyLower := strings.ToLower(strings.TrimSpace(residency))
	var filtered []providers.Target
	for _, t := range targets {
		if matchesResidency(t, residencyLower) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// matchesResidency checks if a target satisfies a residency requirement.
func matchesResidency(target providers.Target, residency string) bool {
	// No region info → can't enforce residency → filtered out
	if target.Region == "" || target.Region == "default" {
		return false
	}

	// Check explicit DataResidency tag first
	if target.DataResidency != "" {
		return strings.ToLower(target.DataResidency) == residency
	}

	// Fall back to region name prefix: "eu-west" matches "eu"
	return strings.HasPrefix(strings.ToLower(target.Region), residency)
}
