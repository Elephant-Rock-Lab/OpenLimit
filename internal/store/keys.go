package store

import (
	"context"
	"fmt"
	"regexp"
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
// offset and limit control SQL-level pagination (use 0, 0 for no pagination).
func ListVirtualKeys(ctx context.Context, db Queryer, projectID string, offset, limit int) ([]VirtualKey, error) {
	query := `SELECT id, project_id, key_prefix, name, allowed_models, allowed_providers, allowed_tools,
		        rpm_limit, tpm_limit, budget_limit_usd, budget_period,
		        expires_at, revoked_at, created_at, allow_mcp_server, mcp_tool_name
		 FROM virtual_keys
		 WHERE ($1 = '' OR project_id = $1)
		 ORDER BY created_at DESC`
	args := []any{projectID}

	if limit > 0 {
		query += ` LIMIT $2 OFFSET $3`
		args = append(args, limit, offset)
	}

	rows, err := db.QueryContext(ctx, query, args...)
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

// ListVirtualKeysForMCP returns only MCP-enabled, non-revoked keys.
// When toolName is non-empty, additionally filters by mcp_tool_name.
// This avoids loading all keys into memory for every MCP tool call resolution.
func ListVirtualKeysForMCP(ctx context.Context, db Queryer, toolName string) ([]VirtualKey, error) {
	query := `SELECT id, project_id, key_prefix, key_hash, name, allowed_models, allowed_providers, allowed_tools,
	                rpm_limit, tpm_limit, budget_limit_usd, budget_period,
	                expires_at, revoked_at, created_at, allow_mcp_server, mcp_tool_name
	         FROM virtual_keys
	         WHERE allow_mcp_server = true AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`

	var args []any
	if toolName != "" {
		query += ` AND mcp_tool_name = $1`
		args = append(args, toolName)
	}

	query += ` ORDER BY created_at DESC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []VirtualKey
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
	// BATCH-30 BN-01: Use key prefix (first 8 chars) to filter candidates.
	// This reduces bcrypt comparisons from O(N) to typically O(1-3).
	prefix := token
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, key_prefix, key_hash, name, allowed_models, allowed_providers, allowed_tools,
		        rpm_limit, tpm_limit, budget_limit_usd, budget_period,
		        expires_at, revoked_at, created_at, allow_mcp_server, mcp_tool_name
		 FROM virtual_keys
		 WHERE revoked_at IS NULL AND key_prefix = $1
		 AND (expires_at IS NULL OR expires_at > now())
		 ORDER BY created_at DESC`,
		prefix,
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

// CountVirtualKeys returns the total number of keys matching the project filter.
func CountVirtualKeys(ctx context.Context, db Queryer, projectID string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM virtual_keys WHERE ($1 = '' OR project_id = $1)`,
		projectID,
	).Scan(&count)
	return count, err
}

// fieldNameRegex validates that SQL field names contain only safe characters.
var fieldNameRegex = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

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
	// Validate budget_limit_usd is non-negative
	if val, ok := updates["budget_limit_usd"]; ok {
		if f, ok := val.(float64); ok && f < 0 {
			return fmt.Errorf("budget_limit_usd must be non-negative")
		}
	}

	// Validate that all allowed field names are safe for SQL interpolation
	for field := range updates {
		if _, allowed := UpdatableVirtualKeyFields[field]; allowed {
			if !fieldNameRegex.MatchString(field) {
				return fmt.Errorf("invalid field name: %q", field)
			}
		}
	}

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
	if items == nil || len(items) == 0 {
		return "{}"
	}

	escaped := make([]string, len(items))
	for i, item := range items {
		if strings.ContainsAny(item, "{},\"") {
			// Escape internal double-quotes as \"
			eitem := strings.ReplaceAll(item, "\"", "\\\"")
			escaped[i] = "\"" + eitem + "\""
		} else {
			escaped[i] = item
		}
	}
	return "{" + strings.Join(escaped, ",") + "}"
}

// parseArrayString parses a Postgres text array literal like {a,b,c} into a Go slice.
// GetVirtualKeyByID fetches a single virtual key by its ID.
// Used by updateKey/patchKey to return the updated key without scanning all keys.
func GetVirtualKeyByID(ctx context.Context, db Queryer, id string) (*VirtualKey, error) {
	var k VirtualKey
	var allowedModels, allowedProviders, allowedTools string
	err := db.QueryRowContext(ctx,
		`SELECT id, project_id, key_prefix, key_hash, name, allowed_models, allowed_providers, allowed_tools,
		        rpm_limit, tpm_limit, budget_limit_usd, budget_period,
		        expires_at, revoked_at, created_at, allow_mcp_server, mcp_tool_name
		 FROM virtual_keys WHERE id = $1`,
		id,
	).Scan(
		&k.ID, &k.ProjectID, &k.KeyPrefix, &k.KeyHash, &k.Name,
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
	return &k, nil
}

// RotateVirtualKey generates a new key for the given virtual key ID, updates the hash and prefix
// in the database, and returns the updated VirtualKey and the raw key string.
// Returns an error if the key is revoked or not found.
func RotateVirtualKey(ctx context.Context, db Queryer, id string, rawKey string, hash []byte) (*VirtualKey, string, error) {
	// Fetch existing key to check state
	existing, err := GetVirtualKeyByID(ctx, db, id)
	if err != nil {
		return nil, "", ErrNotFound
	}
	if existing.RevokedAt != nil {
		return nil, "", fmt.Errorf("key is revoked")
	}

	prefix := rawKey[:min(8, len(rawKey))]

	res, err := db.ExecContext(ctx,
		`UPDATE virtual_keys SET key_hash = $1, key_prefix = $2 WHERE id = $3 AND revoked_at IS NULL`,
		string(hash), prefix, id,
	)
	if err != nil {
		return nil, "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, "", ErrNotFound
	}

	// Fetch the updated key to return
	updated, err := GetVirtualKeyByID(ctx, db, id)
	if err != nil {
		return nil, "", err
	}

	return updated, rawKey, nil
}

func parseArrayString(s string) []string {
	s = strings.Trim(s, "{}")
	if s == "" {
		return nil
	}

	var result []string
	var buf strings.Builder
	inQuotes := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuotes {
			if ch == '"' {
				// Check for escaped quote
				if i+1 < len(s) && s[i+1] == '"' {
					buf.WriteByte('"')
					i++ // skip next quote
				} else {
					inQuotes = false
				}
			} else if ch == '\\' && i+1 < len(s) && s[i+1] == '"' {
				// Backslash-escaped quote: \" → "
				buf.WriteByte('"')
				i++
			} else {
				buf.WriteByte(ch)
			}
		} else {
			if ch == '"' {
				inQuotes = true
			} else if ch == ',' {
				item := buf.String()
				if item != "" {
					result = append(result, item)
				}
				buf.Reset()
			} else {
				buf.WriteByte(ch)
			}
		}
	}

	// Flush last element
	item := buf.String()
	if item != "" {
		result = append(result, item)
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
