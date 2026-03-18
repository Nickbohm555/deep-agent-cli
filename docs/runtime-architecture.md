# Runtime Architecture

This repository keeps the runtime scaffold split across three internal areas so orchestration, registry metadata, and tool implementations do not collapse into one package.

## Module Map

- `internal/runtime`
  - Owns execution contracts, turn orchestration, and the interactive/one-shot drivers.
  - `types.go` defines the shared DTOs and interfaces: `ProviderClient`, `Registry`, `ToolDispatcher`, and `TurnRunner`.
  - `orchestrator.go` owns the tool-calling loop and only talks to those interfaces.
  - `interactive.go` owns REPL input/output flow.
  - `oneshot.go` owns single-prompt execution flow.
- `internal/tools/registry`
  - Owns the static tool catalog exposed from one path.
  - `static.go` defines the registered tools, their descriptions, generated schemas, and bound handlers.
  - `schema.go` owns JSON schema generation helpers used by the registry.
- `internal/tools/handlers`
  - Owns tool-specific input structs and execution logic.
  - Current handlers are `read_file.go`, `list_files.go`, `bash.go`, and `code_search.go`.
- `cmd/deep-agent-cli`
  - Wires the CLI flags to the runtime entrypoints without moving orchestration logic into `main`.

## Ownership Rules

- `internal/runtime` should depend on contracts, not concrete provider adapters or concrete tool packages.
- `internal/tools/registry` is the single static source of truth for tool metadata and schema visibility.
- `internal/tools/handlers` should not own orchestration state; they only execute one tool call at a time.
- Provider adapters belong outside `internal/runtime` so the runtime contracts stay provider-agnostic.

## Inspection Path

To inspect the runtime scaffold quickly:

1. Read `internal/runtime/types.go` for the core contracts.
2. Read `internal/runtime/orchestrator.go` for the turn loop.
3. Read `internal/tools/registry/static.go` for the registered tools and schema bindings.
4. Read `internal/tools/handlers/*.go` for tool implementation details.

## Guardrails

- `go test ./internal/runtime -v` verifies the runtime boundary expectations and driver ownership contracts.
- `go test ./internal/tools/registry -v` verifies that the static registry stays complete and schema-backed.
