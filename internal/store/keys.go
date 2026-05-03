package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// VirtualKey represents a gateway API key scoped to a project.
type VirtualKey struct {
	ID               string     `json:"id"`
	ProjectID        string     `json:"project_id"`
	KeyPrefix        string     `json:"key_prefix"`
	KeyHash          string     `json:"-"`
	Name             string     `json:"name"`
	AllowedModels    []string   `json:"allowed_models"`
	AllowedProviders []string   `json:"allowed_providers"`
	AllowedTools     []string   `json:"allowed_tools"`
	RPMLimit         int        `json:"rpm_limit"`
	TPMLimit         int        `json:"tpm_limit"`
	BudgetLimitUSD   float64    `json:"budget_limit_usd"`
	BudgetPeriod     string     `json:"budget_period"`
	ExpiresAt        *time.Time `json:"expires_at"`
	RevokedAt        *time.Time `json:"revoked_at"`
	CreatedAt        time.Time  `json:"created_at"`
	AllowMCPServer   bool       `json:"allow_mcp_server"`
	MCPToolName      string     `json:"mcp_tool_name,omitempty"`
}

// CreateVirtualKey inserts a new virtual key.
func CreateVirtualKey(ctx context.Context, db Queryer, key *VirtualKey) error {
	return db.QueryRowContext(ctx,
		`INSERT INTO virtual_keys
			(project_id, key_prefix, key_hash, name, allowed_models, allowed_providers, allowed_tools,
			 rpm_limit, tpm_limit, budget_limit_usd, budget_period, expires_at,
			 allow_mcp_server, mcp_tool_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING id, created_at`,
		key.ProjectID, key.KeyPrefix, key.KeyHash, key.Name,
		arrayString(key.AllowedModels), arrayString(key.AllowedProviders), arrayString(key.AllowedTools),
		key.RPMLimit, key.TPMLimit, key.BudgetLimitUSD, key.BudgetPeriod, key.ExpiresAt,
		key.AllowMCPServer, key.MCPToolName,
	).Scan(&key.ID, &key.CreatedAt)
}

