# Architecture Research

**Domain:** Repo-scoped Go CLI agent with tool use, session persistence, and semantic code retrieval
**Researched:** 2026-03-18
**Confidence:** HIGH

## Standard Architecture

### System Overview

```
┌────────────────────────────────────────────────────────────────────────────────────┐
│                                  CLI / Application Layer                          │
├────────────────────────────────────────────────────────────────────────────────────┤
│  cmd/deep-agent                                                                    │
│    ├─ Agent Runtime Loop                                                           │
│    ├─ Prompt/Console I/O                                                           │
│    └─ Provider Adapter (OpenAI/Anthropic)                                          │
├────────────────────────────────────────────────────────────────────────────────────┤
│                                  Domain / Service Layer                           │
├────────────────────────────────────────────────────────────────────────────────────┤
│  Tool Registry     Tool Executor Sandbox     Session Service       Retrieval Svc   │
│  (typed specs)     (policy + limits)         (threads/messages)    (semantic kNN) │
│                                                                                    │
│  Repo Sync Service (Merkle detector)      Indexing Pipeline Orchestrator           │
│  (detect changed files/chunks only)       (chunk -> embed -> upsert)               │
├────────────────────────────────────────────────────────────────────────────────────┤
│                              Infrastructure / Persistence Layer                    │
├────────────────────────────────────────────────────────────────────────────────────┤
│  Postgres + pgvector               Local Filesystem (target repo)                  │
│  - sessions, threads, messages     - file bytes, metadata                           │
│  - repo snapshots + hashes         - .gitignore-aware traversal                     │
│  - chunks + embeddings             - content source of truth                         │
└────────────────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|----------------|------------------------|
| `RuntimeLoop` | Own one turn cycle (user input -> model call -> tool loop -> persistence) | Stateful orchestrator with explicit turn state machine |
| `ProviderAdapter` | Normalize provider-specific APIs into one inference contract | Interface + OpenAI/Anthropic adapters |
| `ToolRegistry` | Provide typed tool schemas and stable tool IDs | Generic registry keyed by tool name + typed arg structs |
| `ToolSandbox` | Enforce execution policy (timeouts, cwd allowlist, command denylist, max output) | Wrapper around tool executors with policy checks |
| `SessionService` | Create/load sessions, append thread messages/events, list history | Service over repository interfaces |
| `RepoSyncService` | Compute Merkle-style hashes, detect file/chunk deltas | Incremental scanner with persisted snapshot table |
| `IndexPipeline` | Chunk changed files, generate embeddings, write vectors | Worker pipeline with retry queue and idempotent upsert |
| `RetrievalService` | Execute semantic query and rank/filter results for prompt context | Query service over pgvector + metadata filters |
| `StorageRepos` | Isolate SQL and transaction boundaries | Repository package (`sessions`, `index`, `sync`, `jobs`) |

## Recommended Project Structure

```
cmd/
└── deep-agent/
    └── main.go                  # CLI bootstrap, wiring, config

internal/
├── app/
│   ├── runtime/                 # Agent loop and turn orchestration
│   ├── session/                 # Session and thread services
│   └── retrieval/               # Semantic retrieval service
├── tools/
│   ├── registry/                # Typed tool definitions + schema export
│   ├── sandbox/                 # Policy enforcement and execution wrapper
│   └── builtin/                 # read_file, list_files, bash, semantic_search
├── index/
│   ├── sync/                    # Merkle detector and changed file planner
│   ├── chunk/                   # Chunking/splitting strategies
│   ├── embed/                   # Embedding provider client + batching
│   └── pipeline/                # End-to-end indexing orchestration
├── provider/
│   ├── model/                   # Common model interface/contracts
│   ├── openai/                  # OpenAI implementation
│   └── anthropic/               # Anthropic implementation
├── store/
│   ├── postgres/                # DB client, migrations, repositories
│   └── migrations/              # SQL schema (sessions, chunks, vectors)
└── config/
    └── config.go                # Env/config loading and validation

