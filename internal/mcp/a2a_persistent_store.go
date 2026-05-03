package mcp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// PersistentTaskStore stores A2A tasks in Postgres.
type PersistentTaskStore struct {
	db *sql.DB
}

// NewPersistentTaskStore creates a new Postgres-backed task store.
func NewPersistentTaskStore(db *sql.DB) *PersistentTaskStore {
	return &PersistentTaskStore{db: db}
}

// Close is a no-op; the db connection is managed externally.
func (s *PersistentTaskStore) Close() {}

// Create inserts a new task into the database.
func (s *PersistentTaskStore) Create(task *A2ATask) error {
	history, err := json.Marshal(task.History)
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	artifacts, err := json.Marshal(task.Artifacts)
	if err != nil {
		return fmt.Errorf("marshal artifacts: %w", err)
	}
	metadata, err := json.Marshal(task.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	var statusMsg []byte
	if task.StatusMessage != nil {
		statusMsg, err = json.Marshal(task.StatusMessage)
		if err != nil {
			return fmt.Errorf("marshal status_message: %w", err)
		}
	}

	_, err = s.db.Exec(`
		INSERT INTO a2a_tasks (id, context_id, status, history, artifacts, metadata, status_message, model, push_config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, $9, $9)`,
		task.ID, task.ContextID, string(task.Status),
		history, artifacts, metadata, statusMsg, task.Model,
		task.CreatedAt,
	)
	if err != nil {
		// Check for unique violation (duplicate ID)
		if isDuplicateKeyError(err) {
			return ErrDuplicateTask
		}
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

// Get retrieves a task by ID.
func (s *PersistentTaskStore) Get(id string) (*A2ATask, bool) {
	row := s.db.QueryRow(`
		SELECT id, context_id, status, history, artifacts, metadata, status_message, model, created_at, updated_at
		FROM a2a_tasks WHERE id = $1`, id)

	task, err := s.scanTask(row)
	if err != nil {
		return nil, false
	}
	return task, true
}

// Update updates a task in the database.
func (s *PersistentTaskStore) Update(task *A2ATask) error {
	history, err := json.Marshal(task.History)
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	artifacts, err := json.Marshal(task.Artifacts)
	if err != nil {
		return fmt.Errorf("marshal artifacts: %w", err)
	}
	metadata, err := json.Marshal(task.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	var statusMsg []byte
	if task.StatusMessage != nil {
		statusMsg, err = json.Marshal(task.StatusMessage)
		if err != nil {
			return fmt.Errorf("marshal status_message: %w", err)
		}
	}

	res, err := s.db.Exec(`
		UPDATE a2a_tasks
		SET status = $1, history = $2, artifacts = $3, metadata = $4, status_message = $5, model = $6, updated_at = NOW()
		WHERE id = $7`,
		string(task.Status), history, artifacts, metadata, statusMsg, task.Model, task.ID,
	)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// List returns filtered tasks with total count.
func (s *PersistentTaskStore) List(filter TaskListFilter) ([]*A2ATask, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	// Build WHERE clause
	where := "WHERE 1=1"
	args := []any{}
	idx := 1
	if filter.Status != "" {
		where += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, filter.Status)
		idx++
	}
	if filter.ContextID != "" {
		where += fmt.Sprintf(" AND context_id = $%d", idx)
		args = append(args, filter.ContextID)
		idx++
	}

	// Count query
	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM a2a_tasks %s", where)
	if err := s.db.QueryRow(countQ, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}

	// Data query
	query := fmt.Sprintf(`
		SELECT id, context_id, status, history, artifacts, metadata, status_message, model, created_at, updated_at
		FROM a2a_tasks %s
		ORDER BY updated_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*A2ATask
	for rows.Next() {
		task, err := s.scanTaskFromRows(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if tasks == nil {
		tasks = []*A2ATask{}
	}
	return tasks, total, nil
}

// Delete removes a task by ID.
func (s *PersistentTaskStore) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM a2a_tasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// RecoverStale marks any submitted/working tasks from a previous crash as failed.
// Returns the number of recovered tasks.
func (s *PersistentTaskStore) RecoverStale() (int, error) {
	statusMsg, _ := json.Marshal(map[string]string{"error": "server restart"})
	res, err := s.db.Exec(`
		UPDATE a2a_tasks
		SET status = 'failed', status_message = $1, updated_at = NOW()
		WHERE status IN ('submitted', 'working')`, statusMsg)
	if err != nil {
		return 0, fmt.Errorf("recover stale tasks: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// scanTask scans a task from a single-row query.
func (s *PersistentTaskStore) scanTask(row *sql.Row) (*A2ATask, error) {
	var task A2ATask
	var status, historyB, artifactsB, metadataB sql.NullString
	var statusMsgB, model sql.NullString

	err := row.Scan(
		&task.ID, &task.ContextID, &status,
		&historyB, &artifactsB, &metadataB,
		&statusMsgB, &model,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	task.Status = TaskState(status.String)
	task.Model = model.String
	if err := json.Unmarshal([]byte(historyB.String), &task.History); err != nil {
		return nil, fmt.Errorf("unmarshal history: %w", err)
	}
	if err := json.Unmarshal([]byte(artifactsB.String), &task.Artifacts); err != nil {
		return nil, fmt.Errorf("unmarshal artifacts: %w", err)
	}
	if metadataB.Valid && metadataB.String != "" {
		if err := json.Unmarshal([]byte(metadataB.String), &task.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}
	if statusMsgB.Valid && statusMsgB.String != "" {
		var msg A2AMessage
		if err := json.Unmarshal([]byte(statusMsgB.String), &msg); err != nil {
			return nil, fmt.Errorf("unmarshal status_message: %w", err)
		}
		task.StatusMessage = &msg
	}
	return &task, nil
}

// scanTaskFromRows scans a task from multi-row query results.
func (s *PersistentTaskStore) scanTaskFromRows(rows *sql.Rows) (*A2ATask, error) {
	var task A2ATask
	var status, historyB, artifactsB, metadataB sql.NullString
	var statusMsgB, model sql.NullString

	err := rows.Scan(
		&task.ID, &task.ContextID, &status,
		&historyB, &artifactsB, &metadataB,
		&statusMsgB, &model,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	task.Status = TaskState(status.String)
	task.Model = model.String
	if err := json.Unmarshal([]byte(historyB.String), &task.History); err != nil {
		return nil, fmt.Errorf("unmarshal history: %w", err)
	}
	if err := json.Unmarshal([]byte(artifactsB.String), &task.Artifacts); err != nil {
		return nil, fmt.Errorf("unmarshal artifacts: %w", err)
	}
	if metadataB.Valid && metadataB.String != "" {
		if err := json.Unmarshal([]byte(metadataB.String), &task.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}
	if statusMsgB.Valid && statusMsgB.String != "" {
		var msg A2AMessage
		if err := json.Unmarshal([]byte(statusMsgB.String), &msg); err != nil {
			return nil, fmt.Errorf("unmarshal status_message: %w", err)
		}
		task.StatusMessage = &msg
	}
	return &task, nil
}

// isDuplicateKeyError checks if a Postgres error is a unique violation (23505).
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// pgx/v5 and lib/pq both include "23505" in duplicate key errors
	return contains23505(s)
}

func contains23505(s string) bool {
	return len(s) > 4 && (s[:5] == "23505" ||
		s[len(s)-5:] == "23505" ||
		containsString(s, "duplicate key") ||
		containsString(s, "23505"))
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Compile-time interface checks.
var (
	_ TaskStore = (*MemoryTaskStore)(nil)
	_ TaskStore = (*PersistentTaskStore)(nil)
)

// Ensure TaskStore types are usable at compile time.
// The MemoryTaskStore's time fields are set by the store.
// For PersistentTaskStore, created_at defaults to NOW() in SQL.
var _ = time.Now // reference time package
