# Pitfalls Research

**Domain:** CLI coding agents with session persistence + repo-scoped semantic indexing
**Researched:** 2026-03-18
**Confidence:** MEDIUM-HIGH

## Critical Pitfalls

### Pitfall 1: Scope Checks as "Best Effort" Instead of Proof-Like Enforcement

**What goes wrong:**
Retrieval returns semantically good but out-of-scope chunks (other repos, stale copied indexes, or paths outside the bound repo), creating data leakage and wrong edits.

**Why it happens:**
Teams treat repo scope as a metadata hint (`repo_id` filter) instead of a hard gate enforced at every stage (indexing, retrieval, tool execution).

**How to avoid:**
Make scope a mandatory invariant:
- Canonicalize and validate all paths (`realpath`) at ingest and tool execution time.
- Require retrieval predicates that include immutable scope proof fields (repo root hash + file content proof hash), not just path strings.
- Fail closed: if proof fields are missing, drop the chunk from results.
- Add adversarial tests with symlinks, `../`, and copied index artifacts.

**Warning signs:**
- Same query returns files from a previously indexed repo.
- Retrieval occasionally shows absolute paths outside the session root.
- Scope filter can be disabled or omitted in some code path.

**Phase to address:**
Phase 1 - Scope Contract and Security Invariants

---

### Pitfall 2: Incremental Sync That Detects Modified Files but Misses Deletions/Renames

**What goes wrong:**
The index accumulates dead chunks from deleted or moved files, so search answers include code that no longer exists.

**Why it happens:**
Initial "changed files only" implementations focus on upserts and skip tombstones/renames. Many teams model index updates as insert-only.

**How to avoid:**
- Track three delta sets explicitly: added, modified, deleted.
- Store chunk ownership by stable file identity and support hard delete by file identity.
- Include rename handling as delete(old) + add(new) unless proven safe to migrate IDs.
- Block query serving from a sync epoch with unresolved delete operations.

**Warning signs:**
- Search returns file paths that no longer exist on disk.
- Index size grows while repo size shrinks.
- Re-index fixes "mystery" bad retrievals that incremental sync missed.

**Phase to address:**
Phase 2 - Merkle Delta + Index Lifecycle

---

### Pitfall 3: Unstable Chunk Identity (Path/Line-Based IDs) Causing Cache Thrash

**What goes wrong:**
Small edits trigger massive re-embedding because chunk IDs shift with line numbers/path movement. Caches become useless and indexing cost spikes.

**Why it happens:**
Chunk identity is derived from location instead of normalized content (plus deterministic chunking rules).

**How to avoid:**
- Use deterministic chunking and content-addressed chunk IDs (hash of normalized chunk content + parser/chunker version).
- Separate chunk identity from file location metadata.
- Version chunker/parsing rules explicitly; treat version bumps as controlled migrations.
- Add regression tests asserting unchanged chunks retain IDs across unrelated edits.

**Warning signs:**
- Tiny whitespace/refactor changes trigger near full-file re-embedding.
- Embedding cache hit rate drops sharply after harmless edits.
- Frequent "index churn" despite low semantic changes.

**Phase to address:**
Phase 2 - Chunking + Embedding Cache Design

---

### Pitfall 4: Background Indexing Without Snapshot/Epoch Isolation

**What goes wrong:**
Queries run against mixed index states (some files at N, others at N+1), creating contradictory retrieval and nondeterministic behavior.

**Why it happens:**
Background workers write directly to "live" index tables/collections with no read snapshot boundary.

**How to avoid:**
- Use index epochs: write into a new epoch, validate completeness, then atomically promote.
- Keep retrieval pinned to a single active epoch.
- Record sync watermark (repo root hash and timestamp) in query responses for traceability.
- Add chaos tests with concurrent writes/queries and forced worker restarts.

**Warning signs:**
- Same query yields different top hits within seconds with no repo changes.
- Retrieval cites chunks from both pre- and post-edit states.
- Hard-to-reproduce bugs disappear after full rebuild.

**Phase to address:**
Phase 3 - Background Sync and Concurrency Safety

---

### Pitfall 5: Session Persistence That Stores Messages but Not Decision State

**What goes wrong:**
Threads resume with chat history but lose critical execution context (active repo binding, tool constraints, index epoch, prior failed attempts), causing repeated mistakes.

**Why it happens:**
Persistence is designed as a chat log, not a reproducible execution record.

**How to avoid:**
- Persist structured session state alongside messages: repo scope binding, policy flags, tool outcomes, and retrieval provenance.
- Treat critical transitions as append-only events with idempotent replay.
- Add resume tests: kill process mid-task, restart, verify deterministic continuation.

**Warning signs:**
- "Resumed" sessions re-run already completed steps.
- Agent forgets bound repo or tool restrictions after restart.
- Postmortems cannot reconstruct why an action happened.

