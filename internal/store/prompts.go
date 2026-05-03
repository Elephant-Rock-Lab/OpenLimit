package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

// PromptTemplate represents a stored prompt template.
type PromptTemplate struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Content     string    `json:"content"`
	Description string    `json:"description"`
	CreatedAt   string    `json:"created_at"`
	UpdatedAt   string    `json:"updated_at"`
}

// CreatePromptTemplate inserts a new prompt template.
func CreatePromptTemplate(ctx context.Context, db *sql.DB, pt *PromptTemplate) error {
	if pt.ID == "" {
		pt.ID = generateID()
	}
	now := time.Now().UTC()
	pt.CreatedAt = now.Format(time.RFC3339)
	pt.UpdatedAt = now.Format(time.RFC3339)

	_, err := db.ExecContext(ctx,
		`INSERT INTO prompt_templates (id, name, content, description, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		pt.ID, pt.Name, pt.Content, pt.Description, now, now,
	)
	return err
}

// ListPromptTemplates returns all prompt templates.
func ListPromptTemplates(ctx context.Context, db *sql.DB) ([]PromptTemplate, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, content, description, created_at, updated_at
		 FROM prompt_templates ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PromptTemplate
	for rows.Next() {
		var pt PromptTemplate
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Name, &pt.Content, &pt.Description, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		pt.CreatedAt = createdAt.Format(time.RFC3339)
		pt.UpdatedAt = updatedAt.Format(time.RFC3339)
		result = append(result, pt)
	}
	if result == nil {
		result = []PromptTemplate{}
	}
	return result, nil
}

// GetPromptTemplate retrieves a single prompt template by ID.
func GetPromptTemplate(ctx context.Context, db *sql.DB, id string) (*PromptTemplate, error) {
	var pt PromptTemplate
	var createdAt, updatedAt time.Time
	err := db.QueryRowContext(ctx,
		`SELECT id, name, content, description, created_at, updated_at
		 FROM prompt_templates WHERE id = $1`, id,
	).Scan(&pt.ID, &pt.Name, &pt.Content, &pt.Description, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pt.CreatedAt = createdAt.Format(time.RFC3339)
	pt.UpdatedAt = updatedAt.Format(time.RFC3339)
	return &pt, nil
}

// UpdatePromptTemplate updates a prompt template.
func UpdatePromptTemplate(ctx context.Context, db *sql.DB, pt *PromptTemplate) error {
	now := time.Now().UTC()
	pt.UpdatedAt = now.Format(time.RFC3339)

	res, err := db.ExecContext(ctx,
		`UPDATE prompt_templates SET name = $1, content = $2, description = $3, updated_at = $4
		 WHERE id = $5`,
		pt.Name, pt.Content, pt.Description, now, pt.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeletePromptTemplate removes a prompt template by ID.
func DeletePromptTemplate(ctx context.Context, db *sql.DB, id string) (bool, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM prompt_templates WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func generateID() string {
	return "pt_" + randomHex(12)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}
