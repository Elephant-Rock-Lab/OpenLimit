package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"openlimit/internal/config"
)

// ---------------------------------------------------------------------------
// TEST-36-01-03: MCP server authenticate rejects wrong token via ConstantTimeCompare
// ---------------------------------------------------------------------------
func TestMCPServerAuth_RejectsWrongToken(t *testing.T) {
	cfg := config.MCPServerModeConfig{
		Enabled:  true,
		Endpoint: "/mcp",
		Auth:     config.MCPAuthConfig{Mode: "bearer_token", BearerToken: "secret-token"},
	}
	handler := NewServerHandler(cfg, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	if handler.authenticate(req) {
		t.Error("expected authenticate() to return false for wrong token")
	}
}

// ---------------------------------------------------------------------------
// TEST-36-01-04: MCP server authenticate accepts correct token via ConstantTimeCompare
// ---------------------------------------------------------------------------
func TestMCPServerAuth_AcceptsCorrectToken(t *testing.T) {
	cfg := config.MCPServerModeConfig{
		Enabled:  true,
		Endpoint: "/mcp",
		Auth:     config.MCPAuthConfig{Mode: "bearer_token", BearerToken: "secret-token"},
	}
	handler := NewServerHandler(cfg, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret-token")

	if !handler.authenticate(req) {
		t.Error("expected authenticate() to return true for correct token")
	}
}

// ---------------------------------------------------------------------------
// TEST-36-01-05: A2A handler bearer token auth rejects wrong token
// ---------------------------------------------------------------------------
func TestA2AAuth_BearerToken_RejectsWrongToken(t *testing.T) {
	cfg := config.A2AConfig{
		Enabled:        true,
		Endpoint:       "/a2a",
		Authentication: config.MCPAuthConfig{Mode: "bearer_token", BearerToken: "a2a-secret"},
		DefaultModel:   "gpt-4o",
		AgentCard: config.AgentCardConfig{
			Name:    "Test",
			Version: "1.0",
		},
	}
	store := NewMemoryTaskStore(100, 0)
	exec := func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
		return &ChatResult{Content: "ok"}, nil
	}
	h, err := NewA2AHandler(cfg, exec, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()

	req := httptest.NewRequest(http.MethodPost, "/a2a", nil)
	req.Header.Set("Authorization", "Bearer wrong-a2a-token")

	_, ok := h.authenticate(req)
	if ok {
		t.Error("expected authenticate() to return false for wrong A2A token")
	}
}

// ---------------------------------------------------------------------------
// TEST-36-01-06: A2A handler bearer token auth accepts correct token
// ---------------------------------------------------------------------------
func TestA2AAuth_BearerToken_AcceptsCorrectToken(t *testing.T) {
	cfg := config.A2AConfig{
		Enabled:        true,
		Endpoint:       "/a2a",
		Authentication: config.MCPAuthConfig{Mode: "bearer_token", BearerToken: "a2a-secret"},
		DefaultModel:   "gpt-4o",
		AgentCard: config.AgentCardConfig{
			Name:    "Test",
			Version: "1.0",
		},
	}
	store := NewMemoryTaskStore(100, 0)
	exec := func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
		return &ChatResult{Content: "ok"}, nil
	}
	h, err := NewA2AHandler(cfg, exec, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()

	req := httptest.NewRequest(http.MethodPost, "/a2a", nil)
	req.Header.Set("Authorization", "Bearer a2a-secret")

	_, ok := h.authenticate(req)
	if !ok {
		t.Error("expected authenticate() to return true for correct A2A token")
	}
}
