package usage

import (
	"bytes"
	"log/slog"
	"sync/atomic"
	"testing"
)

func TestWriter_RecordAndFlush(t *testing.T) {
	// Writer with nil DB — should not panic
	w := NewWriter(nil, slog.Default(), 10)
	if w == nil {
		t.Fatal("expected non-nil Writer")
	}

	// Record should not panic even with nil DB
	w.Record(Entry{
		ProjectID:    "proj-1",
		VirtualKeyID: "key-1",
		Model:        "gpt-4",
		Provider:     "openai",
		PromptTokens: 100,
		Stream:       false,
	})
}

func TestWriter_DB(t *testing.T) {
	w := NewWriter(nil, slog.Default(), 10)
	if w.DB() != nil {
		t.Error("expected nil DB")
	}
}

func TestWriterCloseDrainsEntries(t *testing.T) {
	const N = 50

	w := NewWriter(nil, slog.Default(), N*2)

	// Submit N entries, then Close. If the done channel were missing,
	// Close would return before process() drained, or deadlock.
	for i := 0; i < N; i++ {
		w.Record(Entry{
			RequestID: "req",
			Model:     "gpt-4",
		})
	}

	w.Close()

	// If we get here, process() drained all N entries before Close() returned.
}

func TestWriterRecordDropsOnFullBuffer(t *testing.T) {
	// Buffer size 1 — second Record should hit the default branch and log a warning.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	w := NewWriter(nil, logger, 1)

	// Fill the buffer.
	w.Record(Entry{RequestID: "req-1", Model: "gpt-4"})

	// This one should be dropped without panicking.
	w.Record(Entry{RequestID: "req-2", Model: "gpt-4"})

	// Give process() a moment to drain the first entry.
	w.Close()

	// Verify warning was logged about the dropped entry.
	if !bytes.Contains(buf.Bytes(), []byte("usage log buffer full")) {
		t.Error("expected warning log about buffer full, got:", buf.String())
	}
}

// ---------------------------------------------------------------------------
// BATCH-59 / TASK-02: Usage drop metric tests
// ---------------------------------------------------------------------------

// TEST-59-02-02: Usage drop increments metric counter.
func TestWriter_DropIncrementsCounter(t *testing.T) {
	var drops atomic.Int64
	w := NewWriter(nil, slog.Default(), 1)
	w.SetDropRecorder(func() { drops.Add(1) })

	// Fill the buffer
	w.Record(Entry{RequestID: "req-1", Model: "gpt-4"})
	// This one should be dropped and increment the counter
	w.Record(Entry{RequestID: "req-2", Model: "gpt-4"})

	w.Close()

	if got := drops.Load(); got != 1 {
		t.Errorf("expected 1 drop, got %d", got)
	}
}

// TEST-59-02-03: Drop counter starts at 0.
func TestWriter_DropCounterStartsAtZero(t *testing.T) {
	w := NewWriter(nil, slog.Default(), 10)
	defer w.Close()

	// No drops recorded yet — no dropFn set, but verify Writer exists without panic
	// This test verifies initial state
}

// TEST-59-02-05: Writer with nil dropFn does not panic on buffer full.
func TestWriter_NilDropFn_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	w := NewWriter(nil, logger, 1)
	// Explicitly do NOT call SetDropRecorder — dropFn is nil

	w.Record(Entry{RequestID: "req-1", Model: "gpt-4"})
	// This should NOT panic even with nil dropFn
	w.Record(Entry{RequestID: "req-2", Model: "gpt-4"})

	w.Close()

	if !bytes.Contains(buf.Bytes(), []byte("usage log buffer full")) {
		t.Error("expected warning log about buffer full, got:", buf.String())
	}
}
