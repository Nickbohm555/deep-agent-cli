package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

// PersistentTurnRunner fails the turn if persistence does not succeed.
// This keeps the visible conversation aligned with durable session history:
// accepted user input is stored before execution, and a generated assistant
// reply is only surfaced after it has also been written successfully.
type PersistentTurnRunner struct {
	Store  session.SessionStore
	Runner TurnRunner
}

func NewPersistentTurnRunner(store session.SessionStore, runner TurnRunner) *PersistentTurnRunner {
	return &PersistentTurnRunner{
		Store:  store,
		Runner: runner,
	}
}

func (r *PersistentTurnRunner) RunTurn(ctx context.Context, input TurnInput) (TurnOutput, error) {
	if r.Runner == nil {
		return TurnOutput{}, fmt.Errorf("persistent turn runner requires a wrapped runner")
	}
	if r.Store == nil {
		return TurnOutput{}, fmt.Errorf("persistent turn runner requires a session store")
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return TurnOutput{}, fmt.Errorf("persistent turn runner requires a session ID")
	}

	if err := PersistTurn(ctx, r.Store, input.SessionID, Message{
		Role:    MessageRoleUser,
		Content: input.UserMessage,
	}); err != nil {
		return TurnOutput{}, err
	}

	result, err := r.Runner.RunTurn(ctx, input)
	if err != nil {
		return TurnOutput{}, err
	}

	assistantMessage, err := finalAssistantMessage(result)
	if err != nil {
		return TurnOutput{}, err
	}

	if err := PersistTurn(ctx, r.Store, input.SessionID, assistantMessage); err != nil {
		return TurnOutput{}, err
	}

	return result, nil
}

func PersistTurn(ctx context.Context, store session.SessionStore, threadID string, message Message) error {
	if store == nil {
		return fmt.Errorf("persist turn: session store is required")
	}

	role := strings.TrimSpace(string(message.Role))
	if role == "" {
		return fmt.Errorf("persist turn: message role is required")
	}

	if _, err := store.AppendMessage(ctx, session.AppendMessageParams{
		ThreadID: threadID,
		Role:     role,
		Content:  message.Content,
	}); err != nil {
		return fmt.Errorf("persist turn: append %s message: %w", role, err)
	}

	return nil
}

func finalAssistantMessage(result TurnOutput) (Message, error) {
	for i := len(result.Messages) - 1; i >= 0; i-- {
		if result.Messages[i].Role == MessageRoleAssistant {
			return result.Messages[i], nil
		}
	}

	if strings.TrimSpace(result.AssistantText) != "" {
		return Message{
			Role:    MessageRoleAssistant,
			Content: result.AssistantText,
		}, nil
	}

	return Message{}, fmt.Errorf("persist turn: assistant response missing from turn output")
}