// ListVirtualKeys returns keys for a project, ordered by creation time.
func ListVirtualKeys(ctx context.Context, db Queryer, projectID string) ([]VirtualKey, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, key_prefix, name, allowed_models, allowed_providers, allowed_tools,
		        rpm_limit, tpm_limit, budget_limit_usd, budget_period,
		        expires_at, revoked_at, created_at, allow_mcp_server, mcp_tool_name
		 FROM virtual_keys
		 WHERE ($1 = '' OR project_id = $1)
		 ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []VirtualKey
	for rows.Next() {
		var k VirtualKey
		var allowedModels, allowedProviders, allowedTools string
		if err := rows.Scan(
			&k.ID, &k.ProjectID, &k.KeyPrefix, &k.Name,
			&allowedModels, &allowedProviders, &allowedTools,
			&k.RPMLimit, &k.TPMLimit, &k.BudgetLimitUSD, &k.BudgetPeriod,
			&k.ExpiresAt, &k.RevokedAt, &k.CreatedAt, &k.AllowMCPServer, &k.MCPToolName,
		); err != nil {
			return nil, err
		}
		k.AllowedModels = parseArrayString(allowedModels)
		k.AllowedProviders = parseArrayString(allowedProviders)
		k.AllowedTools = parseArrayString(allowedTools)
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// RevokeVirtualKey marks a key as revoked. Returns true if a row was updated.
func RevokeVirtualKey(ctx context.Context, db Queryer, id string) (bool, error) {
	res, err := db.ExecContext(ctx,
		"UPDATE virtual_keys SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL",
		id,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// LookupVirtualKeyByHash finds an active (non-revoked, non-expired) key by its bcrypt hash.
func LookupVirtualKeyByHash(ctx context.Context, db Queryer, keyHash string) (*VirtualKey, error) {
	k := &VirtualKey{}
	var allowedModels, allowedProviders, allowedTools string
	err := db.QueryRowContext(ctx,
		`SELECT id, project_id, key_prefix, name, allowed_models, allowed_providers, allowed_tools,
		        rpm_limit, tpm_limit, budget_limit_usd, budget_period,
		        expires_at, revoked_at, created_at, allow_mcp_server, mcp_tool_name
		 FROM virtual_keys
		 WHERE key_hash = $1 AND revoked_at IS NULL
		 LIMIT 1`,
		keyHash,
	).Scan(
		&k.ID, &k.ProjectID, &k.KeyPrefix, &k.Name,
		&allowedModels, &allowedProviders, &allowedTools,
		&k.RPMLimit, &k.TPMLimit, &k.BudgetLimitUSD, &k.BudgetPeriod,
		&k.ExpiresAt, &k.RevokedAt, &k.CreatedAt, &k.AllowMCPServer, &k.MCPToolName,
	)
	if err != nil {
		return nil, err
	}
	k.AllowedModels = parseArrayString(allowedModels)
	k.AllowedProviders = parseArrayString(allowedProviders)
	k.AllowedTools = parseArrayString(allowedTools)
	return k, nil
}

// LookupVirtualKeyByToken finds an active key by comparing the token against stored bcrypt hashes.
// This iterates active keys and uses bcrypt.CompareHashAndPassword.
func LookupVirtualKeyByToken(ctx context.Context, db Queryer, token string) (*VirtualKey, error) {
	// Get all active (non-revoked) key hashes and try bcrypt compare.
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, key_prefix, key_hash, name, allowed_models, allowed_providers, allowed_tools,
		        rpm_limit, tpm_limit, budget_limit_usd, budget_period,
		        expires_at, revoked_at, created_at, allow_mcp_server, mcp_tool_name
		 FROM virtual_keys
		 WHERE revoked_at IS NULL
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var k VirtualKey
		var allowedModels, allowedProviders, allowedTools string
		if err := rows.Scan(
			&k.ID, &k.ProjectID, &k.KeyPrefix, &k.KeyHash, &k.Name,
			&allowedModels, &allowedProviders, &allowedTools,
			&k.RPMLimit, &k.TPMLimit, &k.BudgetLimitUSD, &k.BudgetPeriod,
			&k.ExpiresAt, &k.RevokedAt, &k.CreatedAt, &k.AllowMCPServer, &k.MCPToolName,
		); err != nil {
			return nil, err
		}
		k.AllowedModels = parseArrayString(allowedModels)
		k.AllowedProviders = parseArrayString(allowedProviders)
		k.AllowedTools = parseArrayString(allowedTools)

		if err := bcrypt.CompareHashAndPassword([]byte(k.KeyHash), []byte(token)); err == nil {
			return &k, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no matching virtual key found")
}
// UpdatableVirtualKeyFields lists fields that may be modified via the admin API.
var UpdatableVirtualKeyFields = map[string]bool{
	"name":              true,
	"rpm_limit":         true,
	"tpm_limit":         true,
	"budget_limit_usd":  true,
	"budget_period":     true,
	"allowed_models":    true,
	"allowed_providers": true,
	"expires_at":        true,
}

// UpdateVirtualKey applies a map of field→value updates to the virtual key with the given id.
// Only fields listed in UpdatableVirtualKeyFields are accepted; all others are silently ignored.
// Returns ErrNotFound when no row matches the id.
func UpdateVirtualKey(ctx context.Context, db Queryer, id string, updates map[string]any) error {
	if len(updates) == 0 {
		// Nothing to update — check existence.
		var exists bool
		err := db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM virtual_keys WHERE id = $1)", id,
		).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return nil
	}

	// Build SET clause dynamically from allowed fields.
	setClauses := []string{}
	args := []any{}
	n := 1
	for field, allowed := range UpdatableVirtualKeyFields {
		if !allowed {
			continue
		}
		val, ok := updates[field]
		if !ok {
			continue
		}
		// Convert slices to Postgres array literal
		if field == "allowed_models" || field == "allowed_providers" {
			if slice, ok := val.([]any); ok {
				strs := make([]string, len(slice))
				for i, v := range slice {
					strs[i] = fmt.Sprintf("%v", v)
				}
				val = arrayString(strs)
			} else if slice, ok := val.([]string); ok {
				val = arrayString(slice)
			}
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", field, n))
		args = append(args, val)
		n++
	}

	if len(setClauses) == 0 {
		// No recognized updatable fields — check existence.
		var exists bool
		err := db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM virtual_keys WHERE id = $1)", id,
		).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return nil
	}

	query := fmt.Sprintf(
		"UPDATE virtual_keys SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), n,
	)
	args = append(args, id)

	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func arrayString(items []string) any {
	if len(items) == 0 {
		return "{}"
	}
	return "{" + strings.Join(items, ",") + "}"
}

// parseArrayString parses a Postgres text array literal like {a,b,c} into a Go slice.
func parseArrayString(s string) []string {
	s = strings.Trim(s, "{}")
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
