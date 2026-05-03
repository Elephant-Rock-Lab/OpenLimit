-- 004_mcp_server.sql: MCP server mode support
-- Adds columns for exposing virtual keys as MCP tools.

ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS allow_mcp_server BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS mcp_tool_name TEXT NOT NULL DEFAULT '';

-- Ensure custom tool names are unique when set
CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_keys_mcp_tool_name
    ON virtual_keys(mcp_tool_name) WHERE mcp_tool_name != '';
