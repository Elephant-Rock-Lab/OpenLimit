package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	pn.ssrfDisabled = true // test server runs on localhost
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
	pn.ssrfDisabled = true // test server runs on localhost
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
	pn.ssrfDisabled = true // test server runs on localhost
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
	pn.ssrfDisabled = true // test server runs on localhost
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

// --- SSRF validation tests (TEST-39-01) ---

func TestValidatePushURL_PublicHTTPS(t *testing.T) {
	// TEST-39-01-01: Valid public HTTPS URL passes validation
	err := validatePushURL("https://example.com/webhook")
	if err != nil {
		t.Fatalf("public URL should be allowed, got: %v", err)
	}
}

func TestValidatePushURL_PrivateIPv4_10(t *testing.T) {
	// TEST-39-01-02: Private 10.x.x.x is rejected
	err := validatePushURL("http://10.0.0.1/webhook")
	if err == nil {
		t.Fatal("private IP 10.x should be rejected")
	}
	if !strings.Contains(err.Error(), "private") {
		t.Errorf("error should mention 'private', got: %v", err)
	}
}

func TestValidatePushURL_PrivateIPv4_172(t *testing.T) {
	// TEST-39-01-03: Private 172.16.x.x is rejected
	err := validatePushURL("http://172.16.0.1/webhook")
	if err == nil {
		t.Fatal("private IP 172.16.x should be rejected")
	}
}

func TestValidatePushURL_PrivateIPv4_192(t *testing.T) {
	// TEST-39-01-04: Private 192.168.x.x is rejected
	err := validatePushURL("http://192.168.1.1/webhook")
	if err == nil {
		t.Fatal("private IP 192.168.x should be rejected")
	}
}

func TestValidatePushURL_Loopback(t *testing.T) {
	// TEST-39-01-05: Loopback 127.0.0.1 is rejected
	err := validatePushURL("http://127.0.0.1/webhook")
	if err == nil {
		t.Fatal("loopback should be rejected")
	}
}

func TestValidatePushURL_LinkLocal(t *testing.T) {
	// TEST-39-01-06: Link-local 169.254.169.254 (AWS metadata) is rejected
	err := validatePushURL("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("link-local IP should be rejected")
	}
}

func TestValidatePushURL_IPv6Loopback(t *testing.T) {
	// TEST-39-01-07: IPv6 loopback ::1 is rejected
	err := validatePushURL("http://[::1]/webhook")
	if err == nil {
		t.Fatal("IPv6 loopback should be rejected")
	}
}

func TestValidatePushURL_NonHTTPScheme(t *testing.T) {
	// TEST-39-01-08: Non-HTTP(S) scheme is rejected
	err := validatePushURL("ftp://example.com/file")
	if err == nil {
		t.Fatal("ftp scheme should be rejected")
	}
}

func TestValidatePushURL_Empty(t *testing.T) {
	// TEST-39-01-09: Empty URL returns nil (no push config)
	err := validatePushURL("")
	if err != nil {
		t.Fatalf("empty URL should not error (no push), got: %v", err)
	}
}

func TestNotify_SkipsBlockedURL(t *testing.T) {
	// TEST-39-01-10: Notify rejects SSRF URLs without making HTTP request
	pn := NewPushNotifier(nil)
	task := &A2ATask{ID: "ssrf-test", Status: TaskStateCompleted}
	cfg := &PushConfig{URL: "http://127.0.0.1:9999/webhook"}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := pn.Notify(ctx, task, cfg)
	if err == nil {
		t.Fatal("Notify should reject SSRF URL")
	}
	if !strings.Contains(err.Error(), "push URL rejected") {
		t.Errorf("error should mention 'push URL rejected', got: %v", err)
	}
}
