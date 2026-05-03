CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS admin_users (
    id          TEXT PRIMARY KEY DEFAULT ('u_' || gen_random_uuid()::text),
    subject     TEXT UNIQUE,                        -- OIDC sub claim (nullable for token-authed users)
    email       TEXT,                                -- email claim (nullable)
    role        TEXT NOT NULL DEFAULT 'viewer',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ                          -- soft deletion for audit trail
);

CREATE INDEX idx_admin_users_subject ON admin_users(subject) WHERE deleted_at IS NULL;
CREATE INDEX idx_admin_users_email ON admin_users(email) WHERE deleted_at IS NULL;
