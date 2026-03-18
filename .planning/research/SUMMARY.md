# Project Research Summary

**Project:** deep-agent-cli
**Domain:** Repo-scoped Go CLI coding agent with persistent sessions and semantic retrieval
**Researched:** 2026-03-18
**Confidence:** HIGH

## Executive Summary

This project is a local-first, repo-scoped coding agent where correctness and trust boundaries matter as much as model quality. Strong implementations in this space start with a deterministic runtime loop, explicit tool contracts, strict repo scope enforcement, and auditable persistence. The recommended baseline is a Go service architecture with Postgres plus pgvector, incremental Merkle-based indexing, and evidence-forward retrieval responses (chunk + path + score) instead of opaque context injection.

The best delivery strategy is to build in dependency order: unify runtime and policy boundaries first, then persist session state, then add incremental indexing and embeddings, then expose semantic retrieval. This sequence matches both feature dependencies and architecture guidance: session and scope invariants are prerequisites, while indexing/retrieval can be layered in without destabilizing core chat behavior.

Primary risks are scope leakage, stale index state (especially delete/rename handling), mixed-epoch reads during background sync, and drift between embedding model/dimensions and stored vectors. Mitigation requires fail-closed scope proofs, deterministic chunk identity, epoch-based index promotion, and explicit embedding version contracts. If these controls are implemented early, the roadmap can move quickly without accruing high-risk retrieval debt.

## Key Findings

### Recommended Stack

Research strongly supports a single-system data plane for v1: Go + Postgres + pgvector + OpenAI embeddings. This keeps operations simple while still enabling high-quality semantic retrieval and durable session memory. Supporting libraries (`pgx`, `pgvector-go`, `openai-go`) are mature and align with the required architecture.

**Core technologies:**
- `Go 1.26.x`: primary runtime for CLI orchestration and service boundaries — aligns with current codebase and dependency trajectory.
- `PostgreSQL 17.9 or 18.3`: durable storage for sessions, index metadata, and jobs — enforces repo/session scope via schema and SQL constraints.
- `pgvector 0.8.x`: vector search in the same transactional store — avoids dual-write complexity in v1.
- `OpenAI text-embedding-3-small` (default) / `text-embedding-3-large` (quality mode): embedding generation with dimension control — enables explicit quality/cost tuning.

### Expected Features

The launch feature set should prioritize trust, continuity, and retrieval utility over autonomy breadth. Table-stakes capabilities are dual-mode execution, safe git-aware editing, repo-scoped resume, permissioned tools, and robust repo context retrieval. Differentiators are durable Postgres thread history, Merkle incremental indexing, transparent semantic search evidence, and clean embedding/tool boundaries for future swaps.

**Must have (table stakes):**
- Interactive + headless execution modes — required for both manual and scripted workflows.
- Repo-scoped session persistence/resume — expected continuity behavior for coding agents.
- Permissioned tool execution and safety controls — baseline trust requirement.
- Repo context retrieval with deterministic tooling and search — baseline usefulness requirement.

**Should have (competitive):**
- Postgres-backed thread/message history with strict scoping — durable, queryable continuity.
- Merkle-based incremental indexing — practical latency/cost control at repo scale.
- Semantic search returning `chunk + path + score` — transparent grounding and debuggability.
- Embedding provider adapter boundary — future-proofing without pipeline rewrite.

**Defer (v2+):**
- Cross-repo shared memory — too risky for leakage/correctness in early versions.
- Multi-agent planner/executor decomposition — defer until single-agent reliability is solid.
- Real-time per-keystroke indexing — poor local ergonomics versus debounced incremental sync.

### Architecture Approach

A layered architecture is recommended: CLI/runtime orchestration, domain services (sessions/tools/retrieval/index sync), and infrastructure repos over Postgres/filesystem. Runtime should depend on ports/adapters instead of provider SDKs and direct SQL. Indexing should run as an incremental pipeline with deterministic chunking and optional async jobs; retrieval should query a scoped, version-consistent vector space and return provenance with each result.

**Major components:**
1. `RuntimeLoop + ProviderAdapter` — manages turn lifecycle and model abstraction.
2. `ToolRegistry + ToolSandbox` — typed tool contracts and policy-enforced execution.
3. `SessionService + StorageRepos` — durable thread/event persistence and resume semantics.
4. `RepoSyncService + IndexPipeline` — Merkle deltas, chunking, embedding, and upsert workflow.
5. `RetrievalService` — scoped semantic query, ranking/filtering, and evidence output.

### Critical Pitfalls

1. **Weak scope enforcement** — treat scope as a hard invariant with proof fields, canonical path checks, and fail-closed behavior.
2. **Incremental sync without delete/rename lifecycle** — track add/modify/delete explicitly and reconcile tombstones before serving.
3. **Unstable chunk IDs** — use content-addressed IDs with chunker versioning to prevent cache thrash.
4. **Mixed index states during background sync** — use epoch-based writes and atomic promotion for read consistency.
5. **Embedding version/dimension drift** — persist embedding version metadata and enforce query-time compatibility.

