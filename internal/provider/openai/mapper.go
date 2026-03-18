package openai

import (
	"encoding/json"
	"fmt"

	openaiapi "github.com/openai/openai-go/v3"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

func mapMessages(messages []runtime.Message) ([]openaiapi.ChatCompletionMessageParamUnion, error) {
	params := make([]openaiapi.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, message := range messages {
		param, err := mapMessage(message)
		if err != nil {
			return nil, err
		}
		params = append(params, param)
	}

	return params, nil
}

func mapMessage(message runtime.Message) (openaiapi.ChatCompletionMessageParamUnion, error) {
	switch message.Role {
	case runtime.MessageRoleSystem:
		return openaiapi.SystemMessage(message.Content), nil
	case runtime.MessageRoleUser:
		return openaiapi.UserMessage(message.Content), nil
	case runtime.MessageRoleTool:
		if message.ToolCallID == "" {
			return openaiapi.ChatCompletionMessageParamUnion{}, fmt.Errorf("tool message missing ToolCallID")
		}
		return openaiapi.ToolMessage(message.Content, message.ToolCallID), nil
	case runtime.MessageRoleAssistant:
		return mapAssistantMessage(message), nil
	default:
		return openaiapi.ChatCompletionMessageParamUnion{}, fmt.Errorf("unsupported message role %q", message.Role)
	}
}

func mapAssistantMessage(message runtime.Message) openaiapi.ChatCompletionMessageParamUnion {
	if len(message.ToolCalls) == 0 {
		return openaiapi.AssistantMessage(message.Content)
	}

	param := openaiapi.ChatCompletionAssistantMessageParam{}
	if message.Content != "" {
		param.Content.OfString = openaiapi.String(message.Content)
	}
	for _, call := range message.ToolCalls {
		param.ToolCalls = append(param.ToolCalls, openaiapi.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openaiapi.ChatCompletionMessageFunctionToolCallParam{
				ID: call.ID,
				Function: openaiapi.ChatCompletionMessageFunctionToolCallFunctionParam{
					Arguments: string(call.Arguments),
					Name:      call.Name,
				},
			},
		})
	}

	return openaiapi.ChatCompletionMessageParamUnion{OfAssistant: &param}
}

func mapTools(tools []runtime.ToolDefinition) []openaiapi.ChatCompletionToolUnionParam {
	params := make([]openaiapi.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		params = append(params, openaiapi.ChatCompletionFunctionTool(openaiapi.FunctionDefinitionParam{
			Name:        tool.Name,
			Description: openaiapi.Opt(tool.Description),
			Parameters:  tool.Schema,
			Strict:      openaiapi.Opt(true),
		}))
	}

	return params
}

func mapResponse(message openaiapi.ChatCompletionMessage, usage openaiapi.CompletionUsage) runtime.ProviderResponse {
	assistant := runtime.Message{
		Role:    runtime.MessageRoleAssistant,
		Content: message.Content,
	}

	toolCalls := make([]runtime.ToolCall, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		if call.Type != "function" {
			continue
		}

		functionCall := call.AsFunction()
		toolCall := runtime.ToolCall{
			ID:        functionCall.ID,
			Name:      functionCall.Function.Name,
			Arguments: json.RawMessage(functionCall.Function.Arguments),
		}
		toolCalls = append(toolCalls, toolCall)
	}
	assistant.ToolCalls = append(assistant.ToolCalls, toolCalls...)

	stopReason := runtime.StopReasonComplete
	if len(toolCalls) > 0 {
		stopReason = runtime.StopReasonToolCalls
	}

	return runtime.ProviderResponse{
		AssistantMessage: assistant,
		ToolCalls:        toolCalls,
		StopReason:       stopReason,
		Usage: runtime.Usage{
			InputTokens:  usage.PromptTokens,
			OutputTokens: usage.CompletionTokens,
			TotalTokens:  usage.TotalTokens,
		},
	}
}
