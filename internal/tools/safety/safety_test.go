package safety

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSafety(t *testing.T) {
	t.Parallel()

	t.Run("policy matrix decisions remain deterministic", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name     string
			mode     ToolMode
			action   ToolAction
			expected Decision
		}{
			{name: "default mode allows read file", mode: "", action: ActionReadFile, expected: DecisionAllow},
			{name: "normal mode allows read file", mode: ModeNormal, action: ActionReadFile, expected: DecisionAllow},
			{name: "normal mode allows list files", mode: ModeNormal, action: ActionListFiles, expected: DecisionAllow},
			{name: "normal mode allows bash execute", mode: ModeNormal, action: ActionBashExecute, expected: DecisionAllow},
			{name: "normal mode allows code search", mode: ModeNormal, action: ActionCodeSearch, expected: DecisionAllow},
			{name: "read only mode allows read file", mode: ModeReadOnly, action: ActionReadFile, expected: DecisionAllow},
			{name: "read only mode allows list files", mode: ModeReadOnly, action: ActionListFiles, expected: DecisionAllow},
			{name: "read only mode dry runs bash execute", mode: ModeReadOnly, action: ActionBashExecute, expected: DecisionDryRun},
			{name: "read only mode allows code search", mode: ModeReadOnly, action: ActionCodeSearch, expected: DecisionAllow},
			{name: "permission aware mode allows read file", mode: ModePermissionAware, action: ActionReadFile, expected: DecisionAllow},
			{name: "permission aware mode allows list files", mode: ModePermissionAware, action: ActionListFiles, expected: DecisionAllow},
			{name: "permission aware mode requires approval for bash execute", mode: ModePermissionAware, action: ActionBashExecute, expected: DecisionRequireApproval},
			{name: "permission aware mode allows code search", mode: ModePermissionAware, action: ActionCodeSearch, expected: DecisionAllow},
			{name: "unknown action is denied", mode: ModeNormal, action: ToolAction("unknown"), expected: DecisionDeny},
			{name: "unknown mode is denied", mode: ToolMode("custom"), action: ActionReadFile, expected: DecisionDeny},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				if got := EvaluateAction(tc.mode, tc.action); got != tc.expected {
					t.Fatalf("EvaluateAction(%q, %q) = %q, want %q", tc.mode, tc.action, got, tc.expected)
				}
			})
		}
	})

	t.Run("unknown mode denies every action type", func(t *testing.T) {
		t.Parallel()

		actions := []ToolAction{
			ActionReadFile,
			ActionListFiles,
			ActionBashExecute,
			ActionCodeSearch,
		}

		for _, action := range actions {
			action := action
			t.Run(string(action), func(t *testing.T) {
				t.Parallel()

				if got := EvaluateAction(ToolMode("custom"), action); got != DecisionDeny {
					t.Fatalf("EvaluateAction(custom, %q) = %q, want %q", action, got, DecisionDeny)
				}
			})
		}
	})

	t.Run("repo guard validates local paths", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name    string
			input   string
			want    string
			wantErr string
		}{
			{name: "accept nested relative path", input: filepath.Join("dir", "file.txt"), want: filepath.Join("dir", "file.txt")},
			{name: "accept current directory", input: ".", want: "."},
			{name: "reject empty path", input: "   ", wantErr: "path is required"},
			{name: "reject traversal", input: "../outside.txt", wantErr: "must stay within the session repo"},
			{name: "reject absolute path", input: filepath.Join(string(filepath.Separator), "tmp", "outside.txt"), wantErr: "must stay within the session repo"},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := ValidateLocalPath(tc.input)
				if tc.wantErr != "" {
					if err == nil {
						t.Fatalf("ValidateLocalPath(%q) returned nil error", tc.input)
					}
					if !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("ValidateLocalPath(%q) error = %v, want substring %q", tc.input, err, tc.wantErr)
					}
					return
				}

				if err != nil {
					t.Fatalf("ValidateLocalPath(%q) returned error: %v", tc.input, err)
				}
				if got != tc.want {
					t.Fatalf("ValidateLocalPath(%q) = %q, want %q", tc.input, got, tc.want)
				}
			})
		}
	})

	t.Run("repo guard opens files inside the repo root", func(t *testing.T) {
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

		file, err := OpenInRepoRoot(ToolSafetyContext{SessionRepoRoot: repoRoot}, filepath.Join("nested", "file.txt"))
		if err != nil {
			t.Fatalf("OpenInRepoRoot returned error: %v", err)
		}
		defer file.Close()

		content, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		if string(content) != "content" {
			t.Fatalf("file content = %q, want %q", string(content), "content")
		}
	})

	t.Run("repo guard rejects symlink escapes", func(t *testing.T) {
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
		outsideFile := filepath.Join(outsideDir, "secret.txt")
		if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
			t.Fatalf("WriteFile outside returned error: %v", err)
		}
		if err := os.Symlink(outsideFile, filepath.Join(repoRoot, "escape.txt")); err != nil {
			t.Fatalf("Symlink returned error: %v", err)
		}

		file, err := OpenInRepoRoot(ToolSafetyContext{SessionRepoRoot: repoRoot}, "escape.txt")
		if err == nil {
			file.Close()
			t.Fatal("OpenInRepoRoot returned nil error for symlink escape")
		}
	})

	t.Run("command guard scopes working directory to the repo", func(t *testing.T) {
		t.Parallel()

		repoRoot := t.TempDir()
		canonicalRepoRoot, err := filepath.EvalSymlinks(repoRoot)
		if err != nil {
			t.Fatalf("EvalSymlinks returned error: %v", err)
		}
		nestedDir := filepath.Join(canonicalRepoRoot, "nested")
		if err := os.Mkdir(nestedDir, 0o755); err != nil {
			t.Fatalf("Mkdir returned error: %v", err)
		}

		cmd, cancel, decision, err := PrepareScopedCommand(context.Background(), ToolSafetyContext{
			SessionRepoRoot: repoRoot,
			Mode:            ModeNormal,
			CommandTimeout:  time.Second,
		}, ActionBashExecute, "pwd", "nested")
		if err != nil {
			t.Fatalf("PrepareScopedCommand returned error: %v", err)
		}
		defer cancel()

		if decision != DecisionAllow {
			t.Fatalf("decision = %q, want %q", decision, DecisionAllow)
		}
		if cmd.Dir != nestedDir {
			t.Fatalf("cmd.Dir = %q, want %q", cmd.Dir, nestedDir)
		}
	})

	t.Run("command guard enforces mode decisions", func(t *testing.T) {
		t.Parallel()

		repoRoot := t.TempDir()
		testCases := []struct {
			name        string
			mode        ToolMode
			expected    Decision
			wantCommand bool
			wantError   string
		}{
			{name: "unknown mode denies bash execution", mode: ToolMode("custom"), expected: DecisionDeny, wantError: "denied"},
			{name: "read only returns dry run", mode: ModeReadOnly, expected: DecisionDryRun},
			{name: "permission aware returns require approval", mode: ModePermissionAware, expected: DecisionRequireApproval},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				cmd, cancel, decision, err := PrepareScopedCommand(context.Background(), ToolSafetyContext{
					SessionRepoRoot: repoRoot,
					Mode:            tc.mode,
				}, ActionBashExecute, "pwd", "")

				if tc.wantError != "" {
					if err == nil {
						t.Fatal("PrepareScopedCommand returned nil error")
					}
					if !strings.Contains(err.Error(), tc.wantError) {
						t.Fatalf("PrepareScopedCommand error = %v, want substring %q", err, tc.wantError)
					}
				} else if err != nil {
					t.Fatalf("PrepareScopedCommand returned error: %v", err)
				}

				if cmd != nil {
					t.Fatalf("cmd = %#v, want nil", cmd)
				}
				if cancel != nil {
					t.Fatal("cancel was non-nil when command should not be prepared")
				}
				if decision != tc.expected {
					t.Fatalf("decision = %q, want %q", decision, tc.expected)
				}
			})
		}
	})

	t.Run("command guard rejects out of repo working directories", func(t *testing.T) {
		t.Parallel()

		repoRoot := t.TempDir()
		cmd, cancel, decision, err := PrepareScopedCommand(context.Background(), ToolSafetyContext{
			SessionRepoRoot: repoRoot,
			Mode:            ModeNormal,
		}, ActionBashExecute, "pwd", "../outside")
		if err == nil {
			t.Fatal("PrepareScopedCommand returned nil error")
		}
		if !strings.Contains(err.Error(), "must stay within the session repo") {
			t.Fatalf("PrepareScopedCommand error = %v, want repo scope failure", err)
		}
		if cmd != nil {
			t.Fatalf("cmd = %#v, want nil", cmd)
		}
		if cancel != nil {
			t.Fatal("cancel was non-nil for invalid working directory")
		}
		if decision != DecisionAllow {
			t.Fatalf("decision = %q, want %q", decision, DecisionAllow)
		}
	})

	t.Run("command guard applies timeout bounds", func(t *testing.T) {
		t.Parallel()

		repoRoot := t.TempDir()
		cmd, cancel, decision, err := PrepareScopedCommand(context.Background(), ToolSafetyContext{
			SessionRepoRoot: repoRoot,
			Mode:            ModeNormal,
			CommandTimeout:  10 * time.Millisecond,
		}, ActionBashExecute, "sleep 1", "")
		if err != nil {
			t.Fatalf("PrepareScopedCommand returned error: %v", err)
		}
		defer cancel()

		if decision != DecisionAllow {
			t.Fatalf("decision = %q, want %q", decision, DecisionAllow)
		}

		err = cmd.Run()
		if err == nil {
			t.Fatal("cmd.Run returned nil error, want timeout")
		}
		if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "killed") {
			t.Fatalf("cmd.Run error = %v, want deadline exceeded or process kill", err)
		}
	})
}

