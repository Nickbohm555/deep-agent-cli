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
	if input.SessionID != "" {
		ctx = WithSessionID(ctx, input.SessionID)
	}

	if err := ctx.Err(); err != nil {
		return TurnOutput{
			SessionID:  input.SessionID,
			RequestID:  input.RequestID,
			Messages:   appendConversation(input.Conversation, input.Config.SystemPrompt, input.UserMessage),
			StopReason: StopReasonCancelled,
		}, err
	}

	conversation := appendConversation(input.Conversation, input.Config.SystemPrompt, input.UserMessage)
	if o.Provider == nil {
		return fallbackTurnOutput(input, conversation), nil
	}

	tools, err := o.listTools(ctx)
	if err != nil {
		return TurnOutput{}, err
	}

	maxIterations := input.Config.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = 1
	}

	output := TurnOutput{
		SessionID: input.SessionID,
		RequestID: input.RequestID,
	}

	for iteration := 0; ; iteration++ {
		if err := ctx.Err(); err != nil {
			output.Messages = conversation
			output.StopReason = StopReasonCancelled
			return output, err
		}

		response, err := o.completeTurn(ctx, input, conversation, tools)
		if err != nil {
			return TurnOutput{}, err
		}

		conversation = append(conversation, response.AssistantMessage)
		output.AssistantText = response.AssistantMessage.Content
		output.Messages = conversation
		output.ToolCalls = append(output.ToolCalls, response.ToolCalls...)
		output.StopReason = response.StopReason
		output.Usage = response.Usage

		if len(response.ToolCalls) == 0 {
			return output, nil
		}
		if iteration+1 >= maxIterations {
			output.StopReason = StopReasonMaxTurns
			return output, nil
		}

		results, err := o.dispatchToolCalls(ctx, response.ToolCalls)
		if err != nil {
			return TurnOutput{}, err
		}

		output.ToolResults = append(output.ToolResults, results...)
		for _, result := range results {
			toolMessage := Message{
				Role:     MessageRoleTool,
				Content:  toolResultContent(result),
				ToolName: result.Name,
			}
			toolMessage.ToolCallID = result.CallID
			conversation = append(conversation, toolMessage)
		}
		output.Messages = conversation
	}
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

func (o *Orchestrator) completeTurn(ctx context.Context, input TurnInput, conversation []Message, tools []ToolDefinition) (ProviderResponse, error) {
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
	if response.AssistantMessage.ToolCalls == nil && len(response.ToolCalls) > 0 {
		response.AssistantMessage.ToolCalls = append([]ToolCall(nil), response.ToolCalls...)
	}
	if response.StopReason == "" {
		if len(response.ToolCalls) > 0 {
			response.StopReason = StopReasonToolCalls
		} else {
			response.StopReason = StopReasonComplete
		}
	}

	return response, nil
}

func (o *Orchestrator) listTools(ctx context.Context) ([]ToolDefinition, error) {
	if o.Registry == nil {
		return nil, nil
	}

	tools, err := o.Registry.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	return tools, nil
}

func (o *Orchestrator) dispatchToolCalls(ctx context.Context, calls []ToolCall) ([]ToolResult, error) {
	if o.Dispatcher == nil {
		return nil, fmt.Errorf("dispatch tool calls: no tool dispatcher configured")
	}

	results := make([]ToolResult, 0, len(calls))
	for _, call := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		result, err := o.Dispatcher.Dispatch(ctx, call)
		if err != nil {
			result = normalizeToolResult(result, call, err.Error(), true)
		} else {
			result = normalizeToolResult(result, call, result.Output, result.IsError)
		}
		results = append(results, result)
	}

	return results, nil
}

func normalizeToolResult(result ToolResult, call ToolCall, fallbackOutput string, isError bool) ToolResult {
	if result.CallID == "" {
		result.CallID = call.ID
	}
	if result.Name == "" {
		result.Name = call.Name
	}
	if result.Output == "" {
		result.Output = fallbackOutput
	}
	result.IsError = isError
	return result
}

func toolResultContent(result ToolResult) string {
	if result.Output != "" {
		return result.Output
	}
	if result.IsError {
		return "tool execution failed"
	}
	return ""
}

func fallbackTurnOutput(input TurnInput, conversation []Message) TurnOutput {
	assistant := Message{
		Role:    MessageRoleAssistant,
		Content: fallbackAssistantMessage(input.UserMessage),
	}
	messages := append(conversation, assistant)
	return TurnOutput{
		SessionID:     input.SessionID,
		RequestID:     input.RequestID,
		AssistantText: assistant.Content,
		Messages:      messages,
		StopReason:    StopReasonComplete,
	}
}
