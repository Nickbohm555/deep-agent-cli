package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/sandbox"
)

type CodeSearchInput struct {
	Pattern       string `json:"pattern" jsonschema_description:"The search pattern or regex to look for."`
	Path          string `json:"path,omitempty" jsonschema_description:"Path to search in. Use an empty string to search the current directory."`
	FileType      string `json:"file_type,omitempty" jsonschema_description:"Optional file extension or ripgrep type filter. Use an empty string for no filter."`
	CaseSensitive bool   `json:"case_sensitive,omitempty" jsonschema_description:"Whether the search should be case sensitive."`
}

func CodeSearch(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
	result := runtime.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	var input CodeSearchInput
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		result.IsError = true
		return result, err
	}

	if input.Pattern == "" {
		err := fmt.Errorf("pattern is required")
		result.IsError = true
		return result, err
	}

	repoRoot, err := runtime.RepoRootFromContext(ctx)
	if err != nil {
		result.IsError = true
		return result, err
	}

	searchPath := input.Path
	if strings.TrimSpace(searchPath) == "" {
		searchPath = "."
	}

	resolution, err := sandbox.EnforceRepoScope(repoRoot, sandbox.ScopeTarget{
		ToolName:  call.Name,
		Operation: "search",
		Path:      searchPath,
	})
	if err != nil {
		result.IsError = true
		return result, err
	}

	args := []string{"rg", "--line-number", "--with-filename", "--color=never"}
	if !input.CaseSensitive {
		args = append(args, "--ignore-case")
	}
	if input.FileType != "" {
		args = append(args, "--type", input.FileType)
	}

	args = append(args, input.Pattern)
	targetPath, err := filepath.Rel(resolution.RepoRoot, resolution.ResolvedPath)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("search failed: resolve scoped search path: %w", err)
	}
	if targetPath == "" {
		targetPath = "."
	}
	args = append(args, targetPath)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = resolution.RepoRoot
	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			result.Output = "No matches found"
			return result, nil
		}

		result.IsError = true
		return result, fmt.Errorf("search failed: %w", err)
	}

	formatted := strings.TrimSpace(string(output))
	lines := strings.Split(formatted, "\n")
	if len(lines) > 50 {
		formatted = strings.Join(lines[:50], "\n") + fmt.Sprintf("\n... (showing first 50 of %d matches)", len(lines))
	}

	result.Output = formatted
	return result, nil
}