**Phase to address:**
Phase 1 - Persistence Schema and Replay Semantics

---

### Pitfall 6: Embedding Model/Dimension Drift Without Migration Protocol

**What goes wrong:**
Old and new embeddings coexist in one search space, degrading retrieval quality or causing hard dimension errors.

**Why it happens:**
Model name/dimension changes are treated as config updates rather than data migrations.

**How to avoid:**
- Include `embedding_model`, `embedding_dimensions`, and `embedding_version` in every stored vector record.
- Enforce query-time guard: search only vectors matching active embedding version.
- Run dual-write/dual-read migration windows when changing models.
- Refuse startup if configured dimensions don't match stored index schema.

**Warning signs:**
- Sudden relevance drop after embedding config change.
- Intermittent dimension mismatch errors.
- Retrieval quality differs by machine/environment.

**Phase to address:**
Phase 2 - Embedding Storage Contracts and Migration

---

### Pitfall 7: Over-Indexing Noise (generated/vendor/build/docs dump) Hurts Retrieval

**What goes wrong:**
Top-k is crowded by boilerplate, generated files, lockfiles, or vendored code; relevant app code is buried.

**Why it happens:**
Teams ingest "everything text-like" but skip curation thresholds and ignore rules harmonization.

**How to avoid:**
- Define explicit include/exclude policy for v1 (code + curated docs only).
- Respect `.gitignore` + project-specific ignore rules with explicit overrides.
- Add per-extension caps and file-size limits; downrank low-signal paths.
- Track retrieval attribution metrics (how often top results come from generated/vendor paths).

**Warning signs:**
- Top hits frequently come from vendored/generated directories.
- Index growth is dominated by non-source files.
- Users must manually re-query to avoid noisy results.

**Phase to address:**
Phase 2 - Corpus Curation and Index Scope

---

### Pitfall 8: Weak Observability Makes Retrieval Bugs Unfixable

**What goes wrong:**
When relevance or leakage issues appear, teams cannot diagnose whether cause is chunking, filtering, embeddings, or background sync race.

**Why it happens:**
No structured telemetry for "why this chunk was returned."

**How to avoid:**
- Log retrieval traces: query embedding version, filters applied, candidate counts per stage, final score components.
- Expose a debug command to print retrieval proof (scope checks passed, index epoch, chunk IDs).
- Add golden query set for regression (expected in-scope files and minimum recall).

**Warning signs:**
- Engineers rely on ad hoc printf debugging for search issues.
- Bugs are labeled "flaky" with no root cause.
- Regressions appear only after deployment, not in CI.

**Phase to address:**
Phase 4 - Evaluation Harness and Observability

---

## Technical Debt Patterns

Shortcuts that seem reasonable but create long-term problems.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Full re-index on every startup | Simple implementation | Slow startup, API cost blowups, poor UX | Temporary in prototype-only branch |
| String-path repo scoping only | Fast to ship | Path traversal/symlink leaks, weak guarantees | Never |
| No embedding version metadata | Less schema work | Painful migrations, silent relevance failures | Never |
| Single mutable "live" index | Fewer tables/collections | Mixed-state reads and race conditions | Never |
| Persist messages only | Minimal DB schema | Non-reproducible resume behavior | MVP only if resume is explicitly unsupported |

## Integration Gotchas

Common mistakes when connecting to external services.

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| OpenAI Embeddings API | Ignoring token budgeting and dimension config consistency | Enforce token counting pre-embed and pin model+dimension in config + schema |
| Vector store filtering | Applying filters as optional post-step | Make repo scope filter/proof mandatory at query plan construction |
| Git/worktree file state | Trusting mtime-only change detection | Use Merkle/content hashes for authoritative delta detection |
| Background worker + DB | Uncoordinated writes from multiple workers | Use leases/locks and idempotent jobs keyed by repo + epoch |

## Performance Traps

Patterns that work at small scale but fail as usage grows.

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Re-embedding unchanged chunks | High embedding bill, long sync | Content-addressed chunk cache + deterministic chunking | Hundreds of files with frequent edits |
| Overly broad top-k before filtering | Latency spikes, irrelevant hits | Apply scope and corpus constraints first; then semantic rank | Medium repos with noisy docs/vendor |
| No checkpoint/compaction policy for local metadata DB | Growing disk usage, slow reads | Scheduled compaction/checkpoint and vacuum policy | Multi-day indexing sessions |
| Single-thread ingest pipeline | Backlog after modest repo churn | Bounded parallel ingest with per-file idempotency | Repos with >10k files |

## Security Mistakes

Domain-specific security issues beyond general web security.

