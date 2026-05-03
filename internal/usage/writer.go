package usage

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// Entry represents a single usage log record.
type Entry struct {
	RequestID        string
	ProjectID        string
	VirtualKeyID     string
	Model            string
	Provider         string
	ProviderModel    string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
	CacheHit         bool
	Stream           bool
	Attempts         int
	DurationMS       int64
	Error            string
}

// Writer asynchronously writes usage log entries to the database.
type Writer struct {
	db     *sql.DB
	logs   chan Entry
	logger *slog.Logger
}

// DB returns the underlying database connection.
// Used for budget queries that need direct DB access.
func (w *Writer) DB() *sql.DB {
	if w == nil {
		return nil
	}
	return w.db
}

// NewWriter creates a new usage writer with the given buffer size.
func NewWriter(db *sql.DB, logger *slog.Logger, bufferSize int) *Writer {
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	w := &Writer{
		db:     db,
		logs:   make(chan Entry, bufferSize),
		logger: logger,
	}
	go w.process()
	return w
}

// Record queues a usage entry for async writing.
func (w *Writer) Record(entry Entry) {
	if w == nil {
		return
	}
	select {
	case w.logs <- entry:
	default:
		w.logger.Warn("usage log buffer full, dropping entry",
			"request_id", entry.RequestID,
		)
	}
}

// Close stops the writer and flushes pending entries.
func (w *Writer) Close() {
	if w == nil {
		return
	}
	close(w.logs)
}

func (w *Writer) process() {
	for entry := range w.logs {
		if err := w.write(entry); err != nil {
			w.logger.Error("failed to write usage log",
				"request_id", entry.RequestID,
				"error", err,
			)
		}
	}
}

func (w *Writer) write(e Entry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := w.db.ExecContext(ctx,
		`INSERT INTO usage_logs
			(request_id, project_id, virtual_key_id, model, provider, provider_model,
			 prompt_tokens, completion_tokens, total_tokens, cost_usd,
			 cache_hit, stream, attempts, duration_ms, error, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		e.RequestID, nullableString(e.ProjectID), nullableString(e.VirtualKeyID),
		e.Model, e.Provider, e.ProviderModel,
		e.PromptTokens, e.CompletionTokens, e.TotalTokens, e.CostUSD,
		e.CacheHit, e.Stream, e.Attempts, e.DurationMS, e.Error, time.Now(),
	)
	return err
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// GetSpendForCurrentPeriod returns the total spend for a virtual key in the current period.
func GetSpendForCurrentPeriod(ctx context.Context, db *sql.DB, virtualKeyID string, period string) (float64, error) {
	var trunc string
	switch period {
	case "daily":
		trunc = "day"
	default:
		trunc = "month"
	}

	var total float64
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM usage_logs
		 WHERE virtual_key_id = $1
		   AND created_at >= date_trunc($2, now())`,
		virtualKeyID, trunc,
	).Scan(&total)
	return total, err
}
