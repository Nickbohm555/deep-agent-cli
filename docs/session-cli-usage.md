## Session CLI Usage

This runbook covers the Phase 02 session flow in `cmd/deep-agent-cli`.

### Prerequisites

- Export `DATABASE_URL` to a Postgres database that the CLI can read and write.
- Apply the session schema before the first run:

```bash
psql "$DATABASE_URL" -f db/migrations/0001_sessions_messages.sql
```

- Export `OPENAI_API_KEY` if you want model-backed responses. Without it, the CLI still runs in local fallback mode and replies with `Echo: ...`.

### Create a New Session

Start interactive mode from the repository you want to bind:

```bash
go run ./cmd/deep-agent-cli -mode interactive -repo-root "$(pwd)"
```

Expected startup behavior:

- The CLI prints `Session: <thread-id>`.
- The CLI prints `Chat with deep-agent-cli`.
- The CLI shows the `You:` prompt and accepts terminal input.

Enter a message, then exit with `Ctrl-D` or `Ctrl-C`. Save the printed session ID for resume.

### Resume an Existing Session

Resume the same thread by ID:

```bash
go run ./cmd/deep-agent-cli -mode interactive -session "<thread-id>"
```

For a single prompt against an existing thread:

```bash
go run ./cmd/deep-agent-cli -mode oneshot -session "<thread-id>" -prompt "summarize our last turn"
```

The runtime reloads persisted messages in message ID order before appending the next turn.

### Inspect Persisted History

Inspect a thread directly in Postgres:

```sql
SELECT id, role, content, created_at
FROM messages
WHERE thread_id = '<thread-id>'
ORDER BY id ASC;
```

The `id` column is the replay order. Use it to verify resume order, not `created_at` alone.

### Repo Scope Expectations

Each session is bound to exactly one canonical repository root at creation time. Resume keeps that original root.

Expected scope-denied error text for file or shell operations is:

```text
... denied: path escapes repository scope
```

Current implementation note: `cmd/deep-agent-cli` persists sessions and resumes history, but it does not yet wire provider-driven tool dispatch. Scope-denial behavior is currently exercised by the runtime integration tests in `internal/runtime/e2e_session_scope_test.go`, not by the interactive CLI path.
