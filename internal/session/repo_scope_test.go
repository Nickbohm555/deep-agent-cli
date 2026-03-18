package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCanonicalizeRepoRootResolvesRelativeAndSymlinkedRoots(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}

	linkRoot := filepath.Join(tempDir, "repo-link")
	if err := os.Symlink(repoRoot, linkRoot); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	relativeInput, err := filepath.Rel(tempDir, linkRoot)
	if err != nil {
		t.Fatalf("Rel returned error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	got, err := CanonicalizeRepoRoot(relativeInput)
	if err != nil {
		t.Fatalf("CanonicalizeRepoRoot returned error: %v", err)
	}
	want, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	if got != want {
		t.Fatalf("CanonicalizeRepoRoot = %q, want %q", got, want)
	}
}

func TestResolvePathWithinRepoAllowsExactRootAndDescendants(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	nestedDir := filepath.Join(repoRoot, "dir")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	filePath := filepath.Join(nestedDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	resolvedRoot, err := ResolvePathWithinRepo(repoRoot, ".")
	if err != nil {
		t.Fatalf("ResolvePathWithinRepo root returned error: %v", err)
	}
	wantRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	if resolvedRoot != wantRoot {
		t.Fatalf("resolved root = %q, want %q", resolvedRoot, wantRoot)
	}

	resolvedFile, err := ResolvePathWithinRepo(repoRoot, filepath.Join("dir", "..", "dir", "file.txt"))
	if err != nil {
		t.Fatalf("ResolvePathWithinRepo file returned error: %v", err)
	}
	wantFile, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	if resolvedFile != wantFile {
		t.Fatalf("resolved file = %q, want %q", resolvedFile, wantFile)
	}
}

func TestResolvePathWithinRepoRejectsTraversalEscape(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}

	escaped, err := ResolvePathWithinRepo(repoRoot, "../outside.txt")
	if err == nil {
		t.Fatalf("ResolvePathWithinRepo returned %q, want scope error", escaped)
	}
	if !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("error = %v, want scope error", err)
	}
}

func TestResolvePathWithinRepoRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	outsideDir := filepath.Join(tempDir, "outside")
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	escapeLink := filepath.Join(repoRoot, "escape-link")
	if err := os.Symlink(outsideDir, escapeLink); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	resolved, err := ResolvePathWithinRepo(repoRoot, filepath.Join("escape-link", "secret.txt"))
	if err == nil {
		t.Fatalf("ResolvePathWithinRepo returned %q, want scope error", resolved)
	}
	if !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("error = %v, want scope error", err)
	}
}

func TestEnsureSessionRepoRootImmutableRejectsRebinding(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	otherRoot := filepath.Join(tempDir, "other")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	if err := os.Mkdir(otherRoot, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}

	s := Session{
		ThreadID:  "thread-123",
		RepoRoot:  repoRoot,
		CreatedAt: time.Now(),
	}

	if err := EnsureSessionRepoRootImmutable(s, repoRoot); err != nil {
		t.Fatalf("EnsureSessionRepoRootImmutable same root returned error: %v", err)
	}

	err := EnsureSessionRepoRootImmutable(s, otherRoot)
	if err == nil {
		t.Fatal("EnsureSessionRepoRootImmutable returned nil error, want rebinding error")
	}
	if !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("error = %v, want rebinding error", err)
	}
}
