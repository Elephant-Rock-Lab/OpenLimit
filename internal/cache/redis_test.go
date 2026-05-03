package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	rediscli "openlimit/internal/redis"

	"github.com/alicebob/miniredis/v2"

	"openlimit/internal/schema/openai"
)

func newTestRedisCache(t *testing.T) (*RedisCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := rediscli.NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	return NewRedisCache(rc, 5*time.Minute), mr
}

func TestRedisCache_GetMiss(t *testing.T) {
	c, _ := newTestRedisCache(t)
	resp, ok, err := c.Get(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected miss")
	}
	if resp != nil {
		t.Fatal("expected nil response on miss")
	}
}

func TestRedisCache_SetGet(t *testing.T) {
	c, _ := newTestRedisCache(t)
	ctx := context.Background()

	orig := &openai.ChatCompletionResponse{
		ID:    "chatcmpl-123",
		Model: "gpt-4o",
		Choices: []openai.Choice{
			{Index: 0, Message: openai.ChatMessage{Role: "assistant", Content: json.RawMessage(`"Hello!"`)}},
		},
		Usage: &openai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}

	if err := c.Set(ctx, "key1", orig, 0); err != nil {
		t.Fatal(err)
	}

	got, ok, err := c.Get(ctx, "key1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected hit")
	}
	if got.ID != "chatcmpl-123" {
		t.Fatalf("expected ID chatcmpl-123, got %s", got.ID)
	}
	if got.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %s", got.Model)
	}
	if got.Usage.TotalTokens != 15 {
		t.Fatalf("expected 15 tokens, got %d", got.Usage.TotalTokens)
	}
}

func TestRedisCache_Overwrite(t *testing.T) {
	c, _ := newTestRedisCache(t)
	ctx := context.Background()

	v1 := &openai.ChatCompletionResponse{ID: "v1"}
	v2 := &openai.ChatCompletionResponse{ID: "v2"}

	c.Set(ctx, "key", v1, 0)
	c.Set(ctx, "key", v2, 0)

	got, ok, _ := c.Get(ctx, "key")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.ID != "v2" {
		t.Fatalf("expected v2, got %s", got.ID)
	}
}

func TestRedisCache_ImplementsInterface(t *testing.T) {
	var _ Cache = (*RedisCache)(nil)
}

func TestRedisCache_DefaultTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := rediscli.NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	c := NewRedisCache(rc, 0) // zero TTL should use default

	ctx := context.Background()
	resp := &openai.ChatCompletionResponse{ID: "test"}
	if err := c.Set(ctx, "k", resp, 0); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := c.Get(ctx, "k")
	if !ok || got.ID != "test" {
		t.Fatal("expected hit with default TTL")
	}
}

func TestRedisCache_CustomTTL(t *testing.T) {
	c, mr := newTestRedisCache(t)
	ctx := context.Background()

	resp := &openai.ChatCompletionResponse{ID: "short-lived"}
	if err := c.Set(ctx, "k", resp, 1*time.Second); err != nil {
		t.Fatal(err)
	}

	// Should exist immediately
	_, ok, _ := c.Get(ctx, "k")
	if !ok {
		t.Fatal("expected hit before expiry")
	}

	// Expire the key
	mr.FastForward(2 * time.Second)

	_, ok, _ = c.Get(ctx, "k")
	if ok {
		t.Fatal("expected miss after TTL expiry")
	}
}