func TestPolicyMatrix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		mode     ToolMode
		action   ToolAction
		expected Decision
	}{
		{name: "default mode allows read file", mode: "", action: ActionReadFile, expected: DecisionAllow},
		{name: "normal mode allows read file", mode: ModeNormal, action: ActionReadFile, expected: DecisionAllow},
		{name: "normal mode allows list files", mode: ModeNormal, action: ActionListFiles, expected: DecisionAllow},
		{name: "normal mode allows bash execute", mode: ModeNormal, action: ActionBashExecute, expected: DecisionAllow},
		{name: "normal mode allows code search", mode: ModeNormal, action: ActionCodeSearch, expected: DecisionAllow},
		{name: "read only mode allows read file", mode: ModeReadOnly, action: ActionReadFile, expected: DecisionAllow},
		{name: "read only mode allows list files", mode: ModeReadOnly, action: ActionListFiles, expected: DecisionAllow},
		{name: "read only mode dry runs bash execute", mode: ModeReadOnly, action: ActionBashExecute, expected: DecisionDryRun},
		{name: "read only mode allows code search", mode: ModeReadOnly, action: ActionCodeSearch, expected: DecisionAllow},
		{name: "permission aware mode allows read file", mode: ModePermissionAware, action: ActionReadFile, expected: DecisionAllow},
		{name: "permission aware mode allows list files", mode: ModePermissionAware, action: ActionListFiles, expected: DecisionAllow},
		{name: "permission aware mode requires approval for bash execute", mode: ModePermissionAware, action: ActionBashExecute, expected: DecisionRequireApproval},
		{name: "permission aware mode allows code search", mode: ModePermissionAware, action: ActionCodeSearch, expected: DecisionAllow},
		{name: "unknown action is denied", mode: ModeNormal, action: ToolAction("unknown"), expected: DecisionDeny},
		{name: "unknown mode is denied", mode: ToolMode("custom"), action: ActionReadFile, expected: DecisionDeny},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := EvaluateAction(tc.mode, tc.action); got != tc.expected {
				t.Fatalf("EvaluateAction(%q, %q) = %q, want %q", tc.mode, tc.action, got, tc.expected)
			}
		})
	}
}

func TestRepoGuardValidateLocalPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "accept nested relative path", input: filepath.Join("dir", "file.txt"), want: filepath.Join("dir", "file.txt")},
		{name: "accept current directory", input: ".", want: "."},
		{name: "reject empty path", input: "   ", wantErr: "path is required"},
		{name: "reject traversal", input: "../outside.txt", wantErr: "must stay within the session repo"},
		{name: "reject absolute path", input: filepath.Join(string(filepath.Separator), "tmp", "outside.txt"), wantErr: "must stay within the session repo"},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ValidateLocalPath(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("ValidateLocalPath(%q) returned nil error", tc.input)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ValidateLocalPath(%q) error = %v, want substring %q", tc.input, err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ValidateLocalPath(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ValidateLocalPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestRepoGuardOpenInRepoRoot(t *testing.T) {
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

	file, err := OpenInRepoRoot(ToolSafetyContext{SessionRepoRoot: repoRoot}, filepath.Join("nested", "file.txt"))
	if err != nil {
		t.Fatalf("OpenInRepoRoot returned error: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if string(content) != "content" {
		t.Fatalf("file content = %q, want %q", string(content), "content")
	}
}

func TestRepoGuardOpenInRepoRootRejectsSymlinkEscape(t *testing.T) {
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
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile outside returned error: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(repoRoot, "escape.txt")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	file, err := OpenInRepoRoot(ToolSafetyContext{SessionRepoRoot: repoRoot}, "escape.txt")
	if err == nil {
		file.Close()
		t.Fatal("OpenInRepoRoot returned nil error for symlink escape")
	}
}

func TestCommandGuardPrepareScopedCommand(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	canonicalRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	nestedDir := filepath.Join(canonicalRepoRoot, "nested")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}

	cmd, cancel, decision, err := PrepareScopedCommand(context.Background(), ToolSafetyContext{
		SessionRepoRoot: repoRoot,
		Mode:            ModeNormal,
		CommandTimeout:  time.Second,
	}, ActionBashExecute, "pwd", "nested")
	if err != nil {
		t.Fatalf("PrepareScopedCommand returned error: %v", err)
	}
	defer cancel()

	if decision != DecisionAllow {
		t.Fatalf("decision = %q, want %q", decision, DecisionAllow)
	}
	if cmd.Dir != nestedDir {
		t.Fatalf("cmd.Dir = %q, want %q", cmd.Dir, nestedDir)
	}
}

