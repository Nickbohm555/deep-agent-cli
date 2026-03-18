package runtime

import (
	"context"
	"fmt"
	"strings"
)

type Orchestrator struct {
	Provider   ProviderClient
	Registry   Registry
	Dispatcher ToolDispatcher
}

func NewOrchestrator(provider ProviderClient, registry Registry, dispatcher ToolDispatcher) *Orchestrator {
	return &Orchestrator{
		Provider:   provider,
		Registry:   registry,
		Dispatcher: dispatcher,
	}
}

func (o *Orchestrator) RunTurn(ctx context.Context, input TurnInput) (TurnOutput, error) {
	if err := ctx.Err(); err != nil {
		return TurnOutput{
			SessionID:  input.SessionID,
			RequestID:  input.RequestID,
			Messages:   appendConversation(input.Conversation, input.Config.SystemPrompt, input.UserMessage),
			StopReason: StopReasonCancelled,
		}, err
	}

	conversation := appendConversation(input.Conversation, input.Config.SystemPrompt, input.UserMessage)
	response, err := o.completeTurn(ctx, input, conversation)
	if err != nil {
		return TurnOutput{}, err
	}

	messages := append(conversation, response.AssistantMessage)
	return TurnOutput{
		SessionID:     input.SessionID,
		RequestID:     input.RequestID,
		AssistantText: response.AssistantMessage.Content,
		Messages:      messages,
		ToolCalls:     response.ToolCalls,
		StopReason:    response.StopReason,
		Usage:         response.Usage,
	}, nil
}

func (o *Orchestrator) completeTurn(ctx context.Context, input TurnInput, conversation []Message) (ProviderResponse, error) {
	if o.Provider == nil {
		return ProviderResponse{
			AssistantMessage: Message{
				Role:    MessageRoleAssistant,
				Content: fallbackAssistantMessage(input.UserMessage),
			},
			StopReason: StopReasonComplete,
		}, nil
	}

	var tools []ToolDefinition
	if o.Registry != nil {
		listed, err := o.Registry.ListTools(ctx)
		if err != nil {
			return ProviderResponse{}, fmt.Errorf("list tools: %w", err)
		}
		tools = listed
	}

	response, err := o.Provider.CompleteTurn(ctx, ProviderRequest{
		Input:        input,
		Conversation: conversation,
		Tools:        tools,
	})
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("complete turn: %w", err)
	}

	if response.AssistantMessage.Role == "" {
		response.AssistantMessage.Role = MessageRoleAssistant
	}
	if response.StopReason == "" {
		response.StopReason = StopReasonComplete
	}

	return response, nil
}

func appendConversation(existing []Message, systemPrompt, userMessage string) []Message {
	conversation := make([]Message, 0, len(existing)+2)
	conversation = append(conversation, existing...)

	if systemPrompt != "" && !hasSystemMessage(conversation) {
		conversation = append(conversation, Message{
			Role:    MessageRoleSystem,
			Content: systemPrompt,
		})
	}

	conversation = append(conversation, Message{
		Role:    MessageRoleUser,
		Content: userMessage,
	})
	return conversation
}

func hasSystemMessage(messages []Message) bool {
	for _, message := range messages {
		if message.Role == MessageRoleSystem {
			return true
		}
	}

	return false
}

func fallbackAssistantMessage(userMessage string) string {
	trimmed := strings.TrimSpace(userMessage)
	if trimmed == "" {
		return "No input provided."
	}

	return "Echo: " + trimmed
}
