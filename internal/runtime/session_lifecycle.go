package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

type SessionLifecycleParams struct {
	ThreadID string
	RepoRoot string
}

type SessionBootstrap struct {
	Session  session.Session
	Messages []session.Message
	Resumed  bool
}

func CreateOrResumeSession(ctx context.Context, store session.SessionStore, params SessionLifecycleParams) (SessionBootstrap, error) {
	if store == nil {
		return SessionBootstrap{}, fmt.Errorf("session store is required")
	}

	threadID := strings.TrimSpace(params.ThreadID)
	repoRoot, err := resolveRepoRoot(params.RepoRoot)
	if err != nil {
		return SessionBootstrap{}, err
	}

	if threadID == "" {
		created, err := store.CreateSession(ctx, session.CreateSessionParams{
			RepoRoot: repoRoot,
		})
		if err != nil {
			return SessionBootstrap{}, fmt.Errorf("create session: %w", err)
		}

		return SessionBootstrap{
			Session: created,
		}, nil
	}

	resumed, err := store.ResumeSession(ctx, threadID)
	if err != nil {
		return SessionBootstrap{}, fmt.Errorf("resume session %q: %w", threadID, err)
	}

	messages, err := store.ListMessages(ctx, threadID)
	if err != nil {
		return SessionBootstrap{}, fmt.Errorf("list session messages %q: %w", threadID, err)
	}

	return SessionBootstrap{
		Session:  resumed,
		Messages: messages,
		Resumed:  true,
	}, nil
}

func resolveRepoRoot(repoRoot string) (string, error) {
	trimmed := strings.TrimSpace(repoRoot)
	if trimmed == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve repo root: %w", err)
		}
		trimmed = wd
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve repo root %q: %w", trimmed, err)
	}

	return abs, nil
}
