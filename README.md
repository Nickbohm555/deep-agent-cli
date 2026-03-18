# deep-agent-cli

CLI-focused agent tooling extracted from `how-to-build-a-coding-agent`.

## Run

Interactive mode:

```bash
go run ./cmd/deep-agent-cli -mode interactive
```

One-shot mode:

```bash
go run ./cmd/deep-agent-cli -mode oneshot -prompt "summarize the current directory"
```

Tool registry inspection:

```bash
go run ./cmd/deep-agent-cli -registry
```

Set `OPENAI_API_KEY` to enable model-backed responses. Without it, the CLI stays runnable in a local fallback mode so you can still verify prompts, flags, and terminal behavior.

## Contents

- Core Go tools: `bash_tool.go`, `chat.go`, `code_search_tool.go`, `edit_tool.go`, `list_files.go`, `read.go`
- Dependencies in `go.mod` and `go.sum`

## Notes

This repo is intentionally minimal; it only includes the core agent CLI code and its dependencies.
