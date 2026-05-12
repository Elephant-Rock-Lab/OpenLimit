package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"openlimit/internal/store"
	usage "openlimit/internal/usage"
)

func (h *Handler) usage(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "usage:read")
	if actor == nil {
		return
	}

	if h.db == nil {
		writeAdminJSON(w, http.StatusOK, []any{})
		return
	}

	projectID := r.URL.Query().Get("project_id")
	keyID := r.URL.Query().Get("key_id")
	model := r.URL.Query().Get("model")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	limit := 100
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	args := []any{}
	argN := 0
	where := "WHERE 1=1"

	if projectID != "" {
		argN++
		where += " AND project_id = $" + strconv.Itoa(argN)
		args = append(args, projectID)
	}
	if keyID != "" {
		argN++
		where += " AND virtual_key_id = $" + strconv.Itoa(argN)
		args = append(args, keyID)
	}
	if model != "" {
		argN++
		where += " AND model = $" + strconv.Itoa(argN)
		args = append(args, model)
	}
	if from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			argN++
			where += " AND created_at >= $" + strconv.Itoa(argN)
			args = append(args, t)
		}
	}
	if to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			argN++
			where += " AND created_at <= $" + strconv.Itoa(argN)
			args = append(args, t)
		}
	}

	argN++
	query := `SELECT id, request_id, project_id, virtual_key_id, model, provider, provider_model,
	                  prompt_tokens, completion_tokens, total_tokens, cost_usd,
	                  cache_hit, stream, attempts, duration_ms, error, created_at
	           FROM usage_logs ` + where + ` ORDER BY created_at DESC LIMIT $` + strconv.Itoa(argN)
	args = append(args, limit)

	// BATCH-29 TASK-01: offset support
	argN++
	query += ` OFFSET $` + strconv.Itoa(argN)
	args = append(args, offset)

	// Count query for X-Total-Count
	countQuery := `SELECT COUNT(*) FROM usage_logs ` + where
	var totalCount int
	countArgs := args[:len(args)-2] // exclude LIMIT and OFFSET args
	if err := h.db.QueryRowContext(r.Context(), countQuery, countArgs...).Scan(&totalCount); err != nil {
		totalCount = 0
	}

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to query usage")
		return
	}
	defer rows.Close()

	type usageRow struct {
		ID               int64   `json:"id"`
		RequestID        string  `json:"request_id"`
		ProjectID        *string `json:"project_id"`
		VirtualKeyID     *string `json:"virtual_key_id"`
		Model            string  `json:"model"`
		Provider         string  `json:"provider"`
		ProviderModel    string  `json:"provider_model"`
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		CostUSD          float64 `json:"cost_usd"`
		CacheHit         bool    `json:"cache_hit"`
		Stream           bool    `json:"stream"`
		Attempts         int     `json:"attempts"`
		DurationMS       int     `json:"duration_ms"`
		Error            string  `json:"error"`
		CreatedAt        string  `json:"created_at"`
	}

	var result []usageRow
	for rows.Next() {
		var row usageRow
		var createdAt time.Time
		if err := rows.Scan(
			&row.ID, &row.RequestID, &row.ProjectID, &row.VirtualKeyID,
			&row.Model, &row.Provider, &row.ProviderModel,
			&row.PromptTokens, &row.CompletionTokens, &row.TotalTokens, &row.CostUSD,
			&row.CacheHit, &row.Stream, &row.Attempts, &row.DurationMS, &row.Error, &createdAt,
		); err != nil {
			writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to scan usage")
			return
		}
		row.CreatedAt = createdAt.Format(time.RFC3339)
		result = append(result, row)
	}

	if result == nil {
		result = []usageRow{}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(totalCount))
	writeAdminJSON(w, http.StatusOK, result)
}

