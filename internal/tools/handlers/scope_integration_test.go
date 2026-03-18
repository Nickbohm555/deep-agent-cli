package handlers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

func TestReadFileEnforcesRepoScope(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "inside.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile inside.txt returned error: %v", err)
	}

	ctx := mustBindRepoRoot(t, repoRoot)
	result, err := ReadFile(ctx, toolCall(t, "read_file", ReadFileInput{Path: "inside.txt"}))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if result.Output != "hello" {
		t.Fatalf("ReadFile output = %q, want hello", result.Output)
	}

	_, err = ReadFile(ctx, toolCall(t, "read_file", ReadFileInput{Path: "../outside.txt"}))
	if err == nil {
		t.Fatal("ReadFile returned nil error for escaped path")
	}
	if !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("error = %v, want scope denial", err)
	}
}

func TestListFilesEnforcesRepoScope(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, "nested"), 0o755); err != nil {
		t.Fatalf("Mkdir nested returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile nested/file.txt returned error: %v", err)
	}

	ctx := mustBindRepoRoot(t, repoRoot)
	result, err := ListFiles(ctx, toolCall(t, "list_files", ListFilesInput{Path: "nested"}))
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}

	var files []string
	if err := json.Unmarshal([]byte(result.Output), &files); err != nil {
		t.Fatalf("ListFiles output is not valid JSON: %v", err)
	}
	if len(files) != 1 || files[0] != "file.txt" {
		t.Fatalf("ListFiles output = %v, want [file.txt]", files)
	}

	_, err = ListFiles(ctx, toolCall(t, "list_files", ListFilesInput{Path: "../outside"}))
	if err == nil {
		t.Fatal("ListFiles returned nil error for escaped path")
	}
	if !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("error = %v, want scope denial", err)
	}
}

func TestCodeSearchEnforcesRepoScope(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, "subdir"), 0o755); err != nil {
		t.Fatalf("Mkdir subdir returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "subdir", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile subdir/main.go returned error: %v", err)
	}

	ctx := mustBindRepoRoot(t, repoRoot)
	result, err := CodeSearch(ctx, toolCall(t, "code_search", CodeSearchInput{
		Pattern:  "func main",
		Path:     "subdir",
		FileType: "go",
	}))
	if err != nil {
		t.Fatalf("CodeSearch returned error: %v", err)
	}
	if !strings.Contains(result.Output, "subdir/main.go:2:func main() {}") {
		t.Fatalf("CodeSearch output = %q, want match in repo", result.Output)
	}

	_, err = CodeSearch(ctx, toolCall(t, "code_search", CodeSearchInput{
		Pattern: "func main",
		Path:    "../outside",
	}))
	if err == nil {
		t.Fatal("CodeSearch returned nil error for escaped path")
	}
	if !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("error = %v, want scope denial", err)
	}
}

func TestBashLocksWorkingDirectoryToBoundRepo(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, "nested"), 0o755); err != nil {
		t.Fatalf("Mkdir nested returned error: %v", err)
	}

	ctx := mustBindRepoRoot(t, repoRoot)
	result, err := Bash(ctx, toolCall(t, "bash", BashInput{
		Command:    "pwd",
		WorkingDir: "nested",
	}))
	if err != nil {
		t.Fatalf("Bash returned error: %v", err)
	}

	wantWorkingDir, err := filepath.EvalSymlinks(filepath.Join(repoRoot, "nested"))
	if err != nil {
		t.Fatalf("EvalSymlinks nested returned error: %v", err)
	}
	if result.Output != wantWorkingDir {
		t.Fatalf("Bash output = %q, want %q", result.Output, wantWorkingDir)
	}

	_, err = Bash(ctx, toolCall(t, "bash", BashInput{
		Command:    "pwd",
		WorkingDir: "../outside",
	}))
	if err == nil {
		t.Fatal("Bash returned nil error for escaped working directory")
	}
	if !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("error = %v, want scope denial", err)
	}
}

func mustBindRepoRoot(t *testing.T, repoRoot string) context.Context {
	t.Helper()

	ctx, err := runtime.WithRepoRoot(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}
	return ctx
}

func toolCall(t *testing.T, name string, payload any) runtime.ToolCall {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	return runtime.ToolCall{
		ID:        "call-1",
		Name:      name,
		Arguments: raw,
	}
}
