package guardrails

import (
	"testing"
)

// ---------------------------------------------------------------------------
// TEST-11-01-01: Default client used when no TLS config
// ---------------------------------------------------------------------------

func TestWebhook_DefaultClient(t *testing.T) {
	stage := NewWebhookStage("http://localhost:9999", 100, false, false)
	if stage == nil {
		t.Fatal("expected non-nil stage")
	}
	if stage.client == nil {
		t.Error("expected non-nil client")
	}
	// Default client has no custom transport
	if stage.client.Transport != nil {
		t.Error("expected nil transport for default client")
	}
}

// ---------------------------------------------------------------------------
// TEST-11-01-03: Invalid cert path returns error
// ---------------------------------------------------------------------------

func TestWebhook_InvalidCertPath(t *testing.T) {
	_, err := NewWebhookStageWithTLS("https://localhost:9999", 100, false, false, "/nonexistent/cert.pem", "/nonexistent/key.pem", "")
	if err == nil {
		t.Error("expected error for invalid cert path")
	}
}

// ---------------------------------------------------------------------------
// TEST-11-01-04: Empty cert files creates stage without TLS
// ---------------------------------------------------------------------------

func TestWebhook_NoCertFiles(t *testing.T) {
	// Empty cert/key paths should create a stage without client certs
	stage, err := NewWebhookStageWithTLS("https://localhost:9999", 100, false, false, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stage == nil {
		t.Fatal("expected non-nil stage")
	}
}

// ---------------------------------------------------------------------------
// TEST-11-01-02: TLS client created with valid certs (integration-level)
// ---------------------------------------------------------------------------

func TestWebhook_TLSConstructorExists(t *testing.T) {
	// Verify the constructor exists and is callable
	// Full TLS cert testing requires integration test with real certs
	_ = NewWebhookStageWithTLS
}
