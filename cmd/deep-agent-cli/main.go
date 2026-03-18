package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/platform/db"
	"github.com/joho/godotenv"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	sessionpostgres "github.com/Nickbohm555/deep-agent-cli/internal/session/postgres"
	toolregistry "github.com/Nickbohm555/deep-agent-cli/internal/tools/registry"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin *os.File, stdout *os.File) error {
	fs := flag.NewFlagSet("deep-agent-cli", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	mode := fs.String("mode", string(runtime.ExecutionModeInteractive), "execution mode: interactive or oneshot")
	prompt := fs.String("prompt", "", "prompt to run in oneshot mode")
	model := fs.String("model", "", "model name to forward to the runtime")
	systemPrompt := fs.String("system-prompt", "", "optional system prompt")
	sessionID := fs.String("session", "", "existing thread ID to resume; omit to create a new thread")
	repoRoot := fs.String("repo-root", "", "repository root to bind to a new session (defaults to current working directory)")
	maxTurns := fs.Int("max-turns", 8, "maximum model turns per request")
	maxToolIterations := fs.Int("max-tool-iterations", 8, "maximum tool iterations per request")
	showRegistry := fs.Bool("registry", false, "print the registered tools and exit")
	verbose := fs.Bool("verbose", false, "enable verbose runtime output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	_ = godotenv.Load()
	if *showRegistry {
		return printRegistry(stdout)
	}
	if err := validateStartup(*mode, *prompt, *maxTurns, *maxToolIterations); err != nil {
		return err
	}

	cfg := runtime.ExecutionConfig{
		Mode:              runtime.ExecutionMode(*mode),
		Model:             *model,
		SystemPrompt:      *systemPrompt,
		MaxTurns:          *maxTurns,
		MaxToolIterations: *maxToolIterations,
		Verbose:           *verbose,
	}

	pool, err := db.NewPoolFromEnv(ctx)
	if err != nil {
		return fmt.Errorf("initialize session database: %w", err)
	}
	defer pool.Close()

	bootstrap, err := runtime.CreateOrResumeSession(ctx, sessionpostgres.New(pool), runtime.SessionLifecycleParams{
		ThreadID: *sessionID,
		RepoRoot: *repoRoot,
	})
	if err != nil {
		return err
	}

	if stdout == nil {
		stdout = os.Stdout
	}
	fmt.Fprintf(stdout, "Session: %s\n", bootstrap.Session.ThreadID)

	orchestrator := runtime.NewOrchestrator(nil, nil, nil)

	if *mode == string(runtime.ExecutionModeInteractive) {
		driver := runtime.InteractiveDriver{
			Runner:    orchestrator,
			Config:    cfg,
			In:        stdin,
			Out:       stdout,
			SessionID: bootstrap.Session.ThreadID,
			History:   bootstrap.Conversation,
		}
		return driver.Run(ctx)
	}

	if *mode == string(runtime.ExecutionModeOneShot) {
		driver := runtime.OneShotDriver{
			Runner:    orchestrator,
			Config:    cfg,
			Out:       stdout,
			SessionID: bootstrap.Session.ThreadID,
			History:   bootstrap.Conversation,
		}
		_, err := driver.Run(ctx, *prompt)
		return err
	}

	return fmt.Errorf("unsupported mode %q (expected %q or %q)", *mode, runtime.ExecutionModeInteractive, runtime.ExecutionModeOneShot)
}

func validateStartup(mode, prompt string, maxTurns, maxToolIterations int) error {
	trimmedMode := strings.TrimSpace(mode)
	switch runtime.ExecutionMode(trimmedMode) {
	case runtime.ExecutionModeInteractive, runtime.ExecutionModeOneShot:
	default:
		return fmt.Errorf("unsupported mode %q (expected %q or %q)", mode, runtime.ExecutionModeInteractive, runtime.ExecutionModeOneShot)
	}

	if runtime.ExecutionMode(trimmedMode) == runtime.ExecutionModeOneShot && strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("prompt is required in oneshot mode; rerun with -prompt \"your request\"")
	}
	if maxTurns <= 0 {
		return fmt.Errorf("max-turns must be greater than 0")
	}
	if maxToolIterations <= 0 {
		return fmt.Errorf("max-tool-iterations must be greater than 0")
	}

	if os.Getenv("OPENAI_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set; continuing in local fallback mode. Export OPENAI_API_KEY to enable model-backed responses.")
	}

	return nil
}

func printRegistry(stdout *os.File) error {
	out := stdout
	if out == nil {
		out = os.Stdout
	}

	fmt.Fprintln(out, "Registered tools:")
	for _, tool := range toolregistry.Definitions() {
		fmt.Fprintf(out, "- %s: %s\n", tool.Name, tool.Description)
	}

	return nil
}
