package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnforceRepoScopeAllowsInRepoTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	nestedDir := filepath.Join(repoRoot, "nested")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	filePath := filepath.Join(nestedDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	resolution, err := EnforceRepoScope(repoRoot, ScopeTarget{
		ToolName:   "read_file",
		Operation:  "read",
		Path:       filepath.Join("nested", "file.txt"),
		WorkingDir: "nested",
	})
	if err != nil {
		t.Fatalf("EnforceRepoScope returned error: %v", err)
	}

	wantRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks repoRoot returned error: %v", err)
	}
	wantFile, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		t.Fatalf("EvalSymlinks filePath returned error: %v", err)
	}
	wantWorkingDir, err := filepath.EvalSymlinks(nestedDir)
	if err != nil {
		t.Fatalf("EvalSymlinks nestedDir returned error: %v", err)
	}

	if resolution.RepoRoot != wantRepoRoot {
		t.Fatalf("RepoRoot = %q, want %q", resolution.RepoRoot, wantRepoRoot)
	}
	if resolution.ResolvedPath != wantFile {
		t.Fatalf("ResolvedPath = %q, want %q", resolution.ResolvedPath, wantFile)
	}
	if resolution.WorkingDir != wantWorkingDir {
		t.Fatalf("WorkingDir = %q, want %q", resolution.WorkingDir, wantWorkingDir)
	}
}

func TestEnforceRepoScopeDefaultsWorkingDirectoryToRepoRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	resolution, err := EnforceRepoScope(repoRoot, ScopeTarget{
		ToolName:  "bash",
		Operation: "execute",
	})
	if err != nil {
		t.Fatalf("EnforceRepoScope returned error: %v", err)
	}

	wantRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}

	if resolution.WorkingDir != wantRepoRoot {
		t.Fatalf("WorkingDir = %q, want %q", resolution.WorkingDir, wantRepoRoot)
	}
	if resolution.ResolvedTarget != wantRepoRoot {
		t.Fatalf("ResolvedTarget = %q, want %q", resolution.ResolvedTarget, wantRepoRoot)
	}
}

func TestEnforceRepoScopeRejectsEscapedPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	_, err := EnforceRepoScope(repoRoot, ScopeTarget{
		ToolName:  "list_files",
		Operation: "list",
		Path:      "../outside.txt",
	})
	if err == nil {
		t.Fatal("EnforceRepoScope returned nil error, want scope denial")
	}
	if !strings.Contains(err.Error(), "list_files list target") {
		t.Fatalf("error = %v, want tool context", err)
	}
	if !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("error = %v, want scope denial", err)
	}
}

func TestEnforceRepoScopeRejectsEscapedWorkingDirectory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	outsideDir := filepath.Join(tempDir, "outside")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatalf("Mkdir repo returned error: %v", err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatalf("Mkdir outside returned error: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(repoRoot, "escape")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	_, err := EnforceRepoScope(repoRoot, ScopeTarget{
		ToolName:   "bash",
		Operation:  "execute",
		WorkingDir: "escape",
	})
	if err == nil {
		t.Fatal("EnforceRepoScope returned nil error, want working-directory denial")
	}
	if !strings.Contains(err.Error(), "bash execute working directory") {
		t.Fatalf("error = %v, want working-directory context", err)
	}
	if !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("error = %v, want scope denial", err)
	}
}
