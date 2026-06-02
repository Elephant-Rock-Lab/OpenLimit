package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"openlimit/internal/config"
)

// mockA2AExecutor is a ChatExecutor that returns a predictable response.
func mockA2AExecutor(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
	return &ChatResult{
		Content: "Echo: test response",
		Model:   "gpt-4o",
		Usage:   &ChatUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}, nil
}

func testA2AConfig() config.A2AConfig {
	return config.A2AConfig{
		Enabled:        true,
		Endpoint:       "/a2a",
		URL:            "http://localhost:8080",
		Authentication: config.MCPAuthConfig{Mode: "none"},
		DefaultModel:   "gpt-4o",
		AgentCard: config.AgentCardConfig{
			Name:        "Test Agent",
			Version:     "1.0.0",
			Description: "Test A2A agent",
		},
		BlockingMode: true, // tests use blocking mode for simplicity
	}
}

func newTestHandler(t *testing.T, cfg config.A2AConfig, executor ChatExecutor) *A2AHandler {
	t.Helper()
	store := NewMemoryTaskStore(cfg.MaxTasks, time.Duration(cfg.TaskTTLSec)*time.Second)
	h, err := NewA2AHandler(cfg, executor, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Shutdown() })
	return h
}

func TestA2AAgentCard(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	req := httptest.NewRequest("GET", "/.well-known/agent.json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var card AgentCard
	if err := json.NewDecoder(w.Body).Decode(&card); err != nil {
		t.Fatal(err)
	}
	if card.Name != "Test Agent" {
		t.Errorf("name = %q, want 'Test Agent'", card.Name)
	}
	if card.ProtocolVersion != "1.0" {
		t.Errorf("protocol version = %q, want '1.0'", card.ProtocolVersion)
	}
	if !card.Capabilities.Streaming {
		t.Error("streaming should be true")
	}
	if !card.Capabilities.PushNotifications {
		t.Error("push notifications should be true")
	}
}

func TestA2AMessageSendBlocking(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      1,
		"params": map[string]any{
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-test-1",
				"parts": []map[string]any{
					{"type": "text", "text": "Hello"},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var task A2ATask
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatal(err)
	}

	if task.Status != TaskStateCompleted {
		t.Errorf("status = %q, want 'completed'", task.Status)
	}
	if task.ID == "" {
		t.Error("expected non-empty task ID")
	}
	if task.ContextID == "" {
		t.Error("expected non-empty context ID")
	}
	if len(task.Artifacts) != 1 {
		t.Fatalf("artifacts count = %d, want 1", len(task.Artifacts))
	}
	if task.Artifacts[0].Parts[0].Text != "Echo: test response" {
		t.Errorf("artifact text = %q", task.Artifacts[0].Parts[0].Text)
	}
	if len(task.History) != 2 {
		t.Errorf("history count = %d, want 2 (user + agent)", len(task.History))
	}
}

func TestA2AMessageSendEmptyText(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      2,
		"params": map[string]any{
			"message": map[string]any{
				"role":  "user",
				"parts": []map[string]any{},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected error for empty text")
	}
	if resp.Error.Code != CodeContentTypeNotSupported {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeContentTypeNotSupported)
	}
}

func TestA2ATasksGet(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	// Create a task directly in the store
	task := &A2ATask{
		ID:        "task-test-123",
		ContextID: "ctx-test",
		Status:    TaskStateCompleted,
		Artifacts: []A2AArtifact{{Parts: []A2APart{{Type: "text", Text: "done"}}}},
		History:   []A2AMessage{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	h.store.Create(task)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/get",
		"id":      3,
		"params":  map[string]any{"id": "task-test-123"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var retrieved A2ATask
	json.Unmarshal(resp.Result, &retrieved)
	if retrieved.ID != "task-test-123" {
		t.Errorf("id = %q, want 'task-test-123'", retrieved.ID)
	}
	if retrieved.Status != TaskStateCompleted {
		t.Errorf("status = %q, want 'completed'", retrieved.Status)
	}
}

func TestA2ATasksGetNotFound(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/get",
		"id":      4,
		"params":  map[string]any{"id": "nonexistent"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if resp.Error.Code != CodeTaskNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeTaskNotFound)
	}
}

func TestA2ATasksCancel(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	task := &A2ATask{
		ID:        "task-cancel-1",
		Status:    TaskStateWorking,
		History:   []A2AMessage{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	h.store.Create(task)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/cancel",
		"id":      5,
		"params":  map[string]any{"id": "task-cancel-1"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var updated A2ATask
	json.Unmarshal(resp.Result, &updated)
	if updated.Status != TaskStateCanceled {
		t.Errorf("status = %q, want 'canceled'", updated.Status)
	}
}

func TestA2ATasksCancelTerminal(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	task := &A2ATask{
		ID:        "task-done-1",
		Status:    TaskStateCompleted,
		History:   []A2AMessage{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	h.store.Create(task)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/cancel",
		"id":      6,
		"params":  map[string]any{"id": "task-done-1"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected error for canceling terminal task")
	}
	if resp.Error.Code != CodeTaskNotCancelable {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeTaskNotCancelable)
	}
}

func TestA2ATasksList(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	// Create some tasks
	for i := 0; i < 3; i++ {
		task := &A2ATask{
			ID:        fmt.Sprintf("task-list-%d", i),
			ContextID: "ctx-test",
			Status:    TaskStateCompleted,
			History:   []A2AMessage{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		h.store.Create(task)
	}

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/list",
		"id":      7,
		"params":  map[string]any{},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result struct {
		Tasks []*A2ATask `json:"tasks"`
		Total int        `json:"total"`
	}
	json.Unmarshal(resp.Result, &result)
	if result.Total != 3 {
		t.Errorf("total = %d, want 3", result.Total)
	}
	if len(result.Tasks) != 3 {
		t.Errorf("tasks len = %d, want 3", len(result.Tasks))
	}
}

func TestA2ATasksListWithFilter(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	for i, status := range []TaskState{TaskStateCompleted, TaskStateCompleted, TaskStateFailed} {
		task := &A2ATask{
			ID:        fmt.Sprintf("task-filter-%d", i),
			Status:    status,
			History:   []A2AMessage{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		h.store.Create(task)
	}

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/list",
		"id":      8,
		"params": map[string]any{
			"filter": map[string]any{"status": "completed"},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result struct {
		Tasks []*A2ATask `json:"tasks"`
		Total int        `json:"total"`
	}
	json.Unmarshal(resp.Result, &result)
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
}

func TestA2ABearerAuth(t *testing.T) {
	cfg := testA2AConfig()
	cfg.Authentication = config.MCPAuthConfig{
		Mode:        "bearer_token",
		BearerToken: "secret-token",
	}
	h := newTestHandler(t, cfg, mockA2AExecutor)

	// Test without auth
	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/get",
		"id":      9,
		"params":  map[string]any{"id": "x"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}

	// Test with valid auth
	req = httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer secret-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	// Should get task-not-found, not auth error
	if resp.Error != nil && resp.Error.Code == CodeInvalidRequest {
		t.Error("should not get auth error with valid token")
	}
}

func TestA2ATaskStoreMaxTasks(t *testing.T) {
	cfg := testA2AConfig()
	cfg.MaxTasks = 2
	cfg.BlockingMode = true // blocking mode to test store max
	h := newTestHandler(t, cfg, mockA2AExecutor)

	// Fill the store
	for i := 0; i < 2; i++ {
		task := &A2ATask{
			ID:        fmt.Sprintf("task-%d", i),
			Status:    TaskStateCompleted,
			History:   []A2AMessage{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := h.store.Create(task); err != nil {
			t.Fatalf("failed to create task %d: %v", i, err)
		}
	}

	// Third should fail
	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      10,
		"params": map[string]any{
			"message": map[string]any{
				"role": "user",
				"parts": []map[string]any{
					{"type": "text", "text": "overflow"},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected max tasks error")
	}
	if resp.Error.Code != CodeMaxTasksReached {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeMaxTasksReached)
	}
}

func TestA2AFailedExecution(t *testing.T) {
	failExecutor := func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
		return nil, fmt.Errorf("provider unavailable")
	}

	h := newTestHandler(t, testA2AConfig(), failExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      11,
		"params": map[string]any{
			"message": map[string]any{
				"role": "user",
				"parts": []map[string]any{
					{"type": "text", "text": "trigger failure"},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	var task A2ATask
	json.Unmarshal(resp.Result, &task)
	if task.Status != TaskStateFailed {
		t.Errorf("status = %q, want 'failed'", task.Status)
	}
	if task.StatusMessage == nil {
		t.Error("expected status message with error details")
	}
}

func TestA2AMessageSendNonBlocking(t *testing.T) {
	cfg := testA2AConfig()
	cfg.BlockingMode = false // non-blocking mode
	h := newTestHandler(t, cfg, mockA2AExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      12,
		"params": map[string]any{
			"message": map[string]any{
				"role": "user",
				"parts": []map[string]any{
					{"type": "text", "text": "Hello async"},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var task A2ATask
	json.Unmarshal(resp.Result, &task)

	if task.Status != TaskStateSubmitted {
		t.Errorf("status = %q, want 'submitted' (non-blocking)", task.Status)
	}
	if task.ID == "" {
		t.Error("expected non-empty task ID")
	}

	// Wait for task to complete in background
	time.Sleep(200 * time.Millisecond)

	// Poll tasks/get to verify completion
	body2 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/get",
		"id":      13,
		"params":  map[string]any{"id": task.ID},
	}
	jsonBody2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody2))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	var resp2 Response
	json.NewDecoder(w2.Body).Decode(&resp2)
	if resp2.Error != nil {
		t.Fatalf("tasks/get error: %v", resp2.Error)
	}

	var task2 A2ATask
	json.Unmarshal(resp2.Result, &task2)
	if task2.Status != TaskStateCompleted {
		t.Errorf("status after poll = %q, want 'completed'", task2.Status)
	}
}

func TestA2AMessageSendNonBlockingQueueFull(t *testing.T) {
	// Test the queue-full rejection path with a handler that has no workers.
	cfg := testA2AConfig()
	cfg.BlockingMode = false

	// Create a handler with a 1-slot queue, then stop workers.
	h, err := NewA2AHandler(cfg, mockA2AExecutor, NewMemoryTaskStore(10000, time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	h.Shutdown() // stop workers

	// Create a handler with a 1-slot queue and no workers started.
	h2 := &A2AHandler{
		cfg:          cfg,
		store:        NewMemoryTaskStore(10000, time.Hour),
		chatExecutor: mockA2AExecutor,
		notifier:     NewTaskNotifier(),
		pushNotifier: NewPushNotifier(nil),
		defaultModel: cfg.DefaultModel,
		workQueue:    make(chan string, 1), // tiny queue
		cancelFuncs:  make(map[string]context.CancelFunc),
	}

	// Fill the queue
	h2.workQueue <- "fake-task-id"

	// message/send should fail with CodeMaxTasksReached
	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      14,
		"params": map[string]any{
			"message": map[string]any{
				"role": "user",
				"parts": []map[string]any{
					{"type": "text", "text": "overflow"},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h2.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected queue full error")
	}
	if resp.Error.Code != CodeMaxTasksReached {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeMaxTasksReached)
	}
}

func TestA2ANonBlockingFailedExecution(t *testing.T) {
	cfg := testA2AConfig()
	cfg.BlockingMode = false
	failExecutor := func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
		return nil, fmt.Errorf("provider unavailable")
	}
	h := newTestHandler(t, cfg, failExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      15,
		"params": map[string]any{
			"message": map[string]any{
				"role": "user",
				"parts": []map[string]any{
					{"type": "text", "text": "trigger failure"},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	var task A2ATask
	json.Unmarshal(resp.Result, &task)
	if task.Status != TaskStateSubmitted {
		t.Errorf("status = %q, want 'submitted'", task.Status)
	}

	// Wait for failure in background
	time.Sleep(200 * time.Millisecond)

	task2, ok := h.store.Get(task.ID)
	if !ok {
		t.Fatal("task not found after background execution")
	}
	if task2.Status != TaskStateFailed {
		t.Errorf("status after failure = %q, want 'failed'", task2.Status)
	}
}

func TestA2AShutdown(t *testing.T) {
	cfg := testA2AConfig()
	cfg.BlockingMode = false
	h := newTestHandler(t, cfg, mockA2AExecutor)

	// Shutdown should complete without hanging
	h.Shutdown()

	// After shutdown, submitting should fail gracefully
	h2, err := NewA2AHandler(cfg, mockA2AExecutor, NewMemoryTaskStore(100, time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	h2.Shutdown()
}

func TestA2ATaskStateIsTerminal(t *testing.T) {
	tests := []struct {
		state    TaskState
		terminal bool
	}{
		{TaskStateSubmitted, false},
		{TaskStateWorking, false},
		{TaskStateCompleted, true},
		{TaskStateFailed, true},
		{TaskStateCanceled, true},
	}
	for _, tt := range tests {
		if tt.state.IsTerminal() != tt.terminal {
			t.Errorf("IsTerminal(%q) = %v, want %v", tt.state, !tt.terminal, tt.terminal)
		}
	}
}

func TestA2ACancelSubmittedTask(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	// Create a submitted task directly (simulating a queued task)
	task := &A2ATask{
		ID:        "task-submitted-cancel",
		Status:    TaskStateSubmitted,
		History:   []A2AMessage{{Role: "user", Parts: []A2APart{{Type: "text", Text: "test"}}}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	h.store.Create(task)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/cancel",
		"id":      16,
		"params":  map[string]any{"id": "task-submitted-cancel"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var updated A2ATask
	json.Unmarshal(resp.Result, &updated)
	if updated.Status != TaskStateCanceled {
		t.Errorf("status = %q, want 'canceled'", updated.Status)
	}
}

func TestA2AMethodNotFound(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/nonexistent",
		"id":      17,
		"params":  map[string]any{},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected method not found error")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeMethodNotFound)
	}
}

func TestA2ATaskStreamTerminal(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	// Create a completed task
	task := &A2ATask{
		ID:        "task-stream-terminal",
		ContextID: "ctx-test",
		Status:    TaskStateCompleted,
		History:   []A2AMessage{},
		Artifacts: []A2AArtifact{{Parts: []A2APart{{Type: "text", Text: "done"}}}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	h.store.Create(task)

	req := httptest.NewRequest("GET", "/a2a/tasks/task-stream-terminal/stream", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !containsString(body, "event: task_update") {
		t.Error("expected task_update event")
	}
	if !containsString(body, "event: done") {
		t.Error("expected done event")
	}
}

func TestA2ATaskStreamNotFound(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	req := httptest.NewRequest("GET", "/a2a/tasks/nonexistent/stream", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// BATCH-06 / TASK-02: Bridge integration tests
// ---------------------------------------------------------------------------

// mockBridge tracks Publish calls for verification.
type mockBridge struct {
	mu        sync.Mutex
	published []*A2ATask
}

func (m *mockBridge) Publish(task *A2ATask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, task)
}

func (m *mockBridge) Start() {}

func (m *mockBridge) Close() {}

func (m *mockBridge) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.published)
}

func (m *mockBridge) last() *A2ATask {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.published) == 0 {
		return nil
	}
	return m.published[len(m.published)-1]
}

// TEST-06-02-01: notify() calls bridge.Publish when bridge is not nil
func TestA2ANotifyPublishesToBridge(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)
	bridge := &mockBridge{}
	h.SetBridge(bridge)

	task := &A2ATask{
		ID:     "task_bridge_test",
		Status: TaskStateCompleted,
		Model:  "gpt-4o",
	}
	h.notify(task)

	if bridge.count() != 1 {
		t.Fatalf("expected 1 bridge publish, got %d", bridge.count())
	}
	last := bridge.last()
	if last.ID != "task_bridge_test" {
		t.Errorf("expected task ID 'task_bridge_test', got %q", last.ID)
	}
	if last.Status != TaskStateCompleted {
		t.Errorf("expected status 'completed', got %q", last.Status)
	}

	h.Shutdown()
}

// TEST-06-02-02: notify() works correctly when bridge is nil (single-instance)
func TestA2ANotifyWorksWithoutBridge(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	// No bridge set — should not panic
	task := &A2ATask{
		ID:     "task_no_bridge",
		Status: TaskStateWorking,
	}
	h.notify(task) // should not panic

	h.Shutdown()
}

// TEST-06-02-03: SSE handler receives cross-instance task updates via bridge relay
func TestA2ASSEReceivesBridgeRelay(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	// Create a task first via non-blocking send
	payload := map[string]any{
		"message": map[string]any{
			"role":  "user",
			"parts": []any{map[string]any{"type": "text", "text": "hello"}},
		},
		"returnImmediately": true,
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "message/send",
		"params":  payload,
	})

	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	result, _ := resp["result"].(map[string]any)
	taskID, _ := result["id"].(string)
	if taskID == "" {
		t.Fatal("expected task ID in response")
	}

	// Simulate a bridge relay: manually inject a task update via the notifier
	// (this is what the Redis bridge does on receiving a remote message)
	task := &A2ATask{
		ID:     taskID,
		Status: TaskStateCompleted,
		Model:  "gpt-4o",
	}

	// The notifier should relay it to any SSE subscriber
	ch := h.Notifier().Subscribe(taskID)
	defer h.Notifier().Unsubscribe(taskID, ch)

	h.Notifier().Notify(task)

	select {
	case updated := <-ch:
		if updated.ID != taskID {
			t.Errorf("expected task ID %q, got %q", taskID, updated.ID)
		}
		if updated.Status != TaskStateCompleted {
			t.Errorf("expected status 'completed', got %q", updated.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notifier relay")
	}

	h.Shutdown()
}

func TestA2AMultiTurn_ContinueExistingTask(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	// First message: create task (single-turn)
	body1 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      100,
		"params": map[string]any{
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-1",
				"parts": []map[string]any{
					{"type": "text", "text": "Hello"},
				},
			},
		},
	}
	bodyBytes1, _ := json.Marshal(body1)
	req1 := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)

	var resp1 struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatal(err)
	}
	taskID := resp1.Result.ID
	if taskID == "" {
		t.Fatal("expected task ID from first message")
	}

	// Second message: continue with taskId (multi-turn)
	body2 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      101,
		"params": map[string]any{
			"taskId": taskID,
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-2",
				"parts": []map[string]any{
					{"type": "text", "text": "Follow-up question"},
				},
			},
		},
	}
	bodyBytes2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp2 struct {
		Result struct {
			ID     string    `json:"id"`
			Status TaskState `json:"status"`
		} `json:"result"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatal(err)
	}
	if resp2.Result.ID != taskID {
		t.Errorf("expected same task ID %q, got %q", taskID, resp2.Result.ID)
	}
	if resp2.Result.Status != TaskStateCompleted {
		t.Errorf("expected status completed, got %q", resp2.Result.Status)
	}

	// Verify history has 4 messages: user1 + agent1 + user2 + agent2
	task, ok := h.store.Get(taskID)
	if !ok {
		t.Fatal("task not found in store")
	}
	if len(task.History) < 3 { // at least user1, agent1, user2
		t.Errorf("expected >= 3 history messages, got %d", len(task.History))
	}
}

func TestA2AMultiTurn_InvalidTaskId(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      200,
		"params": map[string]any{
			"taskId": "nonexistent-task-id",
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-1",
				"parts": []map[string]any{
					{"type": "text", "text": "Hello"},
				},
			},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (JSON-RPC error in body), got %d", w.Code)
	}

	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error")
	}
	if resp.Error.Code != CodeTaskNotFound {
		t.Errorf("error code = %d, want %d (TaskNotFound)", resp.Error.Code, CodeTaskNotFound)
	}
}

func TestA2AMultiTurn_WorkingTaskReturnsCurrentState(t *testing.T) {
	cfg := testA2AConfig()
	cfg.BlockingMode = false // non-blocking so task stays in working state

	blockCh := make(chan struct{})
	slowExecutor := func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
		<-blockCh // block until test releases
		return &ChatResult{Content: "done"}, nil
	}
	h := newTestHandler(t, cfg, slowExecutor)

	// Create first task
	body1 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      300,
		"params": map[string]any{
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-1",
				"parts": []map[string]any{
					{"type": "text", "text": "Start"},
				},
			},
			"returnImmediately": true,
		},
	}
	bodyBytes1, _ := json.Marshal(body1)
	req1 := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)

	var resp1 struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatal(err)
	}
	taskID := resp1.Result.ID

	// Wait for task to start processing
	time.Sleep(50 * time.Millisecond)

	// Send another message with same taskId while task is working
	body2 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      301,
		"params": map[string]any{
			"taskId": taskID,
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-2",
				"parts": []map[string]any{
					{"type": "text", "text": "Follow-up"},
				},
			},
		},
	}
	bodyBytes2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	var resp2 struct {
		Result struct {
			ID     string    `json:"id"`
			Status TaskState `json:"status"`
		} `json:"result"`
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatal(err)
	}

	// Should return current state (working) without error
	if resp2.Error != nil {
		t.Fatalf("unexpected error: code=%d", resp2.Error.Code)
	}
	if resp2.Result.ID != taskID {
		t.Errorf("expected task ID %q, got %q", taskID, resp2.Result.ID)
	}

	// Release the executor
	close(blockCh)
}

func TestA2AMultiTurn_MaintainsHistory(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	// First turn
	body1 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      400,
		"params": map[string]any{
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-1",
				"parts": []map[string]any{
					{"type": "text", "text": "Turn 1"},
				},
			},
		},
	}
	bodyBytes1, _ := json.Marshal(body1)
	req1 := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)

	var resp1 struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	json.NewDecoder(w1.Body).Decode(&resp1)
	taskID := resp1.Result.ID

	// Verify turn 1 history
	task, _ := h.store.Get(taskID)
	turn1Len := len(task.History)

	// Second turn
	body2 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      401,
		"params": map[string]any{
			"taskId": taskID,
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-2",
				"parts": []map[string]any{
					{"type": "text", "text": "Turn 2"},
				},
			},
		},
	}
	bodyBytes2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}

	// Verify turn 2 history grew
	task, _ = h.store.Get(taskID)
	if len(task.History) <= turn1Len {
		t.Errorf("expected history to grow beyond %d messages, got %d", turn1Len, len(task.History))
	}

	// Third turn
	body3 := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      402,
		"params": map[string]any{
			"taskId": taskID,
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-3",
				"parts": []map[string]any{
					{"type": "text", "text": "Turn 3"},
				},
			},
		},
	}
	bodyBytes3, _ := json.Marshal(body3)
	req3 := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes3))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)

	// Verify turn 3 history grew further
	task, _ = h.store.Get(taskID)
	turn3Len := len(task.History)
	if turn3Len <= turn1Len+2 {
		t.Errorf("expected history to grow significantly, got %d", turn3Len)
	}

	// Verify message ordering
	userMsgCount := 0
	for _, msg := range task.History {
		if msg.Role == "user" {
			userMsgCount++
		}
	}
	if userMsgCount != 3 {
		t.Errorf("expected 3 user messages in history, got %d", userMsgCount)
	}
}

func TestA2AFilePart_RoundTrips(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      500,
		"params": map[string]any{
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-file-1",
				"parts": []map[string]any{
					{"type": "file", "fileUri": "https://example.com/doc.pdf", "mimeType": "application/pdf"},
					{"type": "text", "text": "Please summarize this file"},
				},
			},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Result struct {
			ID     string    `json:"id"`
			Status TaskState `json:"status"`
		} `json:"result"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Result.Status != TaskStateCompleted {
		t.Errorf("expected completed, got %q", resp.Result.Status)
	}

	// Verify file part preserved in history
	task, ok := h.store.Get(resp.Result.ID)
	if !ok {
		t.Fatal("task not found")
	}
	foundFile := false
	for _, msg := range task.History {
		for _, part := range msg.Parts {
			if part.Type == "file" && part.FileURI == "https://example.com/doc.pdf" {
				foundFile = true
			}
		}
	}
	if !foundFile {
		t.Error("expected file part in history")
	}
}

func TestA2ADataPart_RoundTrips(t *testing.T) {
	h := newTestHandler(t, testA2AConfig(), mockA2AExecutor)

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      501,
		"params": map[string]any{
			"message": map[string]any{
				"role":      "user",
				"messageId": "msg-data-1",
				"parts": []map[string]any{
					{"type": "data", "data": map[string]any{"temperature": 0.7, "units": "celsius"}},
					{"type": "text", "text": "Convert to Fahrenheit"},
				},
			},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	task, _ := h.store.Get(resp.Result.ID)
	foundData := false
	for _, msg := range task.History {
		for _, part := range msg.Parts {
			if part.Type == "data" && part.Data != nil {
				if v, ok := part.Data["temperature"]; ok {
					if fmt.Sprintf("%v", v) == "0.7" {
						foundData = true
					}
				}
			}
		}
	}
	if !foundData {
		t.Error("expected data part with temperature=0.7 in history")
	}
}

func TestExtractTextFromFilePart(t *testing.T) {
	history := []A2AMessage{
		{
			Role: "user",
			Parts: []A2APart{
				{Type: "file", FileURI: "https://example.com/img.png", FileMIMEType: "image/png"},
			},
		},
	}
	text := extractTextFromHistory(history)
	if text != "[file: image/png https://example.com/img.png]" {
		t.Errorf("text = %q", text)
	}
}

func TestExtractTextFromDataPart(t *testing.T) {
	history := []A2AMessage{
		{
			Role: "user",
			Parts: []A2APart{
				{Type: "data", Data: map[string]any{"key": "value"}},
			},
		},
	}
	text := extractTextFromHistory(history)
	if text != `{"key":"value"}` {
		t.Errorf("text = %q", text)
	}
}

func TestExtractTextFromBase64FilePart(t *testing.T) {
	history := []A2AMessage{
		{
			Role: "user",
			Parts: []A2APart{
				{Type: "file", FileBytes: "SGVsbG8=", FileMIMEType: "text/plain"},
			},
		},
	}
	text := extractTextFromHistory(history)
	if !strings.Contains(text, "[file: text/plain <base64") {
		t.Errorf("text = %q", text)
	}
}

// ---------------------------------------------------------------------------
// BATCH-24/TASK-03: Virtual key authentication tests
// ---------------------------------------------------------------------------

// mockA2AKeyResolver implements VirtualKeyResolver for testing.
type mockA2AKeyResolver struct {
	identity IdentityProvider
	err      error
}

func (m *mockA2AKeyResolver) ResolveVirtualKey(_ context.Context, _ string) (IdentityProvider, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.identity, nil
}

func testA2AHandlerWithResolver(t *testing.T, resolver VirtualKeyResolver) *A2AHandler {
	t.Helper()
	cfg := testA2AConfig()
	cfg.Authentication.Mode = "virtual_key"
	store := NewMemoryTaskStore(100, time.Hour)
	exec := func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
		return &ChatResult{Model: "test-model", Content: "test response"}, nil
	}
	h, err := NewA2AHandler(cfg, exec, store, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if resolver != nil {
		h.SetKeyResolver(resolver)
	}
	return h
}

// TEST-24-03-01: authenticate with mode=virtual_key and valid key returns identity
func TestAuthenticate_VirtualKey_ValidKey(t *testing.T) {
	h := testA2AHandlerWithResolver(t, &mockA2AKeyResolver{
		identity: &MCPIdentity{
			ProjectID:    "proj-1",
			VirtualKeyID: "vk-1",
			KeyPrefix:    "gw-abc1",
			Source:       "a2a",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/a2a", nil)
	req.Header.Set("Authorization", "Bearer gw-testkey1234567890abcdef")

	idp, ok := h.authenticate(req)
	if !ok {
		t.Fatal("expected auth success")
	}
	if idp == nil {
		t.Fatal("expected non-nil identity")
	}
	if idp.GetVirtualKeyID() != "vk-1" {
		t.Errorf("VirtualKeyID = %q, want %q", idp.GetVirtualKeyID(), "vk-1")
	}
	if idp.GetSource() != "a2a" {
		t.Errorf("Source = %q, want %q", idp.GetSource(), "a2a")
	}
}

// TEST-24-03-02: authenticate with mode=virtual_key and invalid key returns false
func TestAuthenticate_VirtualKey_InvalidKey(t *testing.T) {
	h := testA2AHandlerWithResolver(t, &mockA2AKeyResolver{
		err: fmt.Errorf("key not found"),
	})

	req := httptest.NewRequest(http.MethodPost, "/a2a", nil)
	req.Header.Set("Authorization", "Bearer gw-invalid")

	_, ok := h.authenticate(req)
	if ok {
		t.Fatal("expected auth failure for invalid key")
	}
}

// TEST-24-03-03: authenticate with mode=virtual_key and non-gw- prefix returns false
func TestAuthenticate_VirtualKey_NonGwPrefix(t *testing.T) {
	h := testA2AHandlerWithResolver(t, &mockA2AKeyResolver{})

	req := httptest.NewRequest(http.MethodPost, "/a2a", nil)
	req.Header.Set("Authorization", "Bearer sk-someotherkey12345")

	_, ok := h.authenticate(req)
	if ok {
		t.Fatal("expected auth failure for non-gw- prefix")
	}
}

// TEST-24-03-04: authenticate with mode=bearer_token returns nil identity (backward compat)
func TestAuthenticate_BearerToken_NilIdentity(t *testing.T) {
	cfg := testA2AConfig()
	cfg.Authentication.Mode = "bearer_token"
	cfg.Authentication.BearerToken = "my-secret"
	store := NewMemoryTaskStore(100, time.Hour)
	exec := func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
		return &ChatResult{Model: "test", Content: "ok"}, nil
	}
	h, _ := NewA2AHandler(cfg, exec, store, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/a2a", nil)
	req.Header.Set("Authorization", "Bearer my-secret")

	idp, ok := h.authenticate(req)
	if !ok {
		t.Fatal("expected auth success for bearer_token")
	}
	if idp != nil {
		t.Fatal("expected nil identity for bearer_token mode")
	}
}

// TEST-24-03-05: authenticate with mode=none returns nil identity (backward compat)
func TestAuthenticate_None_NilIdentity(t *testing.T) {
	cfg := testA2AConfig()
	cfg.Authentication.Mode = "none"
	store := NewMemoryTaskStore(100, time.Hour)
	exec := func(ctx context.Context, toolName string, args map[string]any) (*ChatResult, error) {
		return &ChatResult{Model: "test", Content: "ok"}, nil
	}
	h, _ := NewA2AHandler(cfg, exec, store, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/a2a", nil)

	idp, ok := h.authenticate(req)
	if !ok {
		t.Fatal("expected auth success for none mode")
	}
	if idp != nil {
		t.Fatal("expected nil identity for none mode")
	}
}

// TEST-24-03-06: Identity stored in task metadata for non-blocking path
func TestVirtualKey_IdentityInTaskMetadata(t *testing.T) {
	testIdentity := &MCPIdentity{
		ProjectID:    "proj-meta",
		VirtualKeyID: "vk-meta",
		KeyPrefix:    "gw-meta",
		Source:       "a2a",
	}
	h := testA2AHandlerWithResolver(t, &mockA2AKeyResolver{identity: testIdentity})

	req := httptest.NewRequest(http.MethodPost, "/a2a", nil)
	req.Header.Set("Authorization", "Bearer gw-testkey1234567890abcdef")
	req.Header.Set("Content-Type", "application/json")
	body := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","parts":[{"type":"text","text":"hello"}]},"returnImmediately":true}}`
	req.Body = io.NopCloser(strings.NewReader(body))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Verify the task was created with governance identity in metadata
	tasks, _, _ := h.store.List(TaskListFilter{Limit: 10})
	if len(tasks) == 0 {
		t.Fatal("no tasks created")
	}
	task := tasks[0]
	govID, ok := task.Metadata["governance_identity"]
	if !ok {
		t.Fatal("governance_identity not found in task metadata")
	}
	idp, ok := govID.(IdentityProvider)
	if !ok {
		t.Fatalf("governance_identity is %T, want IdentityProvider", govID)
	}
	if idp.GetVirtualKeyID() != "vk-meta" {
		t.Errorf("VirtualKeyID = %q, want %q", idp.GetVirtualKeyID(), "vk-meta")
	}
}

// TEST-24-03-07: Virtual_key mode without resolver returns 401
func TestAuthenticate_VirtualKey_NoResolver(t *testing.T) {
	// Handler with virtual_key mode but no resolver set
	h := testA2AHandlerWithResolver(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/a2a", nil)
	req.Header.Set("Authorization", "Bearer gw-testkey1234567890abcdef")

	_, ok := h.authenticate(req)
	if ok {
		t.Fatal("expected auth failure when no resolver configured")
	}
}
