package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPushNotifySuccess(t *testing.T) {
	var received int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&received, 1)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		var task A2ATask
		json.NewDecoder(r.Body).Decode(&task)
		if task.ID != "task-push-1" {
			t.Errorf("task ID = %q, want task-push-1", task.ID)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pn := NewPushNotifier(nil)
	task := &A2ATask{
		ID:     "task-push-1",
		Status: TaskStateCompleted,
	}
	cfg := &PushConfig{URL: server.URL}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pn.Notify(ctx, task, cfg); err != nil {
		t.Fatalf("Notify failed: %v", err)
	}
	if atomic.LoadInt32(&received) != 1 {
		t.Errorf("received = %d, want 1", received)
	}
}

func TestPushNotifyWithAuth(t *testing.T) {
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pn := NewPushNotifier(nil)
	task := &A2ATask{ID: "task-auth-1", Status: TaskStateCompleted}
	cfg := &PushConfig{URL: server.URL, AuthType: "bearer", AuthToken: "secret"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pn.Notify(ctx, task, cfg); err != nil {
		t.Fatalf("Notify failed: %v", err)
	}
	if gotToken != "Bearer secret" {
		t.Errorf("auth = %q, want 'Bearer secret'", gotToken)
	}
}

func TestPushNotifyRetryOnFailure(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pn := NewPushNotifier(nil)
	task := &A2ATask{ID: "task-retry-1", Status: TaskStateCompleted}
	cfg := &PushConfig{URL: server.URL}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := pn.Notify(ctx, task, cfg); err != nil {
		t.Fatalf("Notify failed: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestPushNotifyGivesUpAfterRetries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	pn := NewPushNotifier(nil)
	task := &A2ATask{ID: "task-fail-1", Status: TaskStateCompleted}
	cfg := &PushConfig{URL: server.URL}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := pn.Notify(ctx, task, cfg)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}
