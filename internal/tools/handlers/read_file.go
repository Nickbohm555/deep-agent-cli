package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/safety"
)

type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
}

func ReadFile(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
	result := runtime.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	var input ReadFileInput
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		result.IsError = true
		return result, err
	}

	if input.Path == "" {
		err := fmt.Errorf("path is required")
		result.IsError = true
		return result, err
	}

	safetyCtx, err := toolSafetyContextFromRuntime(ctx)
	if err != nil {
		result.IsError = true
		return result, err
	}

	localPath, err := resolveScopedPath(safetyCtx.SessionRepoRoot, input.Path)
	if err != nil {
		result.IsError = true
		return result, err
	}

	if err := ensureActionAllowed(safetyCtx, safety.ActionReadFile); err != nil {
		result.IsError = true
		return result, err
	}

	file, err := safety.OpenInRepoRoot(safetyCtx, localPath)
	if err != nil {
		if os.IsNotExist(err) {
			result.IsError = true
			return result, fmt.Errorf("open %s: no such file or directory", localPath)
		}

		result.IsError = true
		return result, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		result.IsError = true
		return result, err
	}
	if fileInfo.IsDir() {
		err := fmt.Errorf("read %s: is a directory", localPath)
		result.IsError = true
		return result, err
	}

	content, err := io.ReadAll(file)
	if err != nil {
		result.IsError = true
		return result, err
	}

	result.Output = string(content)
	return result, nil
}

func toolSafetyContextFromRuntime(ctx context.Context) (safety.ToolSafetyContext, error) {
	repoRoot, err := runtime.RepoRootFromContext(ctx)
	if err != nil {
		return safety.ToolSafetyContext{}, err
	}

	return safety.ToolSafetyContext{SessionRepoRoot: repoRoot}, nil
}

func ensureActionAllowed(safetyCtx safety.ToolSafetyContext, action safety.ToolAction) error {
	decision := safety.EvaluateAction(safetyCtx.EffectiveMode(), action)
	if decision == safety.DecisionAllow {
		return nil
	}

	return fmt.Errorf("tool execution denied for action %q in mode %q", action, safetyCtx.EffectiveMode())
}

func resolveScopedPath(repoRoot, path string) (string, error) {
	localPath, err := safety.ValidateLocalPath(path)
	if err == nil {
		return localPath, nil
	}

	if strings.Contains(err.Error(), "must stay within the session repo") {
		return "", fmt.Errorf("path %q escapes repository scope %q", strings.TrimSpace(path), repoRoot)
	}

	return "", err
}
