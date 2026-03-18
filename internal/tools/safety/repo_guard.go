package safety

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

func ValidateLocalPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path is required")
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return cleaned, nil
	}
	if !filepath.IsLocal(cleaned) {
		return "", fmt.Errorf("path %q must stay within the session repo", trimmed)
	}

	return cleaned, nil
}

func OpenInRepoRoot(safetyCtx ToolSafetyContext, targetPath string) (*os.File, error) {
	repoRoot, err := session.CanonicalizeRepoRoot(safetyCtx.SessionRepoRoot)
	if err != nil {
		return nil, fmt.Errorf("open in repo root: %w", err)
	}

	localPath, err := ValidateLocalPath(targetPath)
	if err != nil {
		return nil, fmt.Errorf("open in repo root: %w", err)
	}

	file, err := os.OpenInRoot(repoRoot, localPath)
	if err != nil {
		return nil, fmt.Errorf("open in repo root %q: %w", localPath, err)
	}

	return file, nil
}