func TestCommandGuardRejectsDeniedAction(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	cmd, cancel, decision, err := PrepareScopedCommand(context.Background(), ToolSafetyContext{
		SessionRepoRoot: repoRoot,
		Mode:            ToolMode("custom"),
	}, ActionBashExecute, "pwd", "")
	if err == nil {
		t.Fatal("PrepareScopedCommand returned nil error for denied action")
	}
	if cmd != nil {
		t.Fatalf("cmd = %#v, want nil", cmd)
	}
	if cancel != nil {
		t.Fatal("cancel was non-nil for denied action")
	}
	if decision != DecisionDeny {
		t.Fatalf("decision = %q, want %q", decision, DecisionDeny)
	}
}

func TestCommandGuardReturnsModeDecisionWithoutPreparingCommand(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	testCases := []struct {
		name     string
		mode     ToolMode
		expected Decision
	}{
		{name: "read only returns dry run", mode: ModeReadOnly, expected: DecisionDryRun},
		{name: "permission aware returns require approval", mode: ModePermissionAware, expected: DecisionRequireApproval},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd, cancel, decision, err := PrepareScopedCommand(context.Background(), ToolSafetyContext{
				SessionRepoRoot: repoRoot,
				Mode:            tc.mode,
			}, ActionBashExecute, "pwd", "")
			if err != nil {
				t.Fatalf("PrepareScopedCommand returned error: %v", err)
			}
			if cmd != nil {
				t.Fatalf("cmd = %#v, want nil", cmd)
			}
			if cancel != nil {
				t.Fatal("cancel was non-nil when command should not be prepared")
			}
			if decision != tc.expected {
				t.Fatalf("decision = %q, want %q", decision, tc.expected)
			}
		})
	}
}

func TestCommandGuardAppliesTimeout(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	cmd, cancel, decision, err := PrepareScopedCommand(context.Background(), ToolSafetyContext{
		SessionRepoRoot: repoRoot,
		Mode:            ModeNormal,
		CommandTimeout:  10 * time.Millisecond,
	}, ActionBashExecute, "sleep 1", "")
	if err != nil {
		t.Fatalf("PrepareScopedCommand returned error: %v", err)
	}
	defer cancel()

	if decision != DecisionAllow {
		t.Fatalf("decision = %q, want %q", decision, DecisionAllow)
	}

	err = cmd.Run()
	if err == nil {
		t.Fatal("cmd.Run returned nil error, want timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "killed") {
		t.Fatalf("cmd.Run error = %v, want deadline exceeded or process kill", err)
	}
}
