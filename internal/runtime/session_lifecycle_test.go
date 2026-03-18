package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

func TestCreateOrResumeSessionCreatesNewThreadWhenThreadIDMissing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	store := &stubSessionStore{
		createSessionFn: func(_ context.Context, params session.CreateSessionParams) (session.Session, error) {
			if params.ThreadID != "" {
				t.Fatalf("CreateSession ThreadID = %q, want empty", params.ThreadID)
			}
			if params.RepoRoot == "" {
				t.Fatal("CreateSession RepoRoot should not be empty")
			}

			return session.Session{
				ThreadID:  "thread-new",
				RepoRoot:  params.RepoRoot,
				CreatedAt: now,
			}, nil
		},
	}

	bootstrap, err := CreateOrResumeSession(context.Background(), store, SessionLifecycleParams{
		RepoRoot: ".",
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession returned error: %v", err)
	}

	if bootstrap.Session.ThreadID != "thread-new" {
		t.Fatalf("Session.ThreadID = %q, want thread-new", bootstrap.Session.ThreadID)
	}
	if bootstrap.Resumed {
		t.Fatal("Resumed = true, want false")
	}
	if len(bootstrap.Messages) != 0 {
		t.Fatalf("Messages length = %d, want 0", len(bootstrap.Messages))
	}
	if store.resumeCalls != 0 {
		t.Fatalf("ResumeSession called %d times, want 0", store.resumeCalls)
	}
	if store.listCalls != 0 {
		t.Fatalf("ListMessages called %d times, want 0", store.listCalls)
	}
}

func TestCreateOrResumeSessionLoadsExistingThreadHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	store := &stubSessionStore{
		resumeSessionFn: func(_ context.Context, threadID string) (session.Session, error) {
			if threadID != "thread-123" {
				t.Fatalf("ResumeSession threadID = %q, want thread-123", threadID)
			}
			return session.Session{
				ThreadID:  "thread-123",
				RepoRoot:  "/repo",
				CreatedAt: now,
			}, nil
		},
		listMessagesFn: func(_ context.Context, threadID string) ([]session.Message, error) {
			if threadID != "thread-123" {
				t.Fatalf("ListMessages threadID = %q, want thread-123", threadID)
			}
			return []session.Message{
				{ID: 1, ThreadID: threadID, Role: "user", Content: "hi", CreatedAt: now},
				{ID: 2, ThreadID: threadID, Role: "assistant", Content: "hello", CreatedAt: now.Add(time.Second)},
			}, nil
		},
	}

	bootstrap, err := CreateOrResumeSession(context.Background(), store, SessionLifecycleParams{
		ThreadID: " thread-123 ",
		RepoRoot: "/ignored-on-resume",
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession returned error: %v", err)
	}

	if !bootstrap.Resumed {
		t.Fatal("Resumed = false, want true")
	}
	if bootstrap.Session.ThreadID != "thread-123" {
		t.Fatalf("Session.ThreadID = %q, want thread-123", bootstrap.Session.ThreadID)
	}
	if len(bootstrap.Messages) != 2 {
		t.Fatalf("Messages length = %d, want 2", len(bootstrap.Messages))
	}
	if len(bootstrap.Conversation) != 2 {
		t.Fatalf("Conversation length = %d, want 2", len(bootstrap.Conversation))
	}
	if bootstrap.Conversation[0].Role != MessageRoleUser || bootstrap.Conversation[0].Content != "hi" {
		t.Fatalf("Conversation[0] = %+v, want user hi", bootstrap.Conversation[0])
	}
	if bootstrap.Conversation[1].Role != MessageRoleAssistant || bootstrap.Conversation[1].Content != "hello" {
		t.Fatalf("Conversation[1] = %+v, want assistant hello", bootstrap.Conversation[1])
	}
	if store.createCalls != 0 {
		t.Fatalf("CreateSession called %d times, want 0", store.createCalls)
	}
}

