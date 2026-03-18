package snapshot

import (
	"fmt"
	"path/filepath"
	"strings"
)

var indexableExtensions = map[string]struct{}{
	".go":   {},
	".json": {},
	".md":   {},
	".toml": {},
	".txt":  {},
	".yaml": {},
	".yml":  {},
}

var skippedDirectoryNames = map[string]struct{}{
	".git":         {},
	"bin":          {},
	"binaries":     {},
	"build":        {},
	"dist":         {},
	"node_modules": {},
	"vendor":       {},
}

func ShouldIndexPath(relPath string) bool {
	normalized, err := normalizeSnapshotPath(relPath)
	if err != nil {
		return false
	}

	parts := strings.Split(normalized, "/")
	if len(parts) == 0 {
		return false
	}

	for i, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") {
			return false
		}
		if i < len(parts)-1 {
			if _, skipped := skippedDirectoryNames[strings.ToLower(part)]; skipped {
				return false
			}
		}
	}

	_, ok := indexableExtensions[strings.ToLower(filepath.Ext(normalized))]
	return ok
}

func shouldSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}

	_, skipped := skippedDirectoryNames[strings.ToLower(name)]
	return skipped
}

func normalizeSnapshotPath(relPath string) (string, error) {
	trimmed := strings.TrimSpace(relPath)
	if trimmed == "" {
		return "", fmt.Errorf("rel_path is required")
	}
	trimmed = strings.ReplaceAll(trimmed, `\`, "/")
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("rel_path must be relative: %q", relPath)
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return "", fmt.Errorf("rel_path is required")
	}

	normalized := filepath.ToSlash(cleaned)
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return "", fmt.Errorf("rel_path escapes repo root: %q", relPath)
		}
	}

	return normalized, nil
}
