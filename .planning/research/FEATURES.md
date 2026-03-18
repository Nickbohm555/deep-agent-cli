# Feature Research

**Domain:** Repo-scoped Go CLI coding agents for local-first solo workflows
**Researched:** 2026-03-18
**Confidence:** MEDIUM-HIGH

## Feature Landscape

### Table Stakes (Users Expect These)

Features users assume exist. Missing these = product feels incomplete.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Interactive + headless execution modes | Modern coding agents support both TUI/chat loops and non-interactive script mode for automation | MEDIUM | Must support human-in-the-loop and CI-style single prompt execution |
| Git-aware safe editing workflow | Users expect agent changes to be reviewable/undoable with clear diffs and commits | MEDIUM | Keep edits auditable; avoid hidden state mutations |
| Session resume in-repo | Leading tools expose resume/continue semantics so work can continue without restating context | MEDIUM | Baseline is "continue last session"; in your case this should be repo-scoped threads |
| Permissioned tool execution | Users expect explicit control over file edits/shell execution in local agents | MEDIUM | Allow/deny patterns and safe defaults are baseline trust features |
| Repo context retrieval | Users expect the agent to find relevant code across files instead of relying only on open file context | HIGH | Start with deterministic file tools plus keyword search before semantic ranking |
| Configurable model + provider wiring | Users expect API key/model selection and reproducible config per environment | LOW | Should include clear env/config precedence and failure messaging |

### Differentiators (Competitive Advantage)

Features that set the product apart. Not required, but valuable.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Postgres-backed thread/message history with strict repo scoping | Durable, queryable history beyond flat local files; clean separation per repository and branch/worktree policy | HIGH | Strong differentiator for continuity, analytics, and deterministic recall boundaries |
| Merkle-based incremental indexing | Fast re-index after small changes; predictable cost for large repos | HIGH | Better than full re-embed cycles; enables frequent refresh during active coding |
| Semantic search tool returning chunks + paths + scores | Improves answer grounding and debugging by exposing retrieval evidence, not opaque context injection | HIGH | Score visibility improves trust and allows downstream rerank/tuning |
| OpenAI embeddings with replaceable embedding backend boundary | Good retrieval quality now, with future swap path for local/self-hosted embeddings | MEDIUM | Design adapter so embedding provider is not hard-coded into index pipeline |
| Session-memory + retrieval interplay policy | Promote "memory references retrieval results" over free-form long memory, reducing drift/hallucinated continuity | HIGH | Distinct UX advantage for long-running projects with many sessions |
| Clean tool scaffolding (first-class tool contracts) | Makes adding tools predictable and safer; improves contributor velocity | MEDIUM | Helps maintainability as the CLI grows from single-agent to richer workflows |

### Anti-Features (Commonly Requested, Often Problematic)

Features that seem good but create problems.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Full autonomous background coding loops by default | "Hands-free coding" appeal | High risk of runaway edits, token cost spikes, and low trust in solo local workflows | Keep explicit approval checkpoints and turn limits; allow opt-in automation modes |
| Multi-repo shared memory in v1 | Feels convenient for "global assistant memory" | Cross-project leakage, incorrect assumptions, and privacy boundary erosion | Enforce repo-scoped memory/index; add explicit import/share primitives later |
| Real-time indexing on every keystroke | Perceived as "always fresh" | CPU churn, noisy embeddings, and poor local laptop ergonomics | Debounced/incremental indexing on file save + explicit reindex command |
| Overly complex multi-agent orchestration in v1 | Sounds advanced and powerful | Large reliability/debug tax before core single-agent UX is solid | Prioritize one robust agent loop with clear tool contracts, then add specialized agents |
| Hidden retrieval (no paths/scores shown) | Cleaner UI on surface | Hard to debug poor results and impossible to calibrate relevance | Always return evidence: chunk text, file path, and similarity score |

## Feature Dependencies

```
[Repo identity + scoping model]
    └──requires──> [Session persistence (thread/message schema)]
                        └──requires──> [Postgres storage layer + migrations]
                                              └──requires──> [Tool/service interfaces]

[File chunking strategy]
    └──requires──> [Merkle incremental indexing]
                        └──requires──> [Embedding pipeline (OpenAI adapter)]
                                              └──requires──> [Vector storage + metadata]
                                                    └──requires──> [Semantic search API]
                                                          └──requires──> [Chunk+path+score response contract]

[Session recall UX] ──enhances──> [Repo-scoped session persistence]
[Semantic search evidence output] ──enhances──> [User trust + debugging]

[Global cross-repo memory] ──conflicts──> [Strict repo-scoped privacy/correctness]
[Always-on reindexing] ──conflicts──> [Local-first performance constraints]
```

