package indexing

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
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

func DiscoverIndexableFiles(repoRoot string) ([]string, error) {
	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("discover indexable files: %w", err)
	}

	files := make([]string, 0)
	err = filepath.WalkDir(canonicalRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == canonicalRoot {
			return nil
		}

		relPath, err := filepath.Rel(canonicalRoot, path)
		if err != nil {
			return fmt.Errorf("relative path for %q: %w", path, err)
		}

		if entry.IsDir() {
			if shouldSkipDiscoveryDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldSkipDiscoveryFile(entry.Name()) || !isIndexableExtension(path) {
			return nil
		}

		if !isPathWithinRoot(canonicalRoot, path) {
			return nil
		}

		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat discovered path %q: %w", path, err)
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}

		files = append(files, filepath.ToSlash(relPath))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover indexable files under %q: %w", canonicalRoot, err)
	}

	slices.Sort(files)
	return files, nil
}

func shouldSkipDiscoveryDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}

	_, skipped := skippedDirectoryNames[strings.ToLower(name)]
	return skipped
}

func shouldSkipDiscoveryFile(name string) bool {
	return strings.HasPrefix(name, ".")
}

func isIndexableExtension(path string) bool {
	_, ok := indexableExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func isPathWithinRoot(root, candidate string) bool {
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