## Implications for Roadmap

Based on research, suggested phase structure:

### Phase 1: Runtime, Scope, and Persistence Foundations
**Rationale:** Runtime unification and scope contracts are prerequisites for safe feature growth and all downstream persistence/indexing work.  
**Delivers:** Unified `cmd` entrypoint/runtime, provider adapters, typed tool contracts, repo identity model, session/thread schema, resumable event/state persistence.  
**Addresses:** Table-stakes execution modes, repo-scoped continuity, permission safety.  
**Avoids:** Scope leakage and message-only persistence pitfalls.

### Phase 2: Incremental Indexing and Embedding Contracts
**Rationale:** Retrieval quality and cost depend on deterministic deltas and stable embedding/chunk identity.  
**Delivers:** Merkle delta detector, add/modify/delete lifecycle, deterministic chunking IDs, embedding adapter, vector schema with version metadata and migration guards.  
**Uses:** Postgres + pgvector, OpenAI embedding models, hashing/chunking libraries.  
**Implements:** `RepoSyncService`, `IndexPipeline`, and storage repositories for snapshots/jobs/chunks.

### Phase 3: Semantic Retrieval and Runtime Integration
**Rationale:** Retrieval should launch only after index correctness controls exist, otherwise users lose trust quickly.  
**Delivers:** Retrieval service, `semantic_search` tool, chunk/path/score/provenance output, prompt injection policy, freshness signaling.  
**Addresses:** Core repo context and key differentiator features.  
**Avoids:** Hidden retrieval and mixed-state read pitfalls.

### Phase 4: Reliability, Observability, and Tuning
**Rationale:** Once core loop works, stabilize through traces, regression harnesses, and performance tuning.  
**Delivers:** Retrieval trace diagnostics, golden query tests, background worker hardening, index epoch observability, hybrid retrieval/rerank (optional v1.x).  
**Addresses:** Ongoing quality, debuggability, and scale readiness.  
**Avoids:** "Flaky" relevance regressions and unfixable production search issues.

### Phase Ordering Rationale

- Phase order follows hard dependencies: scope + persistence -> index lifecycle -> retrieval integration -> tuning/hardening.
- Grouping mirrors architectural boundaries to minimize cross-cutting churn (`runtime`, then `index`, then `retrieval`).
- Early explicit guardrails reduce the highest-impact pitfalls before introducing async complexity.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 2:** Embedding migration strategy and chunking versioning need concrete cutover mechanics and compatibility tests.
- **Phase 3:** Retrieval ranking blend (semantic vs lexical vs metadata) likely needs corpus-specific validation.
- **Phase 4:** Background worker throughput/index type tuning should be benchmark-driven for target repo sizes.

Phases with standard patterns (skip research-phase):
- **Phase 1:** Runtime ports/adapters, typed tool sandboxing, and Postgres session persistence are well-established patterns.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Strong alignment with official docs/version checks and coherent v1 operational trade-offs. |
| Features | MEDIUM-HIGH | Competitor patterns are clear; exact differentiator ROI still depends on user workflow validation. |
| Architecture | HIGH | Recommendations map directly to current codebase constraints and proven service boundaries. |
| Pitfalls | MEDIUM-HIGH | Risks are well characterized; some prevention details (especially ANN filtering behavior) still need local benchmarks. |

**Overall confidence:** HIGH

### Gaps to Address

- **Retrieval quality thresholds:** define objective recall/precision targets and acceptance tests per repo size before tuning decisions.
- **Embedding migration playbook:** codify dual-write/dual-read cutover steps and rollback criteria for model changes.
- **Branch/worktree memory policy:** decide whether isolation is by repo only or repo+branch/worktree for v1.x.
- **Index freshness UX contract:** define how stale-index states are surfaced and how users trigger remediation.

## Sources

### Primary (HIGH confidence)
- Go official downloads/docs — current stable version line and toolchain guidance.
- PostgreSQL official docs/policy — supported versions, transactional semantics, `NOTIFY`, and locking patterns.
- pgvector official README/tags — extension compatibility and index/query behavior.
- OpenAI embeddings guide — model family, dimensions, and embedding configuration.
- Existing codebase docs/files — current architecture and implementation constraints.

### Secondary (MEDIUM confidence)
- Anthropic Claude Code docs — memory/CLI expectations in modern coding agents.
- Aider official docs — git-centric loop and safety expectations.
- Sourcegraph Cody docs — codebase-context and retrieval UX patterns.
- Continue CLI quickstart/docs — ecosystem baseline for local/CLI agent behavior.

### Tertiary (LOW confidence)
- Vendor benchmark materials (Qdrant/Weaviate filter performance comparisons) — directional only; validate with local workloads.
- Intermittently unavailable ecosystem pages noted in FEATURES research — use as weak signal, not design authority.

---
*Research completed: 2026-03-18*
*Ready for roadmap: yes*
