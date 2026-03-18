# Stack Research

**Domain:** Repo-scoped Go CLI coding agent with persistent sessions, Merkle indexing, and semantic code+docs retrieval
**Researched:** 2026-03-18
**Confidence:** HIGH

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended | Confidence |
|------------|---------|---------|-----------------|------------|
| Go | `1.26.x` | Primary runtime and CLI implementation language | Current stable line from official downloads; best fit for Go-first constraint and existing codebase (`go 1.24.2` already in use, so upgrade path is incremental) | HIGH |
| PostgreSQL | `17.9` (recommended) or `18.3` | Durable session memory, metadata store, and retrieval backend | Mature transactional storage + native SQL features for hybrid retrieval; supports local Docker workflows and strict repo/session scoping via schema constraints | HIGH |
| pgvector (Postgres extension) | `0.8.x` (current `0.8.2`) | Vector storage and ANN search (HNSW/IVFFlat) for semantic retrieval | De facto standard for "single Postgres" semantic retrieval; avoids introducing a second distributed datastore for v1 | HIGH |
| OpenAI Embeddings API | `text-embedding-3-small` default (1536 dims), optional `text-embedding-3-large` with `dimensions` | Embedding generation for code and docs chunks | Current OpenAI embedding models are performant and support dimension control, which helps tune recall/latency/cost for prototype constraints | HIGH |

### Supporting Libraries

| Library | Version | Purpose | When to Use | Confidence |
|---------|---------|---------|-------------|------------|
| `github.com/jackc/pgx/v5` | `v5.8.x` | Postgres driver + pooling + transactions | Default DB access layer for all session/index/retrieval operations | HIGH |
| `github.com/pgvector/pgvector-go` | `v0.3.x` | Go type support for pgvector columns and query bindings | Use with pgx for vector insert/search ergonomics | HIGH |
| `github.com/openai/openai-go/v3` | `v3.29.x` | OpenAI API client | Use for embedding generation and future model-based reranking | HIGH |
| `github.com/spf13/cobra` | `v1.10.x` | Command routing, subcommands, flags | Use if CLI command surface is expanding (sessions/index/search/admin commands) | HIGH |
| `github.com/pressly/goose/v3` | `v3.27.x` | SQL migration management | Use for repeatable schema evolution (sessions, chunks, embeddings, Merkle nodes) | HIGH |
| `github.com/sqlc-dev/sqlc` | `v1.30.x` | Type-safe query codegen from SQL | Use when query count grows and you want compile-time query safety | HIGH |
| `github.com/zeebo/blake3` | `v0.2.4` | Fast content hashing for chunk and tree node IDs | Use for Merkle leaf/intermediate hash computation in indexing pipeline | MEDIUM |
| `github.com/cespare/xxhash/v2` | `v2.3.0` | Fast non-cryptographic fingerprints for change detection prefilter | Use before BLAKE3 to skip recompute on unchanged blobs during scans | MEDIUM |
| `github.com/tree-sitter/go-tree-sitter` | `v0.24.0` | Syntax-aware chunk boundaries and symbol extraction | Use for code chunking quality; keep docs chunking simpler (token/section based) in v1 | MEDIUM |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| Docker Compose v2 | Local Postgres + pgvector runtime | Pin image family (`pgvector/pgvector:pg17` or `pg18`) and volume-mount for durable local dev |
| `go test` + benchmark tests | Validate retrieval quality + indexing latency | Add small golden corpus for regression tests on chunking and ranking |
| `golangci-lint` | Static checks and consistency | Run in CI/pre-commit to keep fast-moving prototype safe |
| `air` (optional) | Iterative dev loop for CLI command work | Helpful but not required; skip if command startup is already fast |

## Installation

