package runtime_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/registry"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/safety"
)

func TestToolExecutorDispatchesRegisteredToolWithInjectedSafetyContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeToolFixture(t, repoRoot, "README.md", "scoped file contents")

	executor := runtime.NewToolExecutor(registry.New(), repoRoot, safety.ModeReadOnly)
	result, err := executor.Execute(context.Background(), toolCallForExecutorTest(t, "read_file", handlers.ReadFileInput{
		Path: "README.md",
	}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Output != "scoped file contents" {
		t.Fatalf("result output = %q, want %q", result.Output, "scoped file contents")
	}
	if result.IsError {
		t.Fatal("result unexpectedly marked as error")
	}
}

func TestToolExecutorUsesContextRepoRootAndModeWhenUnsetOnExecutor(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	ctx, err := runtime.WithRepoRoot(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}
	ctx = runtime.WithToolSafetyMode(ctx, safety.ModePermissionAware)

	executor := runtime.NewToolExecutor(registry.New(), "", "")
	result, err := executor.Dispatch(ctx, toolCallForExecutorTest(t, "bash", handlers.BashInput{
		Command: "printf 'should not run'",
	}))
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if result.Output != `Command requires approval in mode "permission_aware"` {
		t.Fatalf("result output = %q, want permission-aware approval message", result.Output)
	}
	if result.IsError {
		t.Fatal("result unexpectedly marked as error")
	}
}

func TestToolExecutorReturnsHelpfulErrorsForMissingRegistrationOrScope(t *testing.T) {
	t.Parallel()

	executor := runtime.NewToolExecutor(registry.New(), "", "")

	_, err := executor.Execute(context.Background(), toolCallForExecutorTest(t, "missing_tool", map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), `tool not registered`) {
		t.Fatalf("Execute missing tool error = %v, want unregistered tool error", err)
	}

	_, err = executor.Execute(context.Background(), toolCallForExecutorTest(t, "read_file", handlers.ReadFileInput{
		Path: "README.md",
	}))
	if err == nil || !strings.Contains(err.Error(), "tool execution requires a bound repository root") {
		t.Fatalf("Execute missing repo scope error = %v, want bound repository root error", err)
	}
}

func toolCallForExecutorTest(t *testing.T, name string, payload any) runtime.ToolCall {
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

func writeToolFixture(t *testing.T, repoRoot, relPath, contents string) {
	t.Helper()

	fullPath := filepath.Join(repoRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
