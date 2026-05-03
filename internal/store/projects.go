package store

import (
	"context"
	"time"
)

// Project represents a tenant in the gateway.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateProject inserts a new project and returns it with the generated ID.
func CreateProject(ctx context.Context, db Queryer, name string) (*Project, error) {
	p := &Project{Name: name}
	err := db.QueryRowContext(ctx,
		"INSERT INTO projects (name) VALUES ($1) RETURNING id, created_at",
		name,
	).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ListProjects returns all projects ordered by name.
func ListProjects(ctx context.Context, db Queryer) ([]Project, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, name, created_at FROM projects ORDER BY name",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// DeleteProject deletes a project by ID. Returns true if a row was deleted.
func DeleteProject(ctx context.Context, db Queryer, id string) (bool, error) {
	res, err := db.ExecContext(ctx, "DELETE FROM projects WHERE id = $1", id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetProject fetches a project by ID.
func GetProject(ctx context.Context, db Queryer, id string) (*Project, error) {
	p := &Project{}
	err := db.QueryRowContext(ctx,
		"SELECT id, name, created_at FROM projects WHERE id = $1", id,
	).Scan(&p.ID, &p.Name, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}