### Dependency Notes

- **Repo scoping requires a canonical repo identity:** without deterministic repo IDs, persistence and retrieval boundaries will leak.
- **Session persistence requires stable schema before UX polish:** lock thread/message model first, then add resume/list/filter UX.
- **Merkle incremental indexing depends on stable chunking:** if chunk boundaries are unstable, Merkle invalidation efficiency collapses.
- **Semantic search quality depends on metadata richness:** chunk IDs, file paths, symbols, and commit/file hash snapshots are required for usable ranking/debugging.
- **Evidence output (chunk/path/score) should be part of v1 contract:** retrofitting observability later is expensive and breaks clients.

## MVP Definition

### Launch With (v1)

Minimum viable product - what's needed to validate the concept.

- [ ] Repo-scoped session persistence in Postgres (threads/messages + resume/list)
- [ ] Deterministic repo identity + scope enforcement across all session/history queries
- [ ] Merkle-based incremental indexing pipeline for changed files
- [ ] OpenAI embedding integration behind an adapter interface
- [ ] Semantic search tool returning top-k chunks with `path` and `score`
- [ ] Basic safety controls (tool permission prompts, dry-run/read-only mode)
- [ ] Clean tool scaffolding for adding future tools without rewrites

### Add After Validation (v1.x)

Features to add once core is working.

- [ ] Hybrid retrieval (keyword + semantic + rerank) - add when semantic-only misses obvious exact matches
- [ ] Retrieval diagnostics command (`why-these-chunks`) - add when tuning relevance in larger repos
- [ ] Branch/worktree-aware memory partitioning - add when users run parallel feature branches frequently
- [ ] Optional local embedding provider - add when cost/privacy pressure appears

### Future Consideration (v2+)

Features to defer until product-market fit is established.

- [ ] Cross-repo knowledge sharing with explicit linking policies - defer until repo-scoped correctness is proven
- [ ] Multi-agent planner/executor decomposition - defer until single-agent reliability and observability are strong
- [ ] Remote/team sync of histories and indexes - defer until local-first solo workflow is mature

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Repo-scoped Postgres sessions (threads/messages) | HIGH | MEDIUM | P1 |
| Merkle incremental indexing | HIGH | HIGH | P1 |
| Semantic search returns chunks + paths + scores | HIGH | MEDIUM | P1 |
| OpenAI embeddings adapter | HIGH | MEDIUM | P1 |
| Tool permission/safety controls | HIGH | LOW | P1 |
| Hybrid retrieval + reranking | HIGH | HIGH | P2 |
| Branch/worktree-aware partitions | MEDIUM | MEDIUM | P2 |
| Local embedding provider option | MEDIUM | MEDIUM | P2 |
| Multi-agent orchestration | LOW | HIGH | P3 |

**Priority key:**
- P1: Must have for launch
- P2: Should have, add when possible
- P3: Nice to have, future consideration

## Competitor Feature Analysis

| Feature | Competitor A (Claude Code) | Competitor B (Aider) | Our Approach |
|---------|-----------------------------|----------------------|--------------|
| Session continuity | Resume/continue workflows + memory mechanisms | Strong git-centric loop; less emphasis on structured DB-backed threads | Repo-scoped Postgres threads/messages with explicit retrieval boundaries |
| Repo context | Uses repository context and explicit context controls | Codebase mapping plus file-focused editing loop | Semantic search with explicit chunk/path/score outputs for transparency |
| Safety + control | Permission/tool control modes and scoped settings | Git-first undoability and auto-commit workflow | Permissioned tools + auditable history + deterministic session scopes |
| Indexing strategy | Not primary surfaced differentiator | Repomap and context mapping | Merkle incremental indexing optimized for local-first Go CLI |

## Sources

- Anthropic Claude Code docs (memory + CLI reference):  
  - <https://docs.anthropic.com/en/docs/claude-code/memory>  
  - <https://code.claude.com/docs/en/cli-reference>  
  - Confidence: HIGH
- Aider official docs + site:  
  - <https://aider.chat/docs/git.html>  
  - <https://aider.chat/>  
  - Confidence: HIGH
- Continue CLI official docs (quickstart):  
  - <https://docs.continue.dev/cli/quickstart>  
  - Confidence: MEDIUM
- Sourcegraph Cody docs (chat + codebase context):  
  - <https://sourcegraph.com/docs/cody/capabilities/chat#use-codebase-context>  
  - Confidence: HIGH
- Continue codebase-embeddings pages were intermittently unavailable during fetch; retained as LOW-confidence ecosystem signal from discovery results and excluded from normative claims.

---
*Feature research for: repo-scoped Go CLI coding agent*
*Researched: 2026-03-18*
