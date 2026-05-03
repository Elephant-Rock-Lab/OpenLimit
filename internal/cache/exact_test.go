package cache

import (
	"context"
	"testing"
	"time"

	"openlimit/internal/schema/openai"
)

func TestExactLRUEvictsOldest(t *testing.T) {
	c := NewExactLRU(1)
	ctx := context.Background()

	if err := c.Set(ctx, "one", response("one"), time.Minute); err != nil {
		t.Fatalf("set one: %v", err)
	}
	if err := c.Set(ctx, "two", response("two"), time.Minute); err != nil {
		t.Fatalf("set two: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "one"); ok {
		t.Fatal("expected oldest entry to be evicted")
	}
	if got, ok, _ := c.Get(ctx, "two"); !ok || got.ID != "two" {
		t.Fatalf("expected newest entry, got ok=%v resp=%#v", ok, got)
	}
}

func TestExactLRUExpiresEntries(t *testing.T) {
	c := NewExactLRU(10)
	ctx := context.Background()

	if err := c.Set(ctx, "expired", response("expired"), time.Nanosecond); err != nil {
		t.Fatalf("set expired: %v", err)
	}
	time.Sleep(time.Millisecond)
	if _, ok, _ := c.Get(ctx, "expired"); ok {
		t.Fatal("expected expired entry to miss")
	}
}

func TestHashRequestIgnoresStreamFlag(t *testing.T) {
	base := openai.ChatCompletionRequest{
		Model:    "fast",
		Messages: []openai.ChatMessage{{Role: "user", Content: []byte(`"hello"`)}},
	}
	streaming := base
	streaming.Stream = true

	baseHash, err := HashRequest(base)
	if err != nil {
		t.Fatalf("hash base: %v", err)
	}
	streamHash, err := HashRequest(streaming)
	if err != nil {
		t.Fatalf("hash streaming: %v", err)
	}
	if baseHash != streamHash {
		t.Fatalf("expected stream flag to be ignored, got %s vs %s", baseHash, streamHash)
	}
}

func response(id string) *openai.ChatCompletionResponse {
	return &openai.ChatCompletionResponse{ID: id, Object: "chat.completion", Model: "test"}
}
