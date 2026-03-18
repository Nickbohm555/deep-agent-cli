package runtime

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

type TurnRunner interface {
	RunTurn(context.Context, TurnInput) (TurnOutput, error)
}

type InteractiveDriver struct {
	Runner    TurnRunner
	Config    ExecutionConfig
	In        io.Reader
	Out       io.Writer
	SessionID string
	History   []Message
}

func (d InteractiveDriver) Run(ctx context.Context) error {
	if d.Runner == nil {
		return fmt.Errorf("interactive driver requires a turn runner")
	}

	in := d.In
	if in == nil {
		in = strings.NewReader("")
	}
	out := d.Out
	if out == nil {
		out = io.Discard
	}

	scanner := bufio.NewScanner(in)
	conversation := append([]Message(nil), d.History...)

	fmt.Fprintln(out, "Chat with deep-agent-cli")
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		fmt.Fprint(out, "You: ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return nil
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}

		result, err := d.Runner.RunTurn(ctx, TurnInput{
			SessionID:    d.SessionID,
			UserMessage:  userInput,
			Conversation: conversation,
			Config:       d.Config,
		})
		if err != nil {
			return err
		}

		conversation = result.Messages
		fmt.Fprintf(out, "Assistant: %s\n", result.AssistantText)
	}
}
