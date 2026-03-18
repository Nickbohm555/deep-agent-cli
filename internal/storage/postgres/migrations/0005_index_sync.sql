CREATE TABLE index_snapshots (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions(thread_id) ON DELETE CASCADE,
    repo_root TEXT NOT NULL,
    root_hash TEXT NOT NULL,
    parent_snapshot_id BIGINT REFERENCES index_snapshots(id) ON DELETE SET NULL,
    status TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT index_snapshots_status_check CHECK (status IN ('pending', 'active', 'superseded', 'failed')),
    CONSTRAINT index_snapshots_scope_root_unique UNIQUE (session_id, repo_root, root_hash)
);

CREATE UNIQUE INDEX idx_index_snapshots_active_scope
    ON index_snapshots (session_id, repo_root)
    WHERE is_active;

CREATE INDEX idx_index_snapshots_latest_lookup
    ON index_snapshots (session_id, repo_root, created_at DESC, id DESC);

CREATE TABLE index_nodes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    snapshot_id BIGINT NOT NULL REFERENCES index_snapshots(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    parent_path TEXT,
    node_type TEXT NOT NULL,
    node_hash TEXT NOT NULL,
    parent_hash TEXT,
    content_hash TEXT,
    size_bytes BIGINT,
    mtime_ns BIGINT,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT index_nodes_type_check CHECK (node_type IN ('file', 'dir')),
    CONSTRAINT index_nodes_status_check CHECK (status IN ('active', 'deleted')),
    CONSTRAINT index_nodes_size_bytes_check CHECK (size_bytes IS NULL OR size_bytes >= 0),
    CONSTRAINT index_nodes_mtime_ns_check CHECK (mtime_ns IS NULL OR mtime_ns >= 0),
    CONSTRAINT index_nodes_snapshot_path_unique UNIQUE (snapshot_id, path)
);

CREATE INDEX idx_index_nodes_snapshot_parent_path
    ON index_nodes (snapshot_id, parent_path, path);

CREATE INDEX idx_index_nodes_snapshot_node_hash
    ON index_nodes (snapshot_id, node_hash);

CREATE TABLE index_file_state (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions(thread_id) ON DELETE CASCADE,
    repo_root TEXT NOT NULL,
    rel_path TEXT NOT NULL,
    last_snapshot_id BIGINT REFERENCES index_snapshots(id) ON DELETE SET NULL,
    content_hash TEXT,
    node_hash TEXT,
    parent_hash TEXT,
    chunk_set_hash TEXT,
    size_bytes BIGINT,
    mtime_ns BIGINT,
    status TEXT NOT NULL,
    deleted_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT index_file_state_status_check CHECK (status IN ('active', 'deleted')),
    CONSTRAINT index_file_state_size_bytes_check CHECK (size_bytes IS NULL OR size_bytes >= 0),
    CONSTRAINT index_file_state_mtime_ns_check CHECK (mtime_ns IS NULL OR mtime_ns >= 0),
    CONSTRAINT index_file_state_scope_path_unique UNIQUE (session_id, repo_root, rel_path)
);

CREATE INDEX idx_index_file_state_snapshot_lookup
    ON index_file_state (last_snapshot_id, rel_path);

CREATE INDEX idx_index_file_state_scope_status
    ON index_file_state (session_id, repo_root, status, rel_path);
