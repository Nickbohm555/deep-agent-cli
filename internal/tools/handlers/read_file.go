package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
}

func ReadFile(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
	_ = ctx

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

	fileInfo, err := os.Stat(input.Path)
	if err != nil {
		result.IsError = true
		return result, err
	}
	if fileInfo.IsDir() {
		err := fmt.Errorf("path points to a directory, not a file")
		result.IsError = true
		return result, err
	}

	content, err := os.ReadFile(input.Path)
	if err != nil {
		result.IsError = true
		return result, err
	}

	result.Output = string(content)
	return result, nil
}
