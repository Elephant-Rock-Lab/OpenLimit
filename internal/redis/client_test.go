package redis

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestNewClient_EmptyAddr(t *testing.T) {
	c := NewClient("", "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	if c != nil {
		t.Fatal("expected nil for empty addr")
	}
}

func TestNewClient_Connects(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	defer c.Close()

	if !c.Healthy() {
		t.Fatal("expected healthy")
	}
}

func TestNewClient_BadAddr_StartsDegraded(t *testing.T) {
	c := NewClient("localhost:19999", "", 0, 0, 5, 10*time.Second, slog.Default(), false)
	defer c.Close()

	if c.Healthy() {
		t.Fatal("expected unhealthy for bad addr")
	}
}

func TestClient_SetGet(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	defer c.Close()

	ctx := context.Background()
	if err := c.Set(ctx, "foo", "bar", 0); err != nil {
		t.Fatal(err)
	}
	val, err := c.Get(ctx, "foo")
	if err != nil {
		t.Fatal(err)
	}
	if val != "bar" {
		t.Fatalf("expected bar, got %s", val)
	}
}

func TestClient_HSetHGetAll(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	defer c.Close()

	ctx := context.Background()
	if err := c.HSet(ctx, "hash1", "field1", "val1", "field2", "val2"); err != nil {
		t.Fatal(err)
	}
	m, err := c.HGetAll(ctx, "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if m["field1"] != "val1" || m["field2"] != "val2" {
		t.Fatalf("unexpected map: %v", m)
	}
}

func TestClient_ZOps(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	defer c.Close()

	ctx := context.Background()
	if err := c.ZAdd(ctx, "zset1", goredis.Z{Score: 1.0, Member: "m1"}); err != nil {
		t.Fatal(err)
	}
	if err := c.ZAdd(ctx, "zset1", goredis.Z{Score: 2.0, Member: "m2"}); err != nil {
		t.Fatal(err)
	}
	card, err := c.ZCard(ctx, "zset1")
	if err != nil {
		t.Fatal(err)
	}
	if card != 2 {
		t.Fatalf("expected 2, got %d", card)
	}
}

func TestClient_Del(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	defer c.Close()

	ctx := context.Background()
	c.Set(ctx, "k", "v", 0)
	if err := c.Del(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	_, err := c.Get(ctx, "k")
	if err == nil {
		t.Fatal("expected key not found")
	}
}

func TestClient_SetHealthy(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewClient(mr.Addr(), "", 0, 3, 20, 10*time.Second, slog.Default(), false)
	defer c.Close()

	if !c.Healthy() {
		t.Fatal("expected healthy")
	}
	c.SetHealthy(false)
	if c.Healthy() {
		t.Fatal("expected unhealthy after SetHealthy(false)")
	}
}

func TestClient_NilMethods(t *testing.T) {
	var c *Client
	if c.Healthy() {
		t.Fatal("nil client should not be healthy")
	}
	if err := c.Close(); err != nil {
		t.Fatal("nil close should be nil")
	}
	if cl := c.Standalone(); cl != nil {
		t.Fatal("nil client should return nil standalone client")
	}
}

func TestClient_Defaults(t *testing.T) {
	mr := miniredis.RunT(t)
	// Test with zero poolSize and negative retries — defaults should apply
	c := NewClient(mr.Addr(), "", 0, -1, 0, 0, slog.Default(), false)
	defer c.Close()
	if !c.Healthy() {
		t.Fatal("expected healthy with defaults")
	}
}
