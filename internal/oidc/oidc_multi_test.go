package oidc

import (
	"testing"
)

func TestExtractIssuer_ValidJWT(t *testing.T) {
	// Create a minimal JWT with known issuer
	// Header: {"alg":"RS256","typ":"JWT"}
	// Payload: {"iss":"https://auth.example.com","sub":"user123"}
	// Using base64url encoding
	header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9"
	payload := "eyJpc3MiOiJodHRwczovL2F1dGguZXhhbXBsZS5jb20iLCJzdWIiOiJ1c2VyMTIzIn0"
	token := header + "." + payload + ".fake-signature"

	issuer, err := extractIssuer(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issuer != "https://auth.example.com" {
		t.Errorf("issuer = %q, want %q", issuer, "https://auth.example.com")
	}
}

func TestExtractIssuer_NoIssuer(t *testing.T) {
	header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9"
	payload := "eyJzdWIiOiJ1c2VyMTIzIn0" // {"sub":"user123"}
	token := header + "." + payload + ".fake-signature"

	_, err := extractIssuer(token)
	if err == nil {
		t.Fatal("expected error for missing iss claim")
	}
}

func TestExtractIssuer_InvalidFormat(t *testing.T) {
	_, err := extractIssuer("not-a-jwt")
	if err == nil {
		t.Fatal("expected error for invalid JWT format")
	}
}

func TestNormalizeIssuer(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"https://Auth.Example.COM/", "https://auth.example.com"},
		{"https://auth.example.com", "https://auth.example.com"},
		{"HTTPS://AUTH.EXAMPLE.COM//", "https://auth.example.com"},
	}
	for _, tt := range tests {
		got := normalizeIssuer(tt.input)
		if got != tt.want {
			t.Errorf("normalizeIssuer(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPool_IsConfigured(t *testing.T) {
	p := &Pool{}
	if p.IsConfigured() {
		t.Error("empty pool should not be configured")
	}

	// Simulate a single provider
	p.single = &Provider{}
	if !p.IsConfigured() {
		t.Error("pool with single provider should be configured")
	}
}

func TestPool_ValidateToken_NoProvider(t *testing.T) {
	p := &Pool{}
	_, err := p.ValidateToken(nil, "sometoken", nil)
	if err == nil {
		t.Fatal("expected error when no provider configured")
	}
}
