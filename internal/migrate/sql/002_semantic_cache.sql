-- 002_semantic_cache.sql: Semantic cache tables using pgvector
-- Requires pgvector extension

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS semantic_cache (
    id          BIGSERIAL PRIMARY KEY,
    model       TEXT NOT NULL,
    embedding   vector(768) NOT NULL,
    request_hash TEXT NOT NULL,
    response    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_semantic_cache_model_embedding ON semantic_cache
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

CREATE INDEX IF NOT EXISTS idx_semantic_cache_expires ON semantic_cache (expires_at);
