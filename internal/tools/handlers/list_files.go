package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/session"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/safety"
)

type ListFilesInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Relative path to list from. Use an empty string for the current directory."`
}

func ListFiles(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
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

	safetyCtx, err := toolSafetyContextFromRuntime(ctx)
	if err != nil {
		result.IsError = true
		return result, err
	}

	if err := ensureActionAllowed(safetyCtx, safety.ActionListFiles); err != nil {
		result.IsError = true
		return result, err
	}

	localPath, err := resolveScopedPath(safetyCtx.SessionRepoRoot, dir)
	if err != nil {
		result.IsError = true
		return result, err
	}

	repoRoot, err := session.CanonicalizeRepoRoot(safetyCtx.SessionRepoRoot)
	if err != nil {
		result.IsError = true
		return result, err
	}

	resolvedPath, err := session.ResolvePathWithinRepo(repoRoot, localPath)
	if err != nil {
		result.IsError = true
		return result, err
	}

	files := make([]string, 0)
	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			result.IsError = true
			return result, fmt.Errorf("exit status 1")
		}

		result.IsError = true
		return result, err
	}
	if !info.IsDir() {
		files = append(files, displayListPath(repoRoot, resolvedPath, localPath))
		payload, err := json.Marshal(files)
		if err != nil {
			result.IsError = true
			return result, err
		}

		result.Output = string(payload)
		return result, nil
	}

	err = filepath.Walk(resolvedPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}

		if info.IsDir() && shouldSkipToolDir(info.Name(), relPath) {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}

		files = append(files, displayListPath(repoRoot, path, localPath))
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

func shouldSkipToolDir(name, relPath string) bool {
	return name == ".devenv" ||
		name == ".git" ||
		relPath == ".devenv" ||
		relPath == ".git" ||
		strings.HasPrefix(relPath, ".devenv"+string(filepath.Separator)) ||
		strings.HasPrefix(relPath, ".git"+string(filepath.Separator))
}

func displayListPath(repoRoot, resolvedPath, requestedPath string) string {
	if requestedPath != "." {
		requestedRoot, err := session.ResolvePathWithinRepo(repoRoot, requestedPath)
		if err == nil {
			relPath, err := filepath.Rel(requestedRoot, resolvedPath)
			if err == nil {
				return filepath.ToSlash(relPath)
			}
		}
	}

	relPath, err := filepath.Rel(repoRoot, resolvedPath)
	if err != nil {
		return requestedPath
	}

	relPath = filepath.ToSlash(relPath)
	if requestedPath == "." {
		return "./" + relPath
	}

	return relPath
}
