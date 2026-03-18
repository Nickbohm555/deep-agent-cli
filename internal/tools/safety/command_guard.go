package safety

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

const defaultCommandTimeout = 30 * time.Second

func PrepareScopedCommand(parent context.Context, safetyCtx ToolSafetyContext, action ToolAction, command string, workingDir string) (*exec.Cmd, context.CancelFunc, Decision, error) {
	decision := EvaluateAction(safetyCtx.EffectiveMode(), action)
	switch decision {
	case DecisionDeny:
		return nil, nil, decision, fmt.Errorf("command execution denied for action %q in mode %q", action, safetyCtx.EffectiveMode())
	case DecisionDryRun, DecisionRequireApproval:
		return nil, nil, decision, nil
	}

	if command == "" {
		return nil, nil, decision, fmt.Errorf("command is required")
	}

	scopedDir, err := resolveRepoDirectory(safetyCtx, workingDir)
	if err != nil {
		return nil, nil, decision, err
	}

	commandCtx, cancel := context.WithTimeout(parent, effectiveCommandTimeout(safetyCtx.CommandTimeout))
	cmd := exec.CommandContext(commandCtx, "bash", "-c", command)
	cmd.Dir = scopedDir

	return cmd, cancel, decision, nil
}

func resolveRepoDirectory(safetyCtx ToolSafetyContext, workingDir string) (string, error) {
	repoRoot, err := session.CanonicalizeRepoRoot(safetyCtx.SessionRepoRoot)
	if err != nil {
		return "", fmt.Errorf("prepare scoped command: %w", err)
	}

	if workingDir == "" {
		return repoRoot, nil
	}

	localPath, err := ValidateLocalPath(workingDir)
	if err != nil {
		return "", fmt.Errorf("prepare scoped command: %w", err)
	}

	resolvedPath, err := session.ResolvePathWithinRepo(repoRoot, localPath)
	if err != nil {
		return "", fmt.Errorf("prepare scoped command: %w", err)
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("prepare scoped command: stat working directory %q: %w", localPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("prepare scoped command: working directory %q is not a directory", localPath)
	}

	return resolvedPath, nil
}

func effectiveCommandTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}

	return defaultCommandTimeout
}
