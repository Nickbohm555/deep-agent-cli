CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE sessions (
    thread_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_root TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE messages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    thread_id UUID NOT NULL REFERENCES sessions(thread_id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_messages_thread_id_id ON messages (thread_id, id);
