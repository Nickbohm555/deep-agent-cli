package runtime

import (
	"context"
	"fmt"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

type repoRootContextKey struct{}

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
