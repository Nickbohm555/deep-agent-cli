package runtime

import (
	"context"
	"fmt"

	"github.com/Nickbohm555/deep-agent-cli/internal/tools/safety"
)

type ToolExecutor struct {
	Registry Registry
	RepoRoot string
	Mode     safety.ToolMode
}

func NewToolExecutor(registry Registry, repoRoot string, mode safety.ToolMode) *ToolExecutor {
	return &ToolExecutor{
		Registry: registry,
		RepoRoot: repoRoot,
		Mode:     mode,
	}
}

func (e *ToolExecutor) Dispatch(ctx context.Context, call ToolCall) (ToolResult, error) {
	return e.Execute(ctx, call)
}

func (e *ToolExecutor) Execute(ctx context.Context, call ToolCall) (ToolResult, error) {
	if e == nil {
		return ToolResult{}, fmt.Errorf("execute tool %q: tool executor is nil", call.Name)
	}
	if e.Registry == nil {
		return ToolResult{}, fmt.Errorf("execute tool %q: no tool registry configured", call.Name)
	}

	tool, ok, err := e.Registry.LookupTool(ctx, call.Name)
	if err != nil {
		return ToolResult{}, fmt.Errorf("execute tool %q: lookup tool: %w", call.Name, err)
	}
	if !ok {
		return ToolResult{}, fmt.Errorf("execute tool %q: tool not registered", call.Name)
	}
	if tool.Handler == nil {
		return ToolResult{}, fmt.Errorf("execute tool %q: tool handler is nil", call.Name)
	}

	execCtx, err := e.bindSafetyContext(ctx)
	if err != nil {
		return ToolResult{}, fmt.Errorf("execute tool %q: %w", call.Name, err)
	}

	return tool.Handler(execCtx, call)
}

func (e *ToolExecutor) bindSafetyContext(ctx context.Context) (context.Context, error) {
	repoRoot := e.RepoRoot
	if repoRoot == "" {
		boundRepoRoot, err := RepoRootFromContext(ctx)
		if err != nil {
			return nil, err
		}
		repoRoot = boundRepoRoot
	}

	execCtx, err := WithRepoRoot(ctx, repoRoot)
	if err != nil {
		return nil, err
	}

	mode := e.Mode
	if mode == "" {
		mode = ToolSafetyModeFromContext(ctx)
	}

	return WithToolSafetyMode(execCtx, mode), nil
}
