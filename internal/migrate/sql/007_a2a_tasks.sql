-- 007_a2a_tasks.sql: A2A task persistence for async task execution.

CREATE TABLE IF NOT EXISTS a2a_tasks (
    id              TEXT PRIMARY KEY,
    context_id      TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'submitted',
    history         JSONB NOT NULL DEFAULT '[]',
    artifacts       JSONB NOT NULL DEFAULT '[]',
    metadata        JSONB DEFAULT '{}',
    status_message  JSONB,
    model           TEXT,
    push_config     JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_a2a_tasks_status ON a2a_tasks(status);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_context ON a2a_tasks(context_id);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_updated ON a2a_tasks(updated_at);
