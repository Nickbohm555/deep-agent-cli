package runtime

import (
	"context"
	"strings"
	"testing"
)

type capturingTurnRunner struct {
	inputs []TurnInput
	output TurnOutput
	err    error
}

func (r *capturingTurnRunner) RunTurn(_ context.Context, input TurnInput) (TurnOutput, error) {
	r.inputs = append(r.inputs, input)
	if r.err != nil {
		return TurnOutput{}, r.err
	}
	return r.output, nil
}

func TestInteractiveDriverSeedsHistoryIntoConversation(t *testing.T) {
	t.Parallel()

	runner := &capturingTurnRunner{
		output: TurnOutput{
			AssistantText: "reply",
			Messages: []Message{
				{Role: MessageRoleUser, Content: "earlier"},
				{Role: MessageRoleAssistant, Content: "response"},
			},
		},
	}

	driver := InteractiveDriver{
		Runner: runner,
		In:     strings.NewReader("next\n"),
		Out:    &strings.Builder{},
		History: []Message{
			{Role: MessageRoleUser, Content: "earlier"},
			{Role: MessageRoleAssistant, Content: "response"},
		},
	}

	if err := driver.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(runner.inputs) != 1 {
		t.Fatalf("RunTurn calls = %d, want 1", len(runner.inputs))
	}
	if len(runner.inputs[0].Conversation) != 2 {
		t.Fatalf("Conversation length = %d, want 2", len(runner.inputs[0].Conversation))
	}
	if runner.inputs[0].Conversation[0].Content != "earlier" || runner.inputs[0].Conversation[1].Content != "response" {
		t.Fatalf("Conversation = %+v, want seeded history", runner.inputs[0].Conversation)
	}
}
