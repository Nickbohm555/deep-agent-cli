package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

func TestPersistentTurnRunnerPersistsUserAndAssistantMessages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	store := newMemorySessionStore(now)
	created, err := store.CreateSession(context.Background(), session.CreateSessionParams{
		ThreadID: "thread-123",
		RepoRoot: "/repo",
	})
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	runner := NewPersistentTurnRunner(store, stubTurnRunner{
		output: TurnOutput{
			SessionID:     created.ThreadID,
			AssistantText: "hello back",
			Messages: []Message{
				{Role: MessageRoleUser, Content: "hello"},
				{Role: MessageRoleAssistant, Content: "hello back"},
			},
		},
	})

	_, err = runner.RunTurn(context.Background(), TurnInput{
		SessionID:   created.ThreadID,
		UserMessage: "hello",
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	messages, err := store.ListMessages(context.Background(), created.ThreadID)
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hello" {
		t.Fatalf("messages[0] = %+v, want persisted user turn", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "hello back" {
		t.Fatalf("messages[1] = %+v, want persisted assistant turn", messages[1])
	}
}

func TestPersistentTurnRunnerHistorySurvivesResumeAcrossProcessRestart(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	store := newMemorySessionStore(now)
	created, err := store.CreateSession(context.Background(), session.CreateSessionParams{
		ThreadID: "thread-restart",
		RepoRoot: "/repo",
	})
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	runner := NewPersistentTurnRunner(store, stubTurnRunner{
		output: TurnOutput{
			SessionID:     created.ThreadID,
			AssistantText: "second",
			Messages: []Message{
				{Role: MessageRoleUser, Content: "first"},
				{Role: MessageRoleAssistant, Content: "second"},
			},
		},
	})

	if _, err := runner.RunTurn(context.Background(), TurnInput{
		SessionID:   created.ThreadID,
		UserMessage: "first",
	}); err != nil {
		t.Fatalf("first RunTurn returned error: %v", err)
	}

	runner.Runner = stubTurnRunner{
		output: TurnOutput{
			SessionID:     created.ThreadID,
			AssistantText: "fourth",
			Messages: []Message{
				{Role: MessageRoleUser, Content: "first"},
				{Role: MessageRoleAssistant, Content: "second"},
				{Role: MessageRoleUser, Content: "third"},
				{Role: MessageRoleAssistant, Content: "fourth"},
			},
		},
	}

	if _, err := runner.RunTurn(context.Background(), TurnInput{
		SessionID:   created.ThreadID,
		UserMessage: "third",
		Conversation: []Message{
			{Role: MessageRoleUser, Content: "first"},
			{Role: MessageRoleAssistant, Content: "second"},
		},
	}); err != nil {
		t.Fatalf("second RunTurn returned error: %v", err)
	}

	bootstrap, err := CreateOrResumeSession(context.Background(), store, SessionLifecycleParams{
		ThreadID: created.ThreadID,
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession returned error: %v", err)
	}

	if len(bootstrap.Messages) != 4 {
		t.Fatalf("message count = %d, want 4", len(bootstrap.Messages))
	}
	want := []struct {
		role    MessageRole
		content string
	}{
		{role: MessageRoleUser, content: "first"},
		{role: MessageRoleAssistant, content: "second"},
		{role: MessageRoleUser, content: "third"},
		{role: MessageRoleAssistant, content: "fourth"},
	}
	for i, item := range want {
		if bootstrap.Conversation[i].Role != item.role || bootstrap.Conversation[i].Content != item.content {
			t.Fatalf("Conversation[%d] = %+v, want %s %q", i, bootstrap.Conversation[i], item.role, item.content)
		}
	}
}

func TestPersistentTurnRunnerFailsIfAssistantPersistenceFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	store := newMemorySessionStore(now)
	if _, err := store.CreateSession(context.Background(), session.CreateSessionParams{
		ThreadID: "thread-assistant-error",
		RepoRoot: "/repo",
	}); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	store.failAppendForRole = "assistant"

	runner := NewPersistentTurnRunner(store, stubTurnRunner{
		output: TurnOutput{
			SessionID:     "thread-assistant-error",
			AssistantText: "reply",
			Messages: []Message{
				{Role: MessageRoleUser, Content: "hello"},
				{Role: MessageRoleAssistant, Content: "reply"},
			},
		},
	})

	_, err := runner.RunTurn(context.Background(), TurnInput{
		SessionID:   "thread-assistant-error",
		UserMessage: "hello",
	})
	if err == nil {
		t.Fatal("RunTurn returned nil error, want assistant persistence failure")
	}

	messages, listErr := store.ListMessages(context.Background(), "thread-assistant-error")
	if listErr != nil {
		t.Fatalf("ListMessages returned error: %v", listErr)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want only persisted user turn", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hello" {
		t.Fatalf("messages[0] = %+v, want persisted user turn only", messages[0])
	}
}

type stubTurnRunner struct {
	output TurnOutput
	err    error
}

func (r stubTurnRunner) RunTurn(_ context.Context, _ TurnInput) (TurnOutput, error) {
	if r.err != nil {
		return TurnOutput{}, r.err
	}
	return r.output, nil
}

type memorySessionStore struct {
	sessions          map[string]session.Session
	messages          map[string][]session.Message
	nextMessageID     int64
	now               time.Time
	failAppendForRole string
}

func newMemorySessionStore(now time.Time) *memorySessionStore {
	return &memorySessionStore{
		sessions: make(map[string]session.Session),
		messages: make(map[string][]session.Message),
		now:      now,
	}
}

func (s *memorySessionStore) CreateSession(_ context.Context, params session.CreateSessionParams) (session.Session, error) {
	created := session.Session{
		ThreadID:  params.ThreadID,
		RepoRoot:  params.RepoRoot,
		CreatedAt: s.now,
	}
	s.sessions[created.ThreadID] = created
	return created, nil
}

func (s *memorySessionStore) ResumeSession(_ context.Context, threadID string) (session.Session, error) {
	found, ok := s.sessions[threadID]
	if !ok {
		return session.Session{}, session.ErrSessionNotFound
	}
	return found, nil
}

func (s *memorySessionStore) AppendMessage(_ context.Context, params session.AppendMessageParams) (session.Message, error) {
	if _, ok := s.sessions[params.ThreadID]; !ok {
		return session.Message{}, session.ErrSessionNotFound
	}
	if params.Role == s.failAppendForRole {
		return session.Message{}, errors.New("forced append failure")
	}

	s.nextMessageID++
	message := session.Message{
		ID:        s.nextMessageID,
		ThreadID:  params.ThreadID,
		Role:      params.Role,
		Content:   params.Content,
		CreatedAt: s.now.Add(time.Duration(s.nextMessageID) * time.Second),
	}
	s.messages[params.ThreadID] = append(s.messages[params.ThreadID], message)
	return message, nil
}

func (s *memorySessionStore) ListMessages(_ context.Context, threadID string) ([]session.Message, error) {
	if _, ok := s.sessions[threadID]; !ok {
		return nil, session.ErrSessionNotFound
	}
	return append([]session.Message(nil), s.messages[threadID]...), nil
}
