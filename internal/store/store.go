package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"openlimit/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ErrNotFound is returned when a query expects a row but finds none.
var ErrNotFound = fmt.Errorf("not found")

// Queryer is the common interface for *sql.DB and *sql.Tx.
type Queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Open creates a connection pool to Postgres using the config.
func Open(cfg config.DatabaseConfig) (*sql.DB, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database.url is required")
	}

	db, err := sql.Open("pgx", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
