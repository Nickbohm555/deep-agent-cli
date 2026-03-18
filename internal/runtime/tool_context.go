package runtime

import (
	"context"
	"fmt"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/safety"
)

type repoRootContextKey struct{}
type toolSafetyModeContextKey struct{}

func WithRepoRoot(ctx context.Context, repoRoot string) (context.Context, error) {
	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("bind repo root to tool context: %w", err)
	}

	return context.WithValue(ctx, repoRootContextKey{}, canonicalRoot), nil
}

func RepoRootFromContext(ctx context.Context) (string, error) {
	repoRoot, ok := ctx.Value(repoRootContextKey{}).(string)
	if !ok || repoRoot == "" {
		return "", fmt.Errorf("tool execution requires a bound repository root")
	}

	return repoRoot, nil
}

func WithToolSafetyMode(ctx context.Context, mode safety.ToolMode) context.Context {
	if mode == "" {
		return ctx
	}

	return context.WithValue(ctx, toolSafetyModeContextKey{}, mode)
}

func ToolSafetyModeFromContext(ctx context.Context) safety.ToolMode {
	mode, _ := ctx.Value(toolSafetyModeContextKey{}).(safety.ToolMode)
	return mode
}
