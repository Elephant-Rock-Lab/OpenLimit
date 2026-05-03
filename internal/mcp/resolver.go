package mcp

import (
	"context"
	"database/sql"
	"fmt"

	"openlimit/internal/store"
)

// DBKeyResolver resolves MCP tool names to virtual keys by querying the database.
type DBKeyResolver struct {
	db *sql.DB
}

// NewDBKeyResolver creates a new resolver backed by the database.
func NewDBKeyResolver(db *sql.DB) *DBKeyResolver {
	return &DBKeyResolver{db: db}
}

// ResolveToolName finds the virtual key associated with the given MCP tool name.
// It tries mcp_tool_name first (exact match), then falls back to sanitized key name.
func (r *DBKeyResolver) ResolveToolName(ctx context.Context, toolName string) (*ResolvedKey, error) {
	keys, err := store.ListVirtualKeys(ctx, r.db, "")
	if err != nil {
		return nil, fmt.Errorf("query keys: %w", err)
	}

	for i := range keys {
		k := &keys[i]
		if !k.AllowMCPServer || k.RevokedAt != nil {
			continue
		}

		// Exact match on mcp_tool_name (if set)
		if k.MCPToolName != "" && sanitizeToolName(k.MCPToolName) == toolName {
			return toResolvedKey(k), nil
		}

		// Fall back to sanitized key name
		if sanitizeToolName(k.Name) == toolName {
			return toResolvedKey(k), nil
		}

		// Fall back to vk_<id[:8]>
		shortID := k.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		if toolName == "vk_"+shortID {
			return toResolvedKey(k), nil
		}
	}

	return nil, fmt.Errorf("no virtual key found for tool %q", toolName)
}

func toResolvedKey(k *store.VirtualKey) *ResolvedKey {
	return &ResolvedKey{
		KeyID:            k.ID,
		KeyPrefix:        k.KeyPrefix,
		KeyHash:          k.KeyHash,
		ProjectID:        k.ProjectID,
		KeyName:          k.Name,
		AllowedModels:    k.AllowedModels,
		AllowedProviders: k.AllowedProviders,
		RPMLimit:         k.RPMLimit,
		TPMLimit:         k.TPMLimit,
		BudgetLimitUSD:   k.BudgetLimitUSD,
		BudgetPeriod:     k.BudgetPeriod,
	}
}
