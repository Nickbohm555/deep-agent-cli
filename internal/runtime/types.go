package runtime

import "context"

type ExecutionMode string

const (
	ExecutionModeInteractive ExecutionMode = "interactive"
	ExecutionModeOneShot     ExecutionMode = "oneshot"
)

type StopReason string

const (
	StopReasonComplete   StopReason = "complete"
	StopReasonToolCalls  StopReason = "tool_calls"
	StopReasonMaxTurns   StopReason = "max_turns"
	StopReasonCancelled  StopReason = "cancelled"
	StopReasonIncomplete StopReason = "incomplete"
)

type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

type ExecutionConfig struct {
	Mode              ExecutionMode
	Model             string
	SystemPrompt      string
	MaxTurns          int
	MaxToolIterations int
	Verbose           bool
}

type TurnInput struct {
	SessionID    string
	RequestID    string
	UserMessage  string
	Conversation []Message
	Config       ExecutionConfig
}

type TurnOutput struct {
	SessionID     string
	RequestID     string
	AssistantText string
	Messages      []Message
	ToolCalls     []ToolCall
	ToolResults   []ToolResult
	StopReason    StopReason
	Usage         Usage
}

type Message struct {
	Role       MessageRole
	Content    string
	ToolCallID string
	ToolName   string
	ToolCalls  []ToolCall
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments []byte
}

type ToolResult struct {
	CallID  string
	Name    string
	Output  string
	IsError bool
}

type Usage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

type ToolDefinition struct {
	Name        string
	Description string
	Schema      map[string]any
	Handler     ToolHandler
	HandlerName string
}

type ToolHandler func(context.Context, ToolCall) (ToolResult, error)

type ProviderRequest struct {
	Input        TurnInput
	Conversation []Message
	Tools        []ToolDefinition
}

type ProviderResponse struct {
	AssistantMessage Message
	ToolCalls        []ToolCall
	StopReason       StopReason
	Usage            Usage
}

type ProviderClient interface {
	CompleteTurn(context.Context, ProviderRequest) (ProviderResponse, error)
}

type Registry interface {
	ListTools(context.Context) ([]ToolDefinition, error)
	LookupTool(context.Context, string) (ToolDefinition, bool, error)
}

type ToolDispatcher interface {
	Dispatch(context.Context, ToolCall) (ToolResult, error)
}