func (h *Handler) usageSummary(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "usage:read")
	if actor == nil {
		return
	}

	if h.db == nil {
		writeAdminJSON(w, http.StatusOK, map[string]any{})
		return
	}

	projectID := r.URL.Query().Get("project_id")
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "daily"
	}

	var trunc string
	switch period {
	case "monthly":
		trunc = "month"
	default:
		trunc = "day"
	}

	args := []any{}
	argN := 0
	where := "WHERE 1=1"

	if projectID != "" {
		argN++
		where += " AND project_id = $" + strconv.Itoa(argN)
		args = append(args, projectID)
	}

	argN++
	query := `SELECT date_trunc('` + trunc + `', created_at) as period,
	                 model, provider,
	                 COUNT(*) as request_count,
	                 SUM(prompt_tokens) as prompt_tokens,
	                 SUM(completion_tokens) as completion_tokens,
	                 SUM(total_tokens) as total_tokens,
	                 SUM(cost_usd) as cost_usd
	          FROM usage_logs ` + where + `
	          GROUP BY 1, 2, 3
	          ORDER BY 1 DESC, 2
	          LIMIT $` + strconv.Itoa(argN)
	args = append(args, 1000)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to query usage summary")
		return
	}
	defer rows.Close()

	type summaryRow struct {
		Period           string  `json:"period"`
		Model            string  `json:"model"`
		Provider         string  `json:"provider"`
		RequestCount     int     `json:"request_count"`
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		CostUSD          float64 `json:"cost_usd"`
	}

	var result []summaryRow
	for rows.Next() {
		var row summaryRow
		var p time.Time
		if err := rows.Scan(&p, &row.Model, &row.Provider,
			&row.RequestCount, &row.PromptTokens, &row.CompletionTokens,
			&row.TotalTokens, &row.CostUSD,
		); err != nil {
			writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to scan summary")
			return
		}
		row.Period = p.Format(time.RFC3339)
		result = append(result, row)
	}

	if result == nil {
		result = []summaryRow{}
	}
	writeAdminJSON(w, http.StatusOK, result)
}

// computeKeyStatus returns a budget utilization status string based on spend vs budget limit.
// Returns "unlimited" when budgetLimit is 0, "healthy" when utilization < 75%,
// "warning" when 75% <= utilization <= 95%, and "critical" when utilization > 95%.
func computeKeyStatus(spend, budgetLimit float64) string {
	if budgetLimit <= 0 {
		return "unlimited"
	}
	utilizationPct := (spend / budgetLimit) * 100
	if utilizationPct > 95 {
		return "critical"
	}
	if utilizationPct >= 75 {
		return "warning"
	}
	return "healthy"
}

// spend handles GET /admin/usage/spend.
// It returns per-key budget utilization data.
// spend_usd is computed as SUM(cost_usd) from usage_logs for the current period.
func (h *Handler) spend(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "usage:read")
	if actor == nil {
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "monthly"
	}

	if h.db == nil {
		writeAdminJSON(w, http.StatusOK, map[string]any{
			"period":           period,
			"keys":             []any{},
			"total_spend_usd":  0,
			"total_budget_usd": 0,
		})
		return
	}

	// List all virtual keys (empty projectID = all projects)
	keys, err := store.ListVirtualKeys(r.Context(), h.db, "", 0, 0)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to list virtual keys")
		return
	}

	type keySpend struct {
		KeyID          string  `json:"key_id"`
		KeyName        string  `json:"key_name"`
		KeyPrefix      string  `json:"key_prefix"`
		SpendUSD       float64 `json:"spend_usd"`
		BudgetLimitUSD float64 `json:"budget_limit_usd"`
		BudgetPeriod   string  `json:"budget_period"`
		UtilizationPct float64 `json:"utilization_pct"`
		Status         string  `json:"status"`
	}

	var result []keySpend
	var totalSpend, totalBudget float64

	for _, k := range keys {
		spend, err := usage.GetSpendForCurrentPeriod(r.Context(), h.db, k.ID, period)
		if err != nil {
			// Log but continue — don't fail the whole request for one key
			spend = 0
		}

		budgetLimit := k.BudgetLimitUSD
		var utilizationPct float64
		if budgetLimit > 0 {
			utilizationPct = (spend / budgetLimit) * 100
		}

		status := computeKeyStatus(spend, budgetLimit)

		totalSpend += spend
		totalBudget += budgetLimit

		result = append(result, keySpend{
			KeyID:          k.ID,
			KeyName:        k.Name,
			KeyPrefix:      k.KeyPrefix,
			SpendUSD:       spend,
			BudgetLimitUSD: budgetLimit,
			BudgetPeriod:   k.BudgetPeriod,
			UtilizationPct: utilizationPct,
			Status:         status,
		})
	}

	if result == nil {
		result = []keySpend{}
	}

	writeAdminJSON(w, http.StatusOK, map[string]any{
		"period":           period,
		"keys":             result,
		"total_spend_usd":  totalSpend,
		"total_budget_usd": totalBudget,
	})
}

// Helper to suppress unused import warning
var _ = json.Marshal
