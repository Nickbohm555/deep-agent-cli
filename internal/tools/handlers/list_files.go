package handlers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

type ListFilesInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Relative path to list from. Use an empty string for the current directory."`
}

func ListFiles(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
	_ = ctx

	result := runtime.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	var input ListFilesInput
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		result.IsError = true
		return result, err
	}

	dir := "."
	if input.Path != "" {
		dir = input.Path
	}

	files := make([]string, 0)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		if info.IsDir() && (info.Name() == ".devenv" || relPath == ".devenv" || strings.HasPrefix(relPath, ".devenv"+string(filepath.Separator))) {
			return filepath.SkipDir
		}

		if relPath == "." {
			return nil
		}
		if info.IsDir() {
			files = append(files, relPath+"/")
			return nil
		}

		files = append(files, relPath)
		return nil
	})
	if err != nil {
		result.IsError = true
		return result, err
	}

	payload, err := json.Marshal(files)
	if err != nil {
		result.IsError = true
		return result, err
	}

	result.Output = string(payload)
	return result, nil
}
