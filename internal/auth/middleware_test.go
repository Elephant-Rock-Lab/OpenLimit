package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"openlimit/internal/config"
)

func TestMiddlewarePassThroughWhenDisabled(t *testing.T) {
	cfg := config.AuthConfig{Enabled: false}
	mw := NewMiddleware(cfg, nil)

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Wrap(inner)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected handler to be called when auth is disabled")
	}
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestMiddlewareRejectsMissingToken(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, KeyCacheSize: 100, KeyCacheTTLSec: 60}
	mw := NewMiddleware(cfg, nil) // nil db = still rejects without token

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})

	handler := mw.Wrap(inner)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestMiddlewareRejectsNonBearer(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, KeyCacheSize: 100, KeyCacheTTLSec: 60}
	mw := NewMiddleware(cfg, nil)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})

	handler := mw.Wrap(inner)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Basic abc123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestMiddlewareRejectsNonGwPrefix(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, KeyCacheSize: 100, KeyCacheTTLSec: 60}
	mw := NewMiddleware(cfg, nil)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})

	handler := mw.Wrap(inner)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-abc123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestContextModelAllowed(t *testing.T) {
	tests := []struct {
		name          string
		allowedModels []string
		model         string
		want          bool
	}{
		{"empty allows all", nil, "gpt-4o", true},
		{"empty slice allows all", []string{}, "gpt-4o", true},
		{"exact match", []string{"gpt-4o", "claude-3-5-haiku"}, "gpt-4o", true},
		{"case insensitive", []string{"GPT-4o"}, "gpt-4o", true},
		{"no match", []string{"gpt-4o"}, "claude-3-5-haiku", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := &Context{AllowedModels: tt.allowedModels}
			if got := ac.ModelAllowed(tt.model); got != tt.want {
				t.Errorf("ModelAllowed(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestContextProviderAllowed(t *testing.T) {
	ac := &Context{AllowedProviders: []string{"openai"}}
	if !ac.ProviderAllowed("openai") {
		t.Error("expected openai to be allowed")
	}
	if ac.ProviderAllowed("anthropic") {
		t.Error("expected anthropic to not be allowed")
	}
	ac2 := &Context{}
	if !ac2.ProviderAllowed("anything") {
		t.Error("empty allowed providers should allow all")
	}
}

func TestKeyCacheSetGet(t *testing.T) {
	cache := NewKeyCache(10, 60*1e9) // 60s TTL
	authCtx := &Context{ProjectID: "test", VirtualKeyID: "key1"}

	cache.Set("token1", authCtx)

	got, ok := cache.Get("token1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.VirtualKeyID != "key1" {
		t.Fatalf("expected key1, got %s", got.VirtualKeyID)
	}
}

func TestKeyCacheMiss(t *testing.T) {
	cache := NewKeyCache(10, 60*1e9)
	_, ok := cache.Get("nonexistent")
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestKeyCacheInvalidate(t *testing.T) {
	cache := NewKeyCache(10, 60*1e9)
	cache.Set("token1", &Context{VirtualKeyID: "key1"})
	cache.Invalidate("token1")
	_, ok := cache.Get("token1")
	if ok {
		t.Fatal("expected cache miss after invalidation")
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name string
		auth string
		want string
	}{
		{"valid", "Bearer gw-abc123", "gw-abc123"},
		{"missing prefix", "Bearer abc123", ""},
		{"wrong scheme", "Basic gw-abc123", ""},
		{"empty", "", ""},
		{"with spaces", "Bearer  gw-abc123 ", "gw-abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.auth != "" {
				r.Header.Set("Authorization", tt.auth)
			}
			got := extractBearerToken(r)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
