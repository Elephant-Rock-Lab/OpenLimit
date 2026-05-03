CREATE TABLE IF NOT EXISTS audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT now(),
    event_type  TEXT NOT NULL,
    actor       TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL DEFAULT '',
    resource    TEXT NOT NULL DEFAULT '',
    outcome     TEXT NOT NULL DEFAULT '',
    request_id  TEXT NOT NULL DEFAULT '',
    metadata    JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX idx_audit_logs_event_type ON audit_logs(event_type);
CREATE INDEX idx_audit_logs_timestamp ON audit_logs(timestamp DESC);
