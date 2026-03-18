package sandbox

import (
	"fmt"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

type ScopeTarget struct {
	ToolName   string
	Operation  string
	Path       string
	WorkingDir string
}

type ScopeResolution struct {
	RepoRoot       string
	ResolvedPath   string
	ResolvedTarget string
	WorkingDir     string
}

func EnforceRepoScope(repoRoot string, target ScopeTarget) (ScopeResolution, error) {
	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		return ScopeResolution{}, fmt.Errorf("%s: %w", scopeLabel(target), err)
	}

	resolution := ScopeResolution{
		RepoRoot:   canonicalRoot,
		WorkingDir: canonicalRoot,
	}

	if strings.TrimSpace(target.Path) != "" {
		resolvedPath, err := session.ResolvePathWithinRepo(canonicalRoot, target.Path)
		if err != nil {
			return ScopeResolution{}, fmt.Errorf("%s target %q denied: %w", scopeLabel(target), strings.TrimSpace(target.Path), err)
		}
		resolution.ResolvedPath = resolvedPath
		resolution.ResolvedTarget = resolvedPath
	}

	if strings.TrimSpace(target.WorkingDir) != "" {
		workingDir, err := session.ResolvePathWithinRepo(canonicalRoot, target.WorkingDir)
		if err != nil {
			return ScopeResolution{}, fmt.Errorf("%s working directory %q denied: %w", scopeLabel(target), strings.TrimSpace(target.WorkingDir), err)
		}
		resolution.WorkingDir = workingDir
		if resolution.ResolvedTarget == "" {
			resolution.ResolvedTarget = workingDir
		}
	}

	if resolution.ResolvedTarget == "" {
		resolution.ResolvedTarget = canonicalRoot
	}

	return resolution, nil
}

func scopeLabel(target ScopeTarget) string {
	toolName := strings.TrimSpace(target.ToolName)
	if toolName == "" {
		toolName = "tool"
	}

	operation := strings.TrimSpace(target.Operation)
	if operation == "" {
		return toolName
	}

	return fmt.Sprintf("%s %s", toolName, operation)
}
