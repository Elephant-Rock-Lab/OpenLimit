package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestGetWithGrace_FreshEntry(t *testing.T) {
	cache := NewKeyCache(10, 60*1e9) // 60s TTL
	authCtx := &Context{ProjectID: "test", VirtualKeyID: "key1"}
	cache.Set("token1", authCtx)

	// Fresh entry should be served via GetWithGrace even with 0 grace
	got, ok := cache.GetWithGrace("token1", 0)
	if !ok {
		t.Fatal("expected GetWithGrace hit for fresh entry")
	}
	if got.VirtualKeyID != "key1" {
		t.Fatalf("expected key1, got %s", got.VirtualKeyID)
	}
}

func TestGetWithGrace_ExpiredWithinGrace(t *testing.T) {
	cache := NewKeyCache(10, 1*time.Nanosecond) // extremely short TTL
	authCtx := &Context{ProjectID: "test", VirtualKeyID: "key2"}
	cache.Set("token1", authCtx)

	// Wait for entry to expire
	time.Sleep(1 * time.Millisecond)

	// Regular Get should miss
	_, ok := cache.Get("token1")
	if ok {
		t.Fatal("expected regular Get to miss for expired entry")
	}

	// GetWithGrace with 5-minute grace should still hit
	got, ok := cache.GetWithGrace("token1", 5*time.Minute)
	if !ok {
		t.Fatal("expected GetWithGrace hit within grace period")
	}
	if got.VirtualKeyID != "key2" {
		t.Fatalf("expected key2, got %s", got.VirtualKeyID)
	}
}

func TestGetWithGrace_ExpiredBeyondGrace(t *testing.T) {
	cache := NewKeyCache(10, 1*time.Nanosecond) // extremely short TTL
	authCtx := &Context{ProjectID: "test", VirtualKeyID: "key3"}
	cache.Set("token1", authCtx)

	// Wait for entry to be well beyond any reasonable grace
	time.Sleep(10 * time.Millisecond)

	// GetWithGrace with very short grace should miss
	_, ok := cache.GetWithGrace("token1", 1*time.Nanosecond)
	if ok {
		t.Fatal("expected GetWithGrace miss for entry beyond grace")
	}
}

func TestGetWithGrace_CacheMiss(t *testing.T) {
	cache := NewKeyCache(10, 60*1e9)

	_, ok := cache.GetWithGrace("nonexistent", 5*time.Minute)
	if ok {
		t.Fatal("expected GetWithGrace miss for nonexistent key")
	}
}

func TestIsDBError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not found", fmt.Errorf("no matching virtual key found"), false},
		{"connection refused", fmt.Errorf("connection refused"), true},
		{"sql error", fmt.Errorf("pq: unexpected EOF"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDBError(tt.err); got != tt.want {
				t.Errorf("isDBError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name string
		auth string
		want string
	}{
		{"valid", "Bearer gw-abc1234567", "gw-abc1234567"},
		{"missing prefix", "Bearer abc123", ""},
		{"wrong scheme", "Basic gw-abc1234567", ""},
		{"empty", "", ""},
		{"with spaces", "Bearer  gw-abc1234567 ", "gw-abc1234567"},
		{"too short", "Bearer gw-abc", ""},
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
