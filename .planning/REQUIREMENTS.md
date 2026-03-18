# Requirements: deep-cli-agent

**Defined:** 2026-03-18
**Core Value:** A repo-scoped CLI agent can reliably answer and act with the right local context by combining persistent session memory and fast semantic search over the repository.

## v1 Requirements

Requirements for the initial release. Each requirement maps to exactly one roadmap phase.

### Runtime & Interaction

- [ ] **RUNT-01**: User can run the agent in interactive CLI mode with a ReAct-style tool-calling loop.
- [ ] **RUNT-02**: User can run the agent in headless single-shot mode for automation workflows.
- [ ] **RUNT-03**: User can inspect a clear runtime scaffold that separates orchestration, tool registry, and tool handlers.

### Tools & Safety

- [ ] **TOOL-01**: User can use the existing tools `read_file`, `list_files`, `bash`, and `code_search` after the refactor without behavioral regressions.
- [ ] **TOOL-02**: User can see all registered tools from a typed static registry with metadata and schema.
- [ ] **TOOL-03**: User can enforce strict repo sandbox boundaries so file and command tools cannot operate outside the session repo.
- [ ] **TOOL-04**: User can run tools in a safety-controlled mode (permission-aware and/or read-only dry run).

### Session & Scope

- [ ] **SESS-01**: User can create and resume thread-scoped sessions persisted in Postgres.
- [ ] **SESS-02**: User can persist and retrieve message history per thread.
- [ ] **SESS-03**: User can bind each session to exactly one repository path and enforce that scope at query and tool-execution time.

### Indexing & Sync

- [ ] **INDX-01**: User can index code + docs files for the session-bound repository.
- [ ] **INDX-02**: System can detect add/modify/delete changes using a Merkle-tree sync model and perform incremental re-indexing.
- [ ] **INDX-03**: System can run index/sync jobs in the background without blocking interaction loops.
- [ ] **INDX-04**: System can generate and store embeddings with an OpenAI embedding model for indexed chunks.

### Semantic Retrieval

- [ ] **SRCH-01**: User can run semantic search over the bound repository as both internal retrieval and an explicit tool call.
- [ ] **SRCH-02**: User receives semantic search results as top chunks with file path and score.
- [ ] **SRCH-03**: User can complete a minimum vertical slice workflow: create session -> index repo -> semantic query.

## v2 Requirements

Deferred features tracked for follow-on milestones.

### Retrieval Enhancements

- **SRCH-10**: User can use hybrid retrieval (keyword + semantic + reranking) for improved precision/recall.
- **SRCH-11**: User can run retrieval diagnostics (`why-these-chunks`) to inspect ranking rationale.

### Session Partitioning

- **SESS-10**: User can optionally partition memory/index by repo + branch/worktree.

### Embedding Flexibility

- **INDX-10**: User can switch to optional local/self-hosted embedding providers.

## Out of Scope

| Feature | Reason |
|---------|--------|
| Multi-user auth/accounts | v1 is explicitly solo local workflow. |
| Distributed/shared remote index | v1 prioritizes local reliability and simplicity. |
| IDE/editor integration | CLI-first scope for this milestone. |
| Advanced reranking pipelines | Deferred until baseline semantic retrieval is validated. |
| Cross-repo shared memory | Conflicts with strict repo-scoped correctness/privacy goals in v1. |
| Full autonomous background coding loops by default | Too risky for trust, safety, and cost in early local-first workflow. |

## Traceability

Which phases cover which requirements. Populated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| RUNT-01 | TBD | Pending |
| RUNT-02 | TBD | Pending |
| RUNT-03 | TBD | Pending |
| TOOL-01 | TBD | Pending |
| TOOL-02 | TBD | Pending |
| TOOL-03 | TBD | Pending |
| TOOL-04 | TBD | Pending |
| SESS-01 | TBD | Pending |
| SESS-02 | TBD | Pending |
| SESS-03 | TBD | Pending |
| INDX-01 | TBD | Pending |
| INDX-02 | TBD | Pending |
| INDX-03 | TBD | Pending |
| INDX-04 | TBD | Pending |
| SRCH-01 | TBD | Pending |
| SRCH-02 | TBD | Pending |
| SRCH-03 | TBD | Pending |

**Coverage:**
- v1 requirements: 17 total
- Mapped to phases: 0
- Unmapped: 17

---
*Requirements defined: 2026-03-18*
*Last updated: 2026-03-18 after initial definition*
