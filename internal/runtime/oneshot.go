package runtime

import (
	"context"
	"fmt"
	"io"
	"strings"
)

type OneShotDriver struct {
	Runner    TurnRunner
	Config    ExecutionConfig
	Out       io.Writer
	SessionID string
}

func (d OneShotDriver) Run(ctx context.Context, prompt string) (TurnOutput, error) {
	if d.Runner == nil {
		return TurnOutput{}, fmt.Errorf("oneshot driver requires a turn runner")
	}

	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return TurnOutput{}, fmt.Errorf("prompt is required in oneshot mode")
	}

	result, err := d.Runner.RunTurn(ctx, TurnInput{
		SessionID:   d.SessionID,
		UserMessage: trimmed,
		Config:      d.Config,
	})
	if err != nil {
		return TurnOutput{}, err
	}

	if d.Out != nil {
		fmt.Fprintln(d.Out, result.AssistantText)
	}

	return result, nil
}
