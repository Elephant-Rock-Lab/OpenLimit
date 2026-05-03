package routing

import (
	"testing"

	"openlimit/internal/providers"
)

func TestFilterByResidency_Empty(t *testing.T) {
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4", Region: "us-east"},
	}
	result := FilterByResidency(targets, "")
	if len(result) != 1 {
		t.Fatalf("expected 1 target, got %d", len(result))
	}
}

func TestFilterByResidency_ExactTag(t *testing.T) {
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4", Region: "eu-west", DataResidency: "eu"},
		{Provider: "openai", Model: "gpt-4", Region: "us-east", DataResidency: "us"},
	}
	result := FilterByResidency(targets, "eu")
	if len(result) != 1 {
		t.Fatalf("expected 1 target, got %d", len(result))
	}
	if result[0].Region != "eu-west" {
		t.Errorf("expected eu-west, got %s", result[0].Region)
	}
}

func TestFilterByResidency_PrefixMatch(t *testing.T) {
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4", Region: "eu-west"},
		{Provider: "openai", Model: "gpt-4", Region: "us-east"},
	}
	result := FilterByResidency(targets, "eu")
	if len(result) != 1 {
		t.Fatalf("expected 1 target, got %d", len(result))
	}
	if result[0].Region != "eu-west" {
		t.Errorf("expected eu-west, got %s", result[0].Region)
	}
}

func TestFilterByResidency_NoMatch(t *testing.T) {
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4", Region: "us-east"},
		{Provider: "openai", Model: "gpt-4", Region: "us-west"},
	}
	result := FilterByResidency(targets, "eu")
	if len(result) != 0 {
		t.Fatalf("expected 0 targets, got %d", len(result))
	}
}

func TestFilterByResidency_DefaultRegionFiltered(t *testing.T) {
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4", Region: ""},        // no region
		{Provider: "openai", Model: "gpt-4", Region: "default"}, // default sentinel
		{Provider: "openai", Model: "gpt-4", Region: "eu-west"},
	}
	result := FilterByResidency(targets, "eu")
	if len(result) != 1 {
		t.Fatalf("expected 1 target, got %d", len(result))
	}
	if result[0].Region != "eu-west" {
		t.Errorf("expected eu-west, got %s", result[0].Region)
	}
}

func TestFilterByResidency_AllFiltered(t *testing.T) {
	targets := []providers.Target{
		{Provider: "openai", Model: "gpt-4", Region: "us-east"},
	}
	result := FilterByResidency(targets, "eu")
	if len(result) != 0 {
		t.Fatalf("expected 0 targets, got %d", len(result))
	}
}
