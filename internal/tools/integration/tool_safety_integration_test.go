package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/safety"
)

func TestToolSafetyIntegration(t *testing.T) {
	t.Parallel()

	t.Run("in_repo_success_matches_contract_goldens", func(t *testing.T) {
		t.Parallel()

		repoRoot := fixtureRepoRoot(t)

		t.Run("read_file", func(t *testing.T) {
			var golden struct {
				Success string `json:"success"`
			}
			readGolden(t, "read_file.golden", &golden)

			result, err := handlers.ReadFile(bindToolContext(t, repoRoot, ""), toolCall(t, "read_file", handlers.ReadFileInput{
				Path: "README.md",
			}))
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}

			assertNormalizedOutput(t, repoRoot, result.Output, golden.Success)
		})

		t.Run("list_files", func(t *testing.T) {
			var golden struct {
				Success string `json:"success"`
			}
			readGolden(t, "list_files.golden", &golden)

			result, err := handlers.ListFiles(bindToolContext(t, repoRoot, ""), toolCall(t, "list_files", handlers.ListFilesInput{
				Path: ".",
			}))
			if err != nil {
				t.Fatalf("ListFiles returned error: %v", err)
			}

			assertNormalizedOutput(t, repoRoot, result.Output, golden.Success)
		})

		t.Run("bash", func(t *testing.T) {
			var golden struct {
				Success string `json:"success"`
			}
			readGolden(t, "bash.golden", &golden)

			result, err := handlers.Bash(bindToolContext(t, repoRoot, ""), toolCall(t, "bash", handlers.BashInput{
				Command: "printf 'alpha-123-beta'",
			}))
			if err != nil {
				t.Fatalf("Bash returned error: %v", err)
			}

			assertNormalizedOutput(t, repoRoot, result.Output, golden.Success)
		})

		t.Run("code_search", func(t *testing.T) {
			var golden struct {
				Success string `json:"success"`
			}
			readGolden(t, "code_search.golden", &golden)

			result, err := handlers.CodeSearch(bindToolContext(t, repoRoot, ""), toolCall(t, "code_search", handlers.CodeSearchInput{
				Pattern: `alpha-[0-9]+-beta`,
				Path:    ".",
			}))
			if err != nil {
				t.Fatalf("CodeSearch returned error: %v", err)
			}

			assertNormalizedOutput(t, repoRoot, result.Output, golden.Success)
		})
	})

	t.Run("out_of_repo_paths_are_denied", func(t *testing.T) {
		t.Parallel()

		repoRoot := fixtureRepoRoot(t)
		ctx := bindToolContext(t, repoRoot, "")

		testCases := []struct {
			name    string
			invoke  func() error
			wantErr string
		}{
			{
				name: "read_file",
				invoke: func() error {
					_, err := handlers.ReadFile(ctx, toolCall(t, "read_file", handlers.ReadFileInput{Path: "../outside.txt"}))
					return err
				},
				wantErr: "escapes repository scope",
			},
			{
				name: "list_files",
				invoke: func() error {
					_, err := handlers.ListFiles(ctx, toolCall(t, "list_files", handlers.ListFilesInput{Path: "../outside"}))
					return err
				},
				wantErr: "escapes repository scope",
			},
			{
				name: "bash",
				invoke: func() error {
					_, err := handlers.Bash(ctx, toolCall(t, "bash", handlers.BashInput{
						Command:    "pwd",
						WorkingDir: "../outside",
					}))
					return err
				},
				wantErr: "escapes repository scope",
			},
			{
				name: "code_search",
				invoke: func() error {
					_, err := handlers.CodeSearch(ctx, toolCall(t, "code_search", handlers.CodeSearchInput{
						Pattern: "alpha",
						Path:    "../outside",
					}))
					return err
				},
				wantErr: "escapes repository scope",
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				err := tc.invoke()
				if err == nil {
					t.Fatalf("%s returned nil error for out-of-repo input", tc.name)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("%s error = %v, want substring %q", tc.name, err, tc.wantErr)
				}
			})
		}
	})

	t.Run("bash_mode_behavior_changes_by_safety_mode", func(t *testing.T) {
		t.Parallel()

		repoRoot := fixtureRepoRoot(t)
		testCases := []struct {
			name string
			mode safety.ToolMode
			want string
		}{
			{
				name: "normal",
				mode: safety.ModeNormal,
				want: "alpha-123-beta",
			},
			{
				name: "read_only",
				mode: safety.ModeReadOnly,
				want: `Command not executed in mode "read_only"`,
			},
			{
				name: "permission_aware",
				mode: safety.ModePermissionAware,
				want: `Command requires approval in mode "permission_aware"`,
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				result, err := handlers.Bash(bindToolContext(t, repoRoot, tc.mode), toolCall(t, "bash", handlers.BashInput{
					Command: "printf 'alpha-123-beta'",
				}))
				if err != nil {
					t.Fatalf("Bash returned error: %v", err)
				}
				if result.Output != tc.want {
					t.Fatalf("Bash output = %q, want %q", result.Output, tc.want)
				}
			})
		}
	})
}

func bindToolContext(t *testing.T, repoRoot string, mode safety.ToolMode) context.Context {
	t.Helper()

	ctx, err := runtime.WithRepoRoot(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}

	return runtime.WithToolSafetyMode(ctx, mode)
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

func fixtureRepoRoot(t *testing.T) string {
	t.Helper()

	return filepath.Join(integrationRootDir(t), "..", "contracts", "fixtures", "fixture_repo")
}

func integrationRootDir(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("locate integration test file: runtime caller unavailable")
	}

	return filepath.Dir(filename)
}

func readGolden(t *testing.T, name string, target any) {
	t.Helper()

	path := filepath.Join(integrationRootDir(t), "..", "contracts", "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}

	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("unmarshal golden %s: %v", path, err)
	}
}

func assertNormalizedOutput(t *testing.T, repoRoot, actual, expected string) {
	t.Helper()

	normalized := strings.ReplaceAll(actual, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, `\`, `/`)
	normalized = strings.ReplaceAll(normalized, repoRoot, "<FIXTURE_ROOT>")
	normalized = strings.ReplaceAll(normalized, filepath.ToSlash(repoRoot), "<FIXTURE_ROOT>")
	if normalized != expected {
		t.Fatalf("output mismatch\nexpected:\n%s\nactual:\n%s", expected, normalized)
	}
}
