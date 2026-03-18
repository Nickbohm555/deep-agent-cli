package session

import (
	"context"
	"errors"
)

var ErrSessionNotFound = errors.New("session not found")

type CreateSessionParams struct {
	ThreadID string
	RepoRoot string
}

type AppendMessageParams struct {
	ThreadID string
	Role     string
	Content  string
}

type SessionStore interface {
	CreateSession(ctx context.Context, params CreateSessionParams) (Session, error)
	ResumeSession(ctx context.Context, threadID string) (Session, error)
	AppendMessage(ctx context.Context, params AppendMessageParams) (Message, error)
	ListMessages(ctx context.Context, threadID string) ([]Message, error)
}
