package errtypes

import (
	"strings"
	"testing"
)

func TestEnrichProviderError_RateLimit(t *testing.T) {
	msg := EnrichProviderError("openai", 429, "")
	if !strings.Contains(msg, "rate-limiting") {
		t.Errorf("expected message to contain 'rate-limiting', got: %s", msg)
	}
	if !strings.Contains(msg, "retry") {
		t.Errorf("expected message to contain 'retry', got: %s", msg)
	}
}

func TestEnrichProviderError_AuthFailed(t *testing.T) {
	msg := EnrichProviderError("anthropic", 401, "")
	if !strings.Contains(msg, "Authentication failed") {
		t.Errorf("expected message to contain 'Authentication failed', got: %s", msg)
	}
	if !strings.Contains(msg, "anthropic") {
		t.Errorf("expected message to contain provider name 'anthropic', got: %s", msg)
	}
}

func TestEnrichProviderError_InternalError(t *testing.T) {
	msg := EnrichProviderError("groq", 500, "")
	if !strings.Contains(msg, "internal error") {
		t.Errorf("expected message to contain 'internal error', got: %s", msg)
	}
	if !strings.Contains(msg, "groq") {
		t.Errorf("expected message to contain provider name 'groq', got: %s", msg)
	}
}

func TestEnrichProviderError_UnknownStatus(t *testing.T) {
	msg := EnrichProviderError("openai", 418, "")
	if !strings.Contains(msg, "418") {
		t.Errorf("expected message to contain status code '418', got: %s", msg)
	}
	if !strings.Contains(msg, "unexpected error") {
		t.Errorf("expected message to contain 'unexpected error', got: %s", msg)
	}
}

func TestEnrichProviderError_ConnectionErrorWithBody(t *testing.T) {
	body := strings.Repeat("x", 300)
	msg := EnrichProviderError("openai", 0, body)
	if !strings.Contains(msg, "failed") {
		t.Errorf("expected message to contain 'failed', got: %s", msg)
	}
	if !strings.Contains(msg, "...") {
		t.Error("expected body to be truncated with '...'")
	}
	if len(msg) > 250 {
		t.Errorf("expected truncated message, got length %d: %s", len(msg), msg)
	}
}

func TestEnrichProviderError_PaymentRequired(t *testing.T) {
	msg := EnrichProviderError("openai", 402, "")
	if !strings.Contains(msg, "payment") {
		t.Errorf("expected message to contain 'payment', got: %s", msg)
	}
}

func TestEnrichProviderError_Forbidden(t *testing.T) {
	msg := EnrichProviderError("anthropic", 403, "")
	if !strings.Contains(msg, "permissions") && !strings.Contains(msg, "model access") {
		t.Errorf("expected message to contain 'permissions' or 'model access', got: %s", msg)
	}
}