| Mistake | Risk | Prevention |
|---------|------|------------|
| Trusting client-provided path metadata without canonical check | Data leakage outside repo scope | Canonicalize path and enforce "must be descendant of bound root" |
| Returning chunks without proving file possession/scope | Cross-repo leakage in retrieval | Require scope proof fields on every returned chunk |
| Allowing fallback to unfiltered semantic search on errors | Silent policy bypass | Fail closed; return explicit "scope validation failed" errors |
| Logging plaintext sensitive chunks in debug mode | Local secrets exposure in logs | Redact chunk text in logs; log IDs and hashes by default |

## UX Pitfalls

Common user experience mistakes in this domain.

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| "Indexing..." with no state detail | Users assume tool is hung | Show per-phase progress (scan, chunk, embed, promote epoch) |
| Silent degraded mode when index stale | Users trust wrong answers | Surface freshness badge and last synced repo hash/time |
| Non-actionable retrieval errors | Users retry blindly | Return remediation hints (reindex, scope mismatch, embedding version mismatch) |
| Query results without provenance | Hard to trust edits | Show path, chunk score, and index epoch in CLI output |

## "Looks Done But Isn't" Checklist

Things that appear complete but are missing critical pieces.

- [ ] **Repo scoping:** Often missing symlink/path traversal adversarial tests - verify escape attempts are blocked.
- [ ] **Incremental sync:** Often missing delete/rename tombstones - verify stale chunks cannot be returned.
- [ ] **Background indexing:** Often missing epoch isolation - verify retrieval never mixes epochs.
- [ ] **Embedding upgrades:** Often missing migration guardrails - verify old/new vectors never mix.
- [ ] **Session resume:** Often missing deterministic replay state - verify restart continues without policy drift.
- [ ] **Observability:** Often missing retrieval traceability - verify every result can explain "why returned."

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Scope leakage detected | HIGH | Disable semantic retrieval, rotate local caches, run scope-audit query suite, rebuild index with hard scope proofs |
| Stale/deleted chunks in results | MEDIUM | Stop incremental worker, run delete reconciliation, rebuild affected file/chunk mappings, re-enable worker |
| Embedding version drift | MEDIUM-HIGH | Freeze writes, mark old vectors inactive, dual-index migration, cut over after quality checks |
| Mixed-epoch retrieval race | MEDIUM | Introduce epoch pinning and atomic promotion, invalidate partial epochs, rerun sync |
| Resume state drift | MEDIUM | Add event-sourced state snapshots, backfill missing metadata for active threads, replay test suite |

## Pitfall-to-Phase Mapping

How roadmap phases should address these pitfalls.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Scope checks are best-effort | Phase 1 - Scope Contract and Security Invariants | Adversarial scope test suite (symlink, `../`, cross-repo cache) passes |
| Deletions/renames missed | Phase 2 - Merkle Delta + Index Lifecycle | Delete/rename integration tests confirm zero stale chunks |
| Unstable chunk IDs | Phase 2 - Chunking + Embedding Cache Design | Cache hit-rate and unchanged-ID regression tests pass |
| Mixed-state background reads | Phase 3 - Background Sync and Concurrency Safety | Concurrent query/sync test confirms single-epoch results |
| Message-only persistence | Phase 1 - Persistence Schema and Replay Semantics | Crash/resume deterministic replay tests pass |
| Embedding model drift | Phase 2 - Embedding Storage Contracts and Migration | Startup schema guard + migration canary queries pass |
| Corpus noise over-indexed | Phase 2 - Corpus Curation and Index Scope | Retrieval attribution from excluded paths stays below threshold |
| No retrieval observability | Phase 4 - Evaluation Harness and Observability | Every search result includes trace/provenance metadata |

## Sources

- Cursor: [Securely indexing large codebases](https://cursor.com/blog/secure-codebase-indexing) (official, 2026) - scope proofs, Merkle sync, reusable indexing.
- Cursor Docs: [Semantic & agentic search](https://cursor.com/docs/context/codebase-indexing) (official) - chunk indexing lifecycle, auto-sync behavior.
- OpenAI Docs: [Embeddings guide](https://platform.openai.com/docs/guides/embeddings) (official) - model dimensions, token constraints, dimension controls.
- Pinecone Docs: [Filter by metadata](https://docs.pinecone.io/guides/search/filter-by-metadata) (official) - filter operators and limits.
- SQLite Docs: [Write-Ahead Logging](https://www.sqlite.org/wal.html) (official, updated 2026-03-13) - concurrency/checkpoint behavior and WAL hazards.
- Qdrant: [Filtered search benchmark](https://qdrant.tech/benchmarks/filtered-search-intro) (vendor benchmark, 2023) - pre/post-filter pitfalls and ANN filtering tradeoffs.
- Weaviate Docs: [Filters](https://weaviate.io/developers/weaviate/search/filters) (official) - filter/indexing considerations and performance caveats.

---
*Pitfalls research for: deep-cli-agent semantic indexing milestone*
*Researched: 2026-03-18*
