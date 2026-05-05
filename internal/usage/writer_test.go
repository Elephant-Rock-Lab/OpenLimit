package usage

import (
	"log/slog"
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
