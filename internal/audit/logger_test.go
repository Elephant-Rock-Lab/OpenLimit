package audit

import (
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestNoOpLogger(t *testing.T) {
	// Nil db → no-op logger
	l := NewLogger(nil, slog.Default(), 100)
	l.Record(Event{EventType: "test"})
	l.RecordSync(Event{EventType: "test"})
	l.Close()
	// No panic = pass
}

func TestRecordSyncWrites(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	l := NewLogger(db, slog.Default(), 100)

	err := l.RecordSync(Event{
		EventType: EventProjectCreate,
		Actor:     "admin",
		Action:    "create",
		Resource:  "project:test_1",
		Outcome:   "success",
		RequestID: "req-001",
		Metadata:  map[string]any{"name": "test-project"},
	})
	if err != nil {
		t.Fatalf("RecordSync: %v", err)
	}

	// Verify the row
	var eventType, actor, resource string
	err = db.QueryRow(
		"SELECT event_type, actor, resource FROM audit_logs WHERE request_id = $1",
		"req-001",
	).Scan(&eventType, &actor, &resource)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if eventType != EventProjectCreate {
		t.Errorf("event_type = %q, want %q", eventType, EventProjectCreate)
	}
	if actor != "admin" {
		t.Errorf("actor = %q, want %q", actor, "admin")
	}
	if resource != "project:test_1" {
		t.Errorf("resource = %q, want %q", resource, "project:test_1")
	}
}

func TestRecordAsyncWrites(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	l := NewLogger(db, slog.Default(), 100)

	l.Record(Event{
		EventType: EventKeyRevoke,
		Actor:     "admin",
		Action:    "revoke",
		Resource:  "key:vk_abc123",
		Outcome:   "success",
		RequestID: "req-002",
	})
	l.Close() // flush

	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM audit_logs WHERE request_id = $1",
		"req-002",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestTimestampAutoSet(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	l := NewLogger(db, slog.Default(), 100)
	before := time.Now()

	err := l.RecordSync(Event{
		EventType: "test",
		Actor:     "system",
		Action:    "test",
		Resource:  "test:auto_ts",
		Outcome:   "success",
	})
	if err != nil {
		t.Fatalf("RecordSync: %v", err)
	}

	var ts time.Time
	err = db.QueryRow(
		"SELECT timestamp FROM audit_logs WHERE resource = $1",
		"test:auto_ts",
	).Scan(&ts)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if ts.Before(before) {
		t.Errorf("timestamp %v before event creation %v", ts, before)
	}
}

func TestTimestampPreserved(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	l := NewLogger(db, slog.Default(), 100)
	fixed := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	err := l.RecordSync(Event{
		Timestamp: fixed,
		EventType: "test",
		Actor:     "system",
		Action:    "test",
		Resource:  "test:fixed_ts",
		Outcome:   "success",
	})
	if err != nil {
		t.Fatalf("RecordSync: %v", err)
	}

	var ts time.Time
	err = db.QueryRow(
		"SELECT timestamp FROM audit_logs WHERE resource = $1",
		"test:fixed_ts",
	).Scan(&ts)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// Compare without timezone offset issues
	if ts.Unix() != fixed.Unix() {
		t.Errorf("timestamp = %v, want %v", ts, fixed)
	}
}

func TestMetadataJSON(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	l := NewLogger(db, slog.Default(), 100)

	err := l.RecordSync(Event{
		EventType: "test",
		Actor:     "system",
		Action:    "test",
		Resource:  "test:metadata",
		Outcome:   "success",
		Metadata: map[string]any{
			"key_count": 3,
			"provider":  "openai",
		},
	})
	if err != nil {
		t.Fatalf("RecordSync: %v", err)
	}

	var meta string
	err = db.QueryRow(
		"SELECT metadata::text FROM audit_logs WHERE resource = $1",
		"test:metadata",
	).Scan(&meta)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !containsAll(meta, `"key_count":3`, `"provider":"openai"`) {
		t.Errorf("metadata = %q, want key_count and provider", meta)
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findSub(s, sub))
}

func findSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := testDBURL()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("cannot ping postgres: %v", err)
	}
	// Clean up audit_logs table for test isolation
	db.Exec("DELETE FROM audit_logs")
	return db
}

func testDBURL() string {
	// Try standard env vars
	for _, env := range []string{"TEST_DATABASE_URL", "DATABASE_URL"} {
		if u := getEnv(env); u != "" {
			return u
		}
	}
	return "postgres://openlimit:openlimit@localhost:5432/openlimit_test?sslmode=disable"
}

func getEnv(key string) string {
	return os.Getenv(key)
}
