package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
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
	maxTurns := fs.Int("max-turns", 8, "maximum model turns per request")
	maxToolIterations := fs.Int("max-tool-iterations", 8, "maximum tool iterations per request")
	verbose := fs.Bool("verbose", false, "enable verbose runtime output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	_ = godotenv.Load()

	cfg := runtime.ExecutionConfig{
		Mode:              runtime.ExecutionMode(*mode),
		Model:             *model,
		SystemPrompt:      *systemPrompt,
		MaxTurns:          *maxTurns,
		MaxToolIterations: *maxToolIterations,
		Verbose:           *verbose,
	}

	orchestrator := runtime.NewOrchestrator(nil, nil, nil)

	if *mode == string(runtime.ExecutionModeInteractive) {
		driver := runtime.InteractiveDriver{
			Runner: orchestrator,
			Config: cfg,
			In:     stdin,
			Out:    stdout,
		}
		return driver.Run(ctx)
	}

	if *mode == string(runtime.ExecutionModeOneShot) {
		driver := runtime.OneShotDriver{
			Runner: orchestrator,
			Config: cfg,
			Out:    stdout,
		}
		_, err := driver.Run(ctx, *prompt)
		return err
	}

	return fmt.Errorf("unsupported mode %q (expected %q or %q)", *mode, runtime.ExecutionModeInteractive, runtime.ExecutionModeOneShot)
}
