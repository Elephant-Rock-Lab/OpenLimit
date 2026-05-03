package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"
)

// Logger asynchronously writes audit events to the database.
// When db is nil, all methods are no-ops.
type Logger struct {
	db     *sql.DB
	events chan Event
	logger *slog.Logger
}

// NewLogger creates a new audit logger with the given buffer size.
// If db is nil, returns a no-op logger.
func NewLogger(db *sql.DB, logger *slog.Logger, bufferSize int) *Logger {
	if db == nil {
		return &Logger{logger: logger}
	}
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	l := &Logger{
		db:     db,
		events: make(chan Event, bufferSize),
		logger: logger,
	}
	go l.process()
	return l
}

// Record enqueues an audit event for async writing.
// If the buffer is full, the event is dropped and a warning is logged.
func (l *Logger) Record(e Event) {
	if l == nil || l.db == nil {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	select {
	case l.events <- e:
	default:
		l.logger.Warn("audit log buffer full, dropping event",
			"event_type", e.EventType,
			"actor", e.Actor,
			"resource", e.Resource,
		)
	}
}

// RecordSync writes an audit event synchronously. Use for startup/shutdown
// events where the async channel may not be drained in time.
func (l *Logger) RecordSync(e Event) error {
	if l == nil || l.db == nil {
		return nil
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	return l.write(e)
}

// Close stops the logger and flushes pending events.
func (l *Logger) Close() {
	if l == nil || l.events == nil {
		return
	}
	close(l.events)
}

func (l *Logger) process() {
	for e := range l.events {
		if err := l.write(e); err != nil {
			l.logger.Error("failed to write audit event",
				"event_type", e.EventType,
				"error", err,
			)
		}
	}
}

func (l *Logger) write(e Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metaJSON, err := json.Marshal(e.Metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	_, err = l.db.ExecContext(ctx,
		`INSERT INTO audit_logs
			(timestamp, event_type, actor, action, resource, outcome, request_id, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.Timestamp, e.EventType, e.Actor, e.Action, e.Resource,
		e.Outcome, e.RequestID, string(metaJSON),
	)
	return err
}
