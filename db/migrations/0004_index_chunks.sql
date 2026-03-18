CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE index_chunks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions(thread_id) ON DELETE CASCADE,
    repo_root TEXT NOT NULL,
    rel_path TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    model TEXT NOT NULL,
    embedding_dims INTEGER NOT NULL CHECK (embedding_dims = 1536),
    embedding VECTOR(1536) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT index_chunks_scope_identity_unique UNIQUE (session_id, repo_root, rel_path, chunk_index, content_hash)
);

CREATE INDEX idx_index_chunks_scope_inspect
    ON index_chunks (session_id, repo_root, rel_path, chunk_index);
