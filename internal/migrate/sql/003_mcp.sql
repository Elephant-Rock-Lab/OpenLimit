-- 003_mcp.sql: MCP tool governance tables
-- Adds allowed_tools to virtual keys and creates tool_logs for auditing.

-- Allow virtual keys to scope which MCP tools they can access.
-- Empty array means all tools are permitted (no restriction).
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS allowed_tools TEXT[] DEFAULT '{}';

-- Audit log for all MCP tool executions.
CREATE TABLE IF NOT EXISTS tool_logs (
    id             BIGSERIAL PRIMARY KEY,
    request_id     TEXT NOT NULL,
    project_id     TEXT,
    virtual_key_id TEXT,
    server_name    TEXT NOT NULL,
    tool_name      TEXT NOT NULL,
    arguments      JSONB DEFAULT '{}',
    result         JSONB DEFAULT '{}',
    is_error       BOOLEAN DEFAULT false,
    duration_ms    INT DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tool_logs_virtual_key_id ON tool_logs(virtual_key_id);
CREATE INDEX IF NOT EXISTS idx_tool_logs_created_at ON tool_logs(created_at);