```bash
# Core runtime deps
go get github.com/jackc/pgx/v5 github.com/pgvector/pgvector-go github.com/openai/openai-go/v3

# CLI + migrations
go get github.com/spf13/cobra github.com/pressly/goose/v3

# Indexing + parsing
go get github.com/zeebo/blake3 github.com/cespare/xxhash/v2 github.com/tree-sitter/go-tree-sitter

# Dev tooling (install binaries)
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| Postgres + pgvector | Dedicated vector DB (Qdrant/Weaviate/Pinecone) | Use when multi-tenant scale or cross-repo global retrieval becomes primary and DB ops are already staffed |
| `text-embedding-3-small` | `text-embedding-3-large` | Use for quality-critical ranking where higher embedding cost/latency is acceptable |
| Goose migrations | `golang-migrate` (`v4.19.x`) | Use if team already standardizes on migrate CLI workflows across services |
| Tree-sitter chunking | Regex/token chunking only | Use only for very early throwaway spike; switch once recall/precision matters for code search |
| pgx + sqlc | ORM-heavy approach (GORM/Ent-first) | Use if you need fast schema iteration over strict SQL control; expect less predictable retrieval query tuning |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| Separate vector store in v1 | Adds infra complexity, dual-write risk, and more operational surface for a prototype | Postgres + pgvector first |
| Legacy OpenAI embedding models for new work | Inferior quality/cost profile vs embedding-3 family | `text-embedding-3-small` default, `-3-large` when needed |
| Full re-index on every run | Too slow for repo-scale iteration and burns embedding quota | Merkle + fast fingerprint delta indexing |
| Over-engineered distributed indexing pipeline | Premature complexity for single-repo-per-session constraint | Single-process worker pool + transactional checkpoints |
| Pure lexical retrieval only | Misses semantic matches in refactors/renames and docs-code mapping | Hybrid retrieval (vector + lexical + recency/structure scoring) |

## Stack Patterns by Variant

**If optimizing for fastest v1 prototype:**
- Use `text-embedding-3-small` (1536 dims), Postgres `17.x`, pgvector `0.8.x`, and simple hybrid retrieval (vector + `tsvector` keyword score).
- Because this minimizes operational complexity and keeps latency/cost predictable.

**If optimizing for retrieval quality over cost:**
- Use `text-embedding-3-large` with explicit `dimensions` tuning (e.g., 1024/1536), add reranking stage, and richer tree-sitter chunk metadata.
- Because larger embeddings + structural signals generally improve code intent matching.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| PostgreSQL `17.x` / `18.x` | pgvector `0.8.x` | pgvector README states support for Postgres 13+; prefer supported Postgres majors only |
| `github.com/pgvector/pgvector-go@v0.3.x` | `github.com/jackc/pgx/v5@v5.8.x` | Standard pairing for Go + Postgres vector operations |
| `text-embedding-3-small` (1536 default) | pgvector `vector(1536)` | Keep DB column dimensions aligned to model/dimensions parameter |
| `text-embedding-3-large` (3072 default) | pgvector `vector(3072)` or shortened via `dimensions` | If shortening dimensions, schema must match chosen size |
| Current repo `go 1.24.2` | Go `1.26.x` target | Upgrade is recommended to align with current toolchain and dependency evolution |

## Sources

- [Go downloads](https://go.dev/dl/) - verified current stable series (`go1.26.1`)
- [PostgreSQL versioning policy](https://www.postgresql.org/support/versioning/) - verified supported majors/minors as of 2026-03
- [pgvector README](https://raw.githubusercontent.com/pgvector/pgvector/master/README.md) - verified Postgres support and Docker examples
- [pgvector tags](https://api.github.com/repos/pgvector/pgvector/tags) - verified current extension version line (`v0.8.2`)
- [OpenAI embeddings guide](https://platform.openai.com/docs/guides/embeddings) - verified model family and default dimensions (`1536`/`3072`) and `dimensions` parameter
- [openai-go tags](https://api.github.com/repos/openai/openai-go/tags) - verified current SDK tag (`v3.29.0`)
- [pgx tags](https://api.github.com/repos/jackc/pgx/tags?per_page=1) - verified current driver tag (`v5.8.0`)
- [cobra tags](https://api.github.com/repos/spf13/cobra/tags) - verified current CLI framework tag (`v1.10.2`)
- [goose tags](https://api.github.com/repos/pressly/goose/tags) - verified migration tool tag line (`v3.27.0`)
- [sqlc tags](https://api.github.com/repos/sqlc-dev/sqlc/tags) - verified current sqlc tag (`v1.30.0`)
- [pgvector-go tags](https://api.github.com/repos/pgvector/pgvector-go/tags) - verified Go vector helper tag (`v0.3.0`)
- [BLAKE3 Go tags](https://api.github.com/repos/zeebo/blake3/tags) - verified hashing library tag (`v0.2.4`)
- [xxhash tags](https://api.github.com/repos/cespare/xxhash/tags) - verified fast hash tag (`v2.3.0`)
- [go-tree-sitter tags](https://api.github.com/repos/tree-sitter/go-tree-sitter/tags) - verified parser binding tag (`v0.24.0`)

---
*Stack research for: repo-scoped Go CLI coding agents with semantic retrieval*
*Researched: 2026-03-18*