pkg/
└── types/                       # Shared DTOs used across internal boundaries
```

### Structure Rationale

- **`cmd/` + `internal/` split:** Replaces current duplicated root-level binaries with one composable runtime and shared modules.
- **`app/` owns orchestration, not SQL/tool details:** Keeps turn logic testable and free of infrastructure coupling.
- **`tools/` separated from runtime:** Enables strict tool typing and policy enforcement without bloating agent loop code.
- **`index/` isolated as its own subsystem:** Indexing can run inline for MVP, then move to worker mode without redesign.
- **`store/postgres` repository boundary:** Prevents SQL spread and allows atomic transaction handling for sessions/index jobs.

## Architectural Patterns

### Pattern 1: Runtime Orchestrator + Ports/Adapters

**What:** Runtime loop depends on interfaces (`Model`, `SessionRepo`, `ToolExecutor`, `Retriever`) rather than SDK clients.
**When to use:** Immediately, because current code duplicates provider/runtime logic across multiple `main` files.
**Trade-offs:** Slight interface overhead; big gain in testability and reduced drift.

**Example:**
```go
type RuntimeDeps struct {
    Model    model.Client
    Tools    tools.Executor
    Sessions session.Service
    Retrieve retrieval.Service
}
```

### Pattern 2: Incremental Indexing via Merkle Snapshot

**What:** Persist hash tree state (`repo`, `path`, `content_hash`, `chunk_hashes`) and only re-index changed units.
**When to use:** Required for repo-scale performance; full re-embed per run does not scale.
**Trade-offs:** More metadata and bookkeeping; far lower embedding cost and latency.

**Example:**
```go
type FileSnapshot struct {
    RepoID      string
    Path        string
    ContentHash string
    ChunkHashes []string
}
```

### Pattern 3: Transactional Outbox for Index Jobs

**What:** On detected changes, write index jobs in DB transaction, then worker claims with `FOR UPDATE SKIP LOCKED`.
**When to use:** As soon as indexing becomes async/background.
**Trade-offs:** Extra table/worker complexity; robust retries and crash recovery.

**Example:**
```go
-- claim next pending jobs safely across workers
SELECT id, payload
FROM index_jobs
WHERE status = 'pending'
ORDER BY created_at
FOR UPDATE SKIP LOCKED
LIMIT 50;
```

## Data Flow

### Request Flow

```
[User Prompt]
    ↓
[RuntimeLoop]
    ↓ append user message
[SessionService] ───────────────→ [Postgres: messages]
    ↓
[ProviderAdapter.runInference]
    ↓
[Tool Calls?] ──no──→ [Assistant Response] → [SessionService persist] → [CLI Output]
    │
   yes
    ↓
[ToolRegistry resolve]
    ↓
[ToolSandbox execute]
    ├─ read/list/bash tools → [Filesystem / Shell]
    └─ semantic_search tool → [RetrievalService] → [pgvector query]
    ↓
[Tool results appended]
    ↓
[ProviderAdapter.runInference (follow-up)]
    ↓
[Final assistant output + persisted turn]
```

### State Management

```
[In-memory Turn State]
    ↓ flush each event
[Session Event Log in Postgres]
    ↓ replay for resume/thread history
