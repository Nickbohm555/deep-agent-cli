package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func CanonicalizeRepoRoot(repoRoot string) (string, error) {
	trimmed := strings.TrimSpace(repoRoot)
	if trimmed == "" {
		return "", fmt.Errorf("repo root is required")
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("canonicalize repo root %q: %w", trimmed, err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("canonicalize repo root %q: %w", trimmed, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat repo root %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repo root %q is not a directory", resolved)
	}

	return resolved, nil
}

func ResolvePathWithinRepo(repoRoot, candidate string) (string, error) {
	root, err := CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		return "", err
	}

	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return "", fmt.Errorf("path is required")
	}

	target := trimmed
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", trimmed, err)
	}

	resolved, err := evalSymlinksPreservingMissing(abs)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", trimmed, err)
	}

	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("check path %q against repo scope: %w", trimmed, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes repository scope %q", trimmed, root)
	}

	return resolved, nil
}

func EnsureSessionRepoRootImmutable(existing Session, repoRoot string) error {
	if strings.TrimSpace(repoRoot) == "" {
		return nil
	}

	existingRoot, err := CanonicalizeRepoRoot(existing.RepoRoot)
	if err != nil {
		return fmt.Errorf("canonicalize existing repo root: %w", err)
	}

	candidateRoot, err := CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		return fmt.Errorf("canonicalize requested repo root: %w", err)
	}

	if existingRoot != candidateRoot {
		return fmt.Errorf("session %q is already bound to repo root %q", existing.ThreadID, existingRoot)
	}

	return nil
}

func evalSymlinksPreservingMissing(path string) (string, error) {
	parts := make([]string, 0)
	current := path
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(parts) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, parts[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(path), nil
		}

		parts = append(parts, filepath.Base(current))
		current = parent
	}
}
