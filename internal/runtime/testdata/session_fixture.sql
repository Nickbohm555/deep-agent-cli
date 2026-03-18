INSERT INTO sessions (thread_id, repo_root, created_at)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    __REPO_ROOT__,
    '2026-03-18T12:00:00Z'
);

INSERT INTO messages (id, thread_id, role, content, created_at)
OVERRIDING SYSTEM VALUE
VALUES
    (7, '11111111-1111-1111-1111-111111111111', 'assistant', 'assistant-before-user', '2026-03-18T12:03:00Z'),
    (8, '11111111-1111-1111-1111-111111111111', 'tool', 'tool-between-turns', '2026-03-18T12:02:00Z'),
    (9, '11111111-1111-1111-1111-111111111111', 'user', 'user-after-tool', '2026-03-18T12:01:00Z');