[RuntimeLoop restore context window]
```

### Key Data Flows

1. **Session persistence flow:** runtime emits normalized events (`user_message`, `tool_call`, `tool_result`, `assistant_message`) that are committed transactionally per turn.
2. **Repo sync flow:** scanner walks repo -> computes Merkle deltas -> enqueues changed file/chunk jobs -> worker embeds -> upserts chunk vectors.
3. **Semantic retrieval flow:** tool query text -> embedding query vector -> pgvector nearest-neighbor + metadata filter -> top-k snippets -> prompt context injection.
4. **Freshness flow:** sync completion emits event (optional `NOTIFY`) so active sessions can re-query retrieval without restart.

## Suggested Build Order (Roadmap Input)

1. **Unify runtime scaffolding first**
   - Create `cmd/deep-agent` + `internal/app/runtime` + provider adapters.
   - Keep existing tools, but route through one shared loop.
2. **Introduce typed tool registry + sandbox**
   - Replace ad-hoc tool definitions with typed arg/result contracts and central policy enforcement.
3. **Add session/thread persistence**
   - Postgres schema + repositories + session service; persist turn/event log before indexing work.
4. **Implement Merkle sync detector**
   - File snapshot tables + deterministic hashing + changed-file planning.
5. **Add embedding pipeline + vector storage**
   - Chunker, embedder, async index jobs, pgvector tables/indexes.
6. **Expose semantic retrieval API + tool**
   - Retrieval service and `semantic_search` tool integrated into runtime loop.
7. **Optimize and harden**
   - Queue retries, backpressure, observability, warm caches, and selective re-ranking.

**Ordering rationale:** Session correctness and runtime unification are prerequisites; indexing/retrieval can then be layered incrementally without blocking core chat usability.

## Scaling Considerations

| Scale | Architecture Adjustments |
|-------|--------------------------|
| 0-100 repos / single user | Single process runtime; inline sync/indexing acceptable |
| 100-5k repos / team usage | Split indexing into background workers; keep runtime latency isolated |
| 5k+ repos / multi-tenant | Dedicated index workers, partitioned tables by repo/tenant, queue monitoring, stricter quotas |

### Scaling Priorities

1. **First bottleneck:** embedding throughput/cost; fix with delta indexing, batching, and queue-based workers.
2. **Second bottleneck:** retrieval query latency; fix with proper pgvector index strategy (`HNSW`/`IVFFlat` per workload) and metadata prefilters.

## Anti-Patterns

### Anti-Pattern 1: Monolithic God-Agent

**What people do:** Keep runtime, tools, SQL, indexing, and provider SDK calls in one package/file set.
**Why it's wrong:** Change velocity drops and behavior diverges across binaries (already visible in current duplicated entrypoints).
**Do this instead:** Isolate orchestration in `app/` and move infra/tool specifics behind interfaces.

### Anti-Pattern 2: Full Re-index on Every Run

**What people do:** Re-scan and re-embed the entire repo on each session start.
**Why it's wrong:** High latency/cost and poor UX as repos grow.
**Do this instead:** Persist snapshots and only enqueue changed files/chunks via Merkle diff.

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| OpenAI / Anthropic | Provider adapter with shared request/response contract | Keep model-specific fields out of core runtime |
| PostgreSQL | Repository layer + migrations | Store sessions, job queues, snapshots, chunk metadata |
| pgvector extension | SQL migration + vector indexes per distance metric | Choose index type by recall/latency profile |
| Local filesystem | Read-only scan + guarded write tools | Respect ignore rules and workspace root policy |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `runtime` ↔ `tools` | Typed interface calls | Runtime never executes shell/filesystem directly |
| `runtime` ↔ `session` | Service API | Session layer owns persistence schema |
| `sync` ↔ `pipeline` | Planned change-set DTOs | Keep hashing deterministic and side-effect free |
| `pipeline` ↔ `retrieval` | Shared chunk/vector schema | Ensure chunk IDs stable across re-index cycles |
| `provider` ↔ `runtime` | Inference port | Enables model/provider swapping without runtime rewrite |

## Sources

- Existing project architecture notes: `.planning/codebase/ARCHITECTURE.md` (HIGH)
- Current implementation files: `chat.go`, `read.go`, `list_files.go`, `bash_tool.go`, `code_search_tool.go`, `edit_tool.go` (HIGH)
- PostgreSQL `NOTIFY` docs: [postgresql.org/docs/current/sql-notify.html](https://www.postgresql.org/docs/current/sql-notify.html) (HIGH)
- PostgreSQL `FOR UPDATE ... SKIP LOCKED`: [postgresql.org/docs/current/sql-select.html](https://www.postgresql.org/docs/current/sql-select.html) (HIGH)
- pgvector README (indexing/operators): [raw.githubusercontent.com/pgvector/pgvector/master/README.md](https://raw.githubusercontent.com/pgvector/pgvector/master/README.md) (HIGH)
- OpenAI embeddings guide (vector dimensions and retrieval usage): [platform.openai.com/docs/guides/embeddings](https://platform.openai.com/docs/guides/embeddings) (HIGH)

---
*Architecture research for: deep-cli-agent (subsequent milestone integration)*
*Researched: 2026-03-18*
