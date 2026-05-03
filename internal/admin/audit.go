package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// AuditRow represents a row from the audit_logs table.
type AuditRow struct {
	ID        int64          `json:"id"`
	Timestamp string         `json:"timestamp"`
	EventType string         `json:"event_type"`
	Actor     string         `json:"actor"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Outcome   string         `json:"outcome"`
	RequestID string         `json:"request_id"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) queryAuditLogs(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "audit:read")
	if actor == nil {
		return
	}
	_ = actor // used for audit trail

	eventType := r.URL.Query().Get("event_type")
	actorFilter := r.URL.Query().Get("actor")
	resource := r.URL.Query().Get("resource")
	since := r.URL.Query().Get("since")
	limitStr := r.URL.Query().Get("limit")

	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if limit > 1000 {
		limit = 1000
	}

	query := `SELECT id, timestamp, event_type, actor, action, resource, outcome, request_id, metadata
			  FROM audit_logs WHERE 1=1`
	args := []any{}
	argN := 1

	if eventType != "" {
		query += ` AND event_type = $` + strconv.Itoa(argN)
		args = append(args, eventType)
		argN++
	}
	if actorFilter != "" {
		query += ` AND actor = $` + strconv.Itoa(argN)
		args = append(args, actorFilter)
		argN++
	}
	if resource != "" {
		query += ` AND resource = $` + strconv.Itoa(argN)
		args = append(args, resource)
		argN++
	}
	if since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			query += ` AND timestamp >= $` + strconv.Itoa(argN)
			args = append(args, t)
			argN++
		}
	}

	query += ` ORDER BY timestamp DESC LIMIT $` + strconv.Itoa(argN)
	args = append(args, limit)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to query audit logs")
		return
	}
	defer rows.Close()

	results := []AuditRow{}
	for rows.Next() {
		var row AuditRow
		var ts time.Time
		var metaJSON string
		if err := rows.Scan(&row.ID, &ts, &row.EventType, &row.Actor, &row.Action, &row.Resource, &row.Outcome, &row.RequestID, &metaJSON); err != nil {
			writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to scan audit row")
			return
		}
		row.Timestamp = ts.UTC().Format(time.RFC3339)
		if metaJSON != "" && metaJSON != "{}" {
			_ = json.Unmarshal([]byte(metaJSON), &row.Metadata)
		}
		results = append(results, row)
	}

	if results == nil {
		results = []AuditRow{}
	}
	writeAdminJSON(w, http.StatusOK, results)
}
