package migrate

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed sql/*.sql
var fs embed.FS

// Run applies all pending SQL migrations to the database.
// Migrations are embedded SQL files sorted by filename.
// A migrations tracking table ensures each file runs only once.
func Run(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS _migrations (
			filename TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := fs.ReadDir("sql")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err := db.QueryRow(
			"SELECT EXISTS(SELECT 1 FROM _migrations WHERE filename = $1)", name,
		).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}

		content, err := fs.ReadFile("sql/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO _migrations (filename) VALUES ($1)", name,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}
