package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/sandbox"
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

	repoRoot, err := runtime.RepoRootFromContext(ctx)
	if err != nil {
		result.IsError = true
		return result, err
	}

	resolution, err := sandbox.EnforceRepoScope(repoRoot, sandbox.ScopeTarget{
		ToolName:   call.Name,
		Operation:  "execute",
		WorkingDir: input.WorkingDir,
	})
	if err != nil {
		result.IsError = true
		return result, err
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", input.Command)
	cmd.Dir = resolution.WorkingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Output = fmt.Sprintf("Command failed with error: %s\nOutput: %s", err.Error(), string(output))
		return result, nil
	}

	result.Output = strings.TrimSpace(string(output))
	return result, nil
}
