package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type stubProvider struct {
	responses []ProviderResponse
	err       error
	requests  []ProviderRequest
}

func (s *stubProvider) CompleteTurn(_ context.Context, request ProviderRequest) (ProviderResponse, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return ProviderResponse{}, s.err
	}
	if len(s.responses) == 0 {
		return ProviderResponse{}, fmt.Errorf("no provider response queued")
	}

	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

type stubDispatcher struct {
	results []ToolResult
	err     error
	calls   []ToolCall
}

func (s *stubDispatcher) Dispatch(_ context.Context, call ToolCall) (ToolResult, error) {
	s.calls = append(s.calls, call)
	if s.err != nil {
		return ToolResult{}, s.err
	}
	if len(s.results) == 0 {
		return ToolResult{}, fmt.Errorf("no dispatcher result queued")
	}

	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

func TestOrchestratorToolLoopPreservesCallIDCorrelation(t *testing.T) {
	provider := &stubProvider{
		responses: []ProviderResponse{
			{
				AssistantMessage: Message{
					Role:    MessageRoleAssistant,
					Content: "Checking the file.",
				},
				ToolCalls: []ToolCall{
					{ID: "call-123", Name: "read_file", Arguments: []byte(`{"path":"README.md"}`)},
				},
				StopReason: StopReasonToolCalls,
			},
			{
				AssistantMessage: Message{
					Role:    MessageRoleAssistant,
					Content: "Done.",
				},
				StopReason: StopReasonComplete,
			},
		},
	}
	dispatcher := &stubDispatcher{
		results: []ToolResult{
			{
				Output: "file contents",
			},
		},
	}

	orchestrator := NewOrchestrator(provider, nil, dispatcher)
	output, err := orchestrator.RunTurn(context.Background(), TurnInput{
		UserMessage: "read the readme",
		Config:      TurnConfigForTest(4),
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	if output.StopReason != StopReasonComplete {
		t.Fatalf("StopReason = %q, want %q", output.StopReason, StopReasonComplete)
	}
	if len(output.ToolCalls) != 1 {
		t.Fatalf("ToolCalls length = %d, want 1", len(output.ToolCalls))
	}
	if len(output.ToolResults) != 1 {
		t.Fatalf("ToolResults length = %d, want 1", len(output.ToolResults))
	}
	if output.ToolResults[0].CallID != "call-123" {
		t.Fatalf("ToolResults[0].CallID = %q, want call-123", output.ToolResults[0].CallID)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].ID != "call-123" {
		t.Fatalf("dispatcher call ID mismatch: %+v", dispatcher.calls)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(provider.requests))
	}
	secondConversation := provider.requests[1].Conversation
	if len(secondConversation) < 3 {
		t.Fatalf("second conversation length = %d, want at least 3", len(secondConversation))
	}
	assistant := secondConversation[len(secondConversation)-2]
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call-123" {
		t.Fatalf("assistant ToolCalls = %+v, want correlated call ID", assistant.ToolCalls)
	}
	toolResult := secondConversation[len(secondConversation)-1]
	if toolResult.Role != MessageRoleTool {
		t.Fatalf("tool result role = %q, want %q", toolResult.Role, MessageRoleTool)
	}
	if toolResult.ToolCallID != "call-123" {
		t.Fatalf("tool result ToolCallID = %q, want call-123", toolResult.ToolCallID)
	}
}

func TestOrchestratorStopsAtMaxToolIterations(t *testing.T) {
	provider := &stubProvider{
		responses: []ProviderResponse{
			{
				AssistantMessage: Message{
					Role:    MessageRoleAssistant,
					Content: "Still working.",
				},
				ToolCalls: []ToolCall{
					{ID: "call-1", Name: "bash", Arguments: []byte(`{"command":"pwd"}`)},
				},
				StopReason: StopReasonToolCalls,
			},
		},
	}

	orchestrator := NewOrchestrator(provider, nil, &stubDispatcher{})
	output, err := orchestrator.RunTurn(context.Background(), TurnInput{
		UserMessage: "loop once",
		Config:      TurnConfigForTest(1),
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	if output.StopReason != StopReasonMaxTurns {
		t.Fatalf("StopReason = %q, want %q", output.StopReason, StopReasonMaxTurns)
	}
}

func TestOrchestratorReturnsCancelledContextBeforeProviderCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	provider := &stubProvider{
		err: errors.New("should not be called"),
	}
	orchestrator := NewOrchestrator(provider, nil, nil)

	_, err := orchestrator.RunTurn(ctx, TurnInput{
		UserMessage: "hello",
		Config:      TurnConfigForTest(2),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunTurn error = %v, want context.Canceled", err)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider was called %d times, want 0", len(provider.requests))
	}
}

func TurnConfigForTest(maxToolIterations int) ExecutionConfig {
	return ExecutionConfig{
		MaxToolIterations: maxToolIterations,
	}
}
