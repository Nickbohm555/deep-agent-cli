package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

type SessionLifecycleParams struct {
	ThreadID string
	RepoRoot string
}

type SessionBootstrap struct {
	Session      session.Session
	Messages     []session.Message
	Conversation []Message
	Resumed      bool
}

func CreateOrResumeSession(ctx context.Context, store session.SessionStore, params SessionLifecycleParams) (SessionBootstrap, error) {
	if store == nil {
		return SessionBootstrap{}, fmt.Errorf("session store is required")
	}

	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		repoRoot, err := resolveRepoRoot(params.RepoRoot)
		if err != nil {
			return SessionBootstrap{}, err
		}

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
	if err := session.EnsureSessionRepoRootImmutable(resumed, params.RepoRoot); err != nil {
		return SessionBootstrap{}, err
	}

	messages, err := store.ListMessages(ctx, threadID)
	if err != nil {
		return SessionBootstrap{}, fmt.Errorf("list session messages %q: %w", threadID, err)
	}

	return SessionBootstrap{
		Session:      resumed,
		Messages:     messages,
		Conversation: rehydrateConversation(messages),
		Resumed:      true,
	}, nil
}

func rehydrateConversation(messages []session.Message) []Message {
	conversation := make([]Message, 0, len(messages))
	for _, persisted := range messages {
		role := MessageRole(strings.TrimSpace(persisted.Role))
		switch role {
		case MessageRoleUser, MessageRoleAssistant, MessageRoleTool, MessageRoleSystem:
		default:
			role = MessageRoleAssistant
		}

		conversation = append(conversation, Message{
			Role:    role,
			Content: persisted.Content,
		})
	}

	return conversation
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

	root, err := session.CanonicalizeRepoRoot(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}

	return root, nil
}
