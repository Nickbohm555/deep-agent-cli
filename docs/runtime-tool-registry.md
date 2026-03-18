# Runtime Tool Registry

The single inspection path for registered tools is `internal/tools/registry/static.go`.

## What Lives There

`internal/tools/registry/static.go` exposes the phase-1 tool catalog through:

- `Definitions()` for static enumeration.
- `StaticRegistry.ListTools(...)` for runtime listing.
- `StaticRegistry.LookupTool(...)` for runtime lookup by name.

Each `runtime.ToolDefinition` includes:

- `Name`
- `Description`
- `Schema`
- `Handler`
- `HandlerName`

The current static registry includes:

- `read_file`
- `list_files`
- `bash`
- `code_search`

## How Schema Metadata Is Produced

- `internal/tools/registry/schema.go` generates JSON schema from the typed input structs owned by `internal/tools/handlers`.
- `internal/tools/registry/static.go` binds each generated schema to its handler in the same definition entry.
- Registry tests assert strict object-schema expectations, including required fields and `additionalProperties: false`.

## How To Inspect Registered Tools

From the CLI:

```bash
go run ./cmd/deep-agent-cli -registry
```

From code:

```go
tools := registry.Definitions()
for _, tool := range tools {
    fmt.Println(tool.Name, tool.HandlerName)
}
```

From tests:

```bash
go test ./internal/tools/registry -v
go test ./internal/runtime -v
go test ./...
```

## Relevant Source Areas

- `internal/tools/registry/static.go`
- `internal/tools/registry/schema.go`
- `internal/tools/handlers`
- `internal/runtime`

These paths are documented together so contributors can inspect runtime structure, registry metadata, and concrete tool handlers without reverse-engineering package ownership.
