package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/safety"
)

type BashInput struct {
	Command    string `json:"command" jsonschema_description:"The bash command to execute."`
	WorkingDir string `json:"working_dir,omitempty" jsonschema_description:"Optional relative working directory inside the bound repository. Use an empty string to run from the repository root."`
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

	safetyCtx, err := toolSafetyContextFromRuntime(ctx)
	if err != nil {
		result.IsError = true
		return result, err
	}

	if strings.TrimSpace(input.Command) == "" {
		err := fmt.Errorf("command is required")
		result.IsError = true
		return result, err
	}

	if _, err := resolveScopedPath(safetyCtx.SessionRepoRoot, defaultScopedPath(input.WorkingDir)); err != nil {
		result.IsError = true
		return result, err
	}

	cmd, cancel, decision, err := safety.PrepareScopedCommand(ctx, safetyCtx, safety.ActionBashExecute, input.Command, input.WorkingDir)
	if err != nil {
		result.IsError = true
		return result, err
	}
	if cancel != nil {
		defer cancel()
	}

	switch decision {
	case safety.DecisionDryRun:
		result.Output = fmt.Sprintf("Command not executed in mode %q", safetyCtx.EffectiveMode())
		return result, nil
	case safety.DecisionRequireApproval:
		result.Output = fmt.Sprintf("Command requires approval in mode %q", safetyCtx.EffectiveMode())
		return result, nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Output = fmt.Sprintf("Command failed with error: %s\nOutput: %s", err.Error(), string(output))
		return result, nil
	}

	result.Output = strings.TrimSpace(string(output))
	return result, nil
}

func defaultScopedPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "."
	}

	return path
}
