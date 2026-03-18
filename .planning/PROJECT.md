# deep-cli-agent

## What This Is

`deep-cli-agent` is a Go CLI coding agent for solo development workflows, scoped to a single repository per session. It provides a ReAct-style tool-calling loop with local developer tools, then adds persistent thread history and semantic code search over the bound repo. The immediate goal is a clean, explicit architecture that makes tool inventory, tool dispatch, and agent scaffolding easy to understand and extend.

## Core Value

A repo-scoped CLI agent can reliably answer and act with the right local context by combining persistent session memory and fast semantic search over the repository.

## Requirements

### Validated

- ✓ Agent REPL loop with iterative tool-use responses from the model — existing
- ✓ Structured tool definitions with names, descriptions, input schema, and handlers — existing
- ✓ Existing local tooling includes `read_file`, `list_files`, `bash`, and `code_search` — existing
- ✓ JSON-schema-based tool input contracts generated from Go structs — existing

### Active

- [ ] Refactor to a clean architecture that clearly separates agent runtime, tool registry, and tool implementations.
- [ ] Preserve and migrate existing tools (`read_file`, `list_files`, `bash`, `code_search`) into the new scaffold without behavior regressions.
- [ ] Introduce persistent session/thread storage in Postgres (Docker-based local setup) with message history per thread.
- [ ] Support initializing an agent session that is strictly scoped to a single repository path.
- [ ] Enforce strict repo sandbox boundaries for file and command tools by default.
- [ ] Build repository indexing for code + docs files, scoped to the bound repo.
- [ ] Implement a Merkle-tree-based sync detector to identify changed files and drive incremental re-indexing.
- [ ] Use an OpenAI embedding model to embed chunks and store/search vectors for semantic retrieval.
- [ ] Expose semantic search as both internal retrieval capability and callable tool, returning top chunks with file paths and scores.
- [ ] Run indexing/sync in a background workflow so sessions remain responsive.
- [ ] Deliver a minimum vertical slice: create session -> index repo -> run semantic query end-to-end.

### Out of Scope

- Multi-user auth/accounts — solo local workflow is the target for v1.
- Distributed/shared remote index — not required for initial local repo-scoped experience.
- IDE/editor integration — CLI-first product direction for this milestone.
- Advanced reranking pipelines — defer until baseline embedding retrieval is validated.

## Context

- Current codebase is a brownfield Go CLI with duplicated entrypoint patterns and tool wiring across files.
- Existing architecture docs identify drift risk from repeated `main`/`Agent`/tool registration code and no package layering.
- Current known tools in active entrypoint flow are `read_file`, `list_files`, `bash`, `code_search`; these must be retained.
- Project direction is rapid prototype speed, but with enough structure to avoid compounding technical debt.
- Semantic indexing design is inspired by Cursor's Merkle-tree-driven sync + chunk embedding approach for fast updates and query readiness.

## Constraints

- **Tech stack**: Go-first CLI implementation with existing agent/tool runtime patterns — preserve momentum and compatibility.
- **Database**: Postgres for session and thread persistence — chosen explicitly over SQLite/JSON.
- **Runtime scope**: Single repo path per session with strict sandboxing — prevents cross-repo leakage and unclear context.
- **Embedding provider**: OpenAI embeddings in v1 — minimize provider abstraction complexity initially.
- **Index scope**: Code + docs only (text-like files) for v1 — focus signal quality and speed.
- **Timeline**: Rapid prototype — prioritize a thin end-to-end slice before broad feature expansion.
- **Execution model**: Background sync/index process — avoid blocking chat/tool workflow.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Project name is `deep-cli-agent` | Clear identity for the refactor and roadmap artifacts | — Pending |
| Build a working end-to-end vertical slice first | De-risks architecture by proving real flow early | — Pending |
| Keep single-user local focus for v1 | Reduces complexity and shortens feedback loop | — Pending |
| Use Postgres (Docker local setup) for session/thread history | Provides durable relational model for threads/messages | — Pending |
| Use strict one-repo-per-session scoping | Ensures context correctness and safer tool execution | — Pending |
| Preserve existing tool names from current implementation | Avoids regressions while introducing new architecture | — Pending |
| Use ReAct-style loop with static typed tool registry | Keeps tool orchestration explicit and extensible | — Pending |
| Use OpenAI embeddings and Merkle-based incremental sync | Matches desired semantic indexing design direction | — Pending |

---
*Last updated: 2026-03-18 after initialization*
