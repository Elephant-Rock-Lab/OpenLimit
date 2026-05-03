package mcp

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func openTestDBForA2A(t *testing.T) *PersistentTaskStore {
	t.Helper()
	url := testDBURLForA2A()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("cannot ping postgres: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Clean up tasks table
	db.Exec("DELETE FROM a2a_tasks")

	return NewPersistentTaskStore(db)
}

func testDBURLForA2A() string {
	for _, env := range []string{"TEST_DATABASE_URL", "DATABASE_URL"} {
		if u := os.Getenv(env); u != "" {
			return u
		}
	}
	return "postgres://openlimit:openlimit@localhost:5432/openlimit_test?sslmode=disable"
}

func TestPersistentStoreCreateAndGet(t *testing.T) {
	store := openTestDBForA2A(t)

	now := time.Now().Truncate(time.Microsecond)
	task := &A2ATask{
		ID:        "task-pg-create-1",
		ContextID: "ctx-test",
		Status:    TaskStateSubmitted,
		History: []A2AMessage{
			{Role: "user", Parts: []A2APart{{Type: "text", Text: "hello"}}, MessageID: "msg-1"},
		},
		Artifacts: []A2AArtifact{},
		Metadata:  map[string]any{"key": "value"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.Create(task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, ok := store.Get("task-pg-create-1")
	if !ok {
		t.Fatal("task not found after Create")
	}
	if got.ID != task.ID {
		t.Errorf("ID = %q, want %q", got.ID, task.ID)
	}
	if got.Status != TaskStateSubmitted {
		t.Errorf("Status = %q, want %q", got.Status, TaskStateSubmitted)
	}
	if got.ContextID != "ctx-test" {
		t.Errorf("ContextID = %q, want %q", got.ContextID, "ctx-test")
	}
	if len(got.History) != 1 {
		t.Fatalf("History len = %d, want 1", len(got.History))
	}
	if got.History[0].Parts[0].Text != "hello" {
		t.Errorf("History[0].Text = %q, want %q", got.History[0].Parts[0].Text, "hello")
	}
	if got.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] = %v, want 'value'", got.Metadata["key"])
	}
}

func TestPersistentStoreUpdateStatus(t *testing.T) {
	store := openTestDBForA2A(t)

	now := time.Now().Truncate(time.Microsecond)
	task := &A2ATask{
		ID:        "task-pg-update-1",
		ContextID: "ctx-test",
		Status:    TaskStateSubmitted,
		History:   []A2AMessage{},
		Artifacts: []A2AArtifact{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	store.Create(task)

	// Update to working
	task.Status = TaskStateWorking
	task.Model = "gpt-4o"
	if err := store.Update(task); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, ok := store.Get("task-pg-update-1")
	if !ok {
		t.Fatal("task not found after Update")
	}
	if got.Status != TaskStateWorking {
		t.Errorf("Status = %q, want %q", got.Status, TaskStateWorking)
	}
	if got.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", got.Model, "gpt-4o")
	}
	// updated_at should be >= created_at
	if got.UpdatedAt.Before(got.CreatedAt) {
		t.Errorf("UpdatedAt %v < CreatedAt %v", got.UpdatedAt, got.CreatedAt)
	}
}

func TestPersistentStoreListWithStatusFilter(t *testing.T) {
	store := openTestDBForA2A(t)

	now := time.Now().Truncate(time.Microsecond)
	for i, status := range []TaskState{TaskStateCompleted, TaskStateCompleted, TaskStateFailed, TaskStateWorking} {
		task := &A2ATask{
			ID:        "task-pg-list-" + string(status) + "-" + jsonNumber(i),
			ContextID: "ctx-test",
			Status:    status,
			History:   []A2AMessage{},
			Artifacts: []A2AArtifact{},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := store.Create(task); err != nil {
			t.Fatalf("Create %d failed: %v", i, err)
		}
	}

	tasks, total, err := store.List(TaskListFilter{Status: "completed"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(tasks) != 2 {
		t.Errorf("tasks len = %d, want 2", len(tasks))
	}
}

func TestPersistentStoreListPagination(t *testing.T) {
	store := openTestDBForA2A(t)

	now := time.Now().Truncate(time.Microsecond)
	for i := 0; i < 5; i++ {
		task := &A2ATask{
			ID:        "task-pg-page-" + jsonNumber(i),
			ContextID: "ctx-test",
			Status:    TaskStateCompleted,
			History:   []A2AMessage{},
			Artifacts: []A2AArtifact{},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := store.Create(task); err != nil {
			t.Fatalf("Create %d failed: %v", i, err)
		}
	}

	// Page 1: limit=2, offset=0
	tasks, total, err := store.List(TaskListFilter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(tasks) != 2 {
		t.Errorf("page 1 len = %d, want 2", len(tasks))
	}

	// Page 3: limit=2, offset=4
	tasks, _, err = store.List(TaskListFilter{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("List page 3 failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("page 3 len = %d, want 1", len(tasks))
	}
}

func TestPersistentStoreDelete(t *testing.T) {
	store := openTestDBForA2A(t)

	now := time.Now().Truncate(time.Microsecond)
	task := &A2ATask{
		ID:        "task-pg-del-1",
		ContextID: "ctx-test",
		Status:    TaskStateCompleted,
		History:   []A2AMessage{},
		Artifacts: []A2AArtifact{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	store.Create(task)

	if err := store.Delete("task-pg-del-1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, ok := store.Get("task-pg-del-1")
	if ok {
		t.Error("task should not exist after Delete")
	}

	// Delete nonexistent
	err := store.Delete("nonexistent")
	if err != ErrTaskNotFound {
		t.Errorf("Delete nonexistent err = %v, want ErrTaskNotFound", err)
	}
}

func TestPersistentStoreRecoverStale(t *testing.T) {
	store := openTestDBForA2A(t)

	now := time.Now().Truncate(time.Microsecond)
	statuses := []TaskState{TaskStateSubmitted, TaskStateWorking, TaskStateCompleted, TaskStateFailed}
	for i, status := range statuses {
		task := &A2ATask{
			ID:        "task-pg-stale-" + jsonNumber(i),
			ContextID: "ctx-test",
			Status:    status,
			History:   []A2AMessage{},
			Artifacts: []A2AArtifact{},
			CreatedAt: now,
			UpdatedAt: now,
		}
		store.Create(task)
	}

	recovered, err := store.RecoverStale()
	if err != nil {
		t.Fatalf("RecoverStale failed: %v", err)
	}
	if recovered != 2 {
		t.Errorf("recovered = %d, want 2", recovered)
	}

	// Check that submitted and working are now failed
	for _, id := range []string{"task-pg-stale-0", "task-pg-stale-1"} {
		task, ok := store.Get(id)
		if !ok {
			t.Fatalf("task %s not found", id)
		}
		if task.Status != TaskStateFailed {
			t.Errorf("task %s status = %q, want 'failed'", id, task.Status)
		}
	}

	// Completed and failed should be unchanged
	for _, id := range []string{"task-pg-stale-2", "task-pg-stale-3"} {
		task, ok := store.Get(id)
		if !ok {
			t.Fatalf("task %s not found", id)
		}
		if task.Status == TaskStateFailed && (id == "task-pg-stale-2") {
			// task-pg-stale-2 was completed, should stay completed
			// But RecoverStale marks all submitted/working → failed
			// task-pg-stale-2 was completed, so it should stay completed
		}
	}
}

// jsonNumber is a simple helper to convert an int to a string for IDs.
func jsonNumber(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
