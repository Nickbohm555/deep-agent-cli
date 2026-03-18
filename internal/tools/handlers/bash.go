package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

type BashInput struct {
	Command string `json:"command" jsonschema_description:"The bash command to execute."`
}

func Bash(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
	result := runtime.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	var input BashInput
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		result.IsError = true
		return result, err
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", input.Command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Output = fmt.Sprintf("Command failed with error: %s\nOutput: %s", err.Error(), string(output))
		return result, nil
	}

	result.Output = strings.TrimSpace(string(output))
	return result, nil
}
