-- 001_init.sql: Core governance schema

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    name        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS virtual_keys (
    id                TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key_prefix        TEXT NOT NULL,
    key_hash          TEXT NOT NULL UNIQUE,
    name              TEXT NOT NULL,
    allowed_models    TEXT[] DEFAULT '{}',
    allowed_providers TEXT[] DEFAULT '{}',
    rpm_limit         INT DEFAULT 0,
    tpm_limit         INT DEFAULT 0,
    budget_limit_usd  NUMERIC(12,4) DEFAULT 0,
    budget_period     TEXT DEFAULT 'monthly',
    expires_at        TIMESTAMPTZ,
    revoked_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_key_hash ON virtual_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_project_id ON virtual_keys(project_id);

CREATE TABLE IF NOT EXISTS usage_logs (
    id                BIGSERIAL PRIMARY KEY,
    request_id        TEXT NOT NULL,
    project_id        TEXT,
    virtual_key_id    TEXT,
    model             TEXT NOT NULL,
    provider          TEXT NOT NULL,
    provider_model    TEXT NOT NULL,
    prompt_tokens     INT DEFAULT 0,
    completion_tokens INT DEFAULT 0,
    total_tokens      INT DEFAULT 0,
    cost_usd          NUMERIC(12,6) DEFAULT 0,
    cache_hit         BOOLEAN DEFAULT false,
    stream            BOOLEAN DEFAULT false,
    attempts          INT DEFAULT 1,
    duration_ms       INT DEFAULT 0,
    error             TEXT DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_usage_logs_project_id ON usage_logs(project_id);
CREATE INDEX IF NOT EXISTS idx_usage_logs_virtual_key_id ON usage_logs(virtual_key_id);
CREATE INDEX IF NOT EXISTS idx_usage_logs_created_at ON usage_logs(created_at);