func TestCreateOrResumeSessionRehydratesConversationInStoredOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	store := &stubSessionStore{
		resumeSessionFn: func(_ context.Context, threadID string) (session.Session, error) {
			return session.Session{
				ThreadID:  threadID,
				RepoRoot:  "/repo",
				CreatedAt: now,
			}, nil
		},
		listMessagesFn: func(_ context.Context, threadID string) ([]session.Message, error) {
			return []session.Message{
				{ID: 10, ThreadID: threadID, Role: "assistant", Content: "first assistant", CreatedAt: now.Add(3 * time.Second)},
				{ID: 11, ThreadID: threadID, Role: "tool", Content: "tool output", CreatedAt: now.Add(time.Second)},
				{ID: 12, ThreadID: threadID, Role: "user", Content: "final user", CreatedAt: now.Add(2 * time.Second)},
			}, nil
		},
	}

	bootstrap, err := CreateOrResumeSession(context.Background(), store, SessionLifecycleParams{
		ThreadID: "thread-ordered",
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession returned error: %v", err)
	}

	if len(bootstrap.Conversation) != 3 {
		t.Fatalf("Conversation length = %d, want 3", len(bootstrap.Conversation))
	}
	if bootstrap.Conversation[0].Role != MessageRoleAssistant || bootstrap.Conversation[0].Content != "first assistant" {
		t.Fatalf("Conversation[0] = %+v, want first stored assistant message", bootstrap.Conversation[0])
	}
	if bootstrap.Conversation[1].Role != MessageRoleTool || bootstrap.Conversation[1].Content != "tool output" {
		t.Fatalf("Conversation[1] = %+v, want stored tool message", bootstrap.Conversation[1])
	}
	if bootstrap.Conversation[2].Role != MessageRoleUser || bootstrap.Conversation[2].Content != "final user" {
		t.Fatalf("Conversation[2] = %+v, want final stored user message", bootstrap.Conversation[2])
	}
}

func TestCreateOrResumeSessionRequiresStore(t *testing.T) {
	t.Parallel()

	_, err := CreateOrResumeSession(context.Background(), nil, SessionLifecycleParams{})
	if err == nil {
		t.Fatal("CreateOrResumeSession returned nil error, want store requirement")
	}
}

func TestCreateOrResumeSessionPropagatesResumeErrors(t *testing.T) {
	t.Parallel()

	store := &stubSessionStore{
		resumeSessionFn: func(context.Context, string) (session.Session, error) {
			return session.Session{}, session.ErrSessionNotFound
		},
	}

	_, err := CreateOrResumeSession(context.Background(), store, SessionLifecycleParams{
		ThreadID: "missing",
	})
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
}

type stubSessionStore struct {
	createSessionFn func(context.Context, session.CreateSessionParams) (session.Session, error)
	resumeSessionFn func(context.Context, string) (session.Session, error)
	appendMessageFn func(context.Context, session.AppendMessageParams) (session.Message, error)
	listMessagesFn  func(context.Context, string) ([]session.Message, error)
	createCalls     int
	resumeCalls     int
	appendCalls     int
	listCalls       int
}

func (s *stubSessionStore) CreateSession(ctx context.Context, params session.CreateSessionParams) (session.Session, error) {
	s.createCalls++
	if s.createSessionFn == nil {
		return session.Session{}, errors.New("unexpected CreateSession call")
	}
	return s.createSessionFn(ctx, params)
}

func (s *stubSessionStore) ResumeSession(ctx context.Context, threadID string) (session.Session, error) {
	s.resumeCalls++
	if s.resumeSessionFn == nil {
		return session.Session{}, errors.New("unexpected ResumeSession call")
	}
	return s.resumeSessionFn(ctx, threadID)
}

func (s *stubSessionStore) AppendMessage(ctx context.Context, params session.AppendMessageParams) (session.Message, error) {
	s.appendCalls++
	if s.appendMessageFn == nil {
		return session.Message{}, errors.New("unexpected AppendMessage call")
	}
	return s.appendMessageFn(ctx, params)
}

func (s *stubSessionStore) ListMessages(ctx context.Context, threadID string) ([]session.Message, error) {
	s.listCalls++
	if s.listMessagesFn == nil {
		return nil, errors.New("unexpected ListMessages call")
	}
	return s.listMessagesFn(ctx, threadID)
}
