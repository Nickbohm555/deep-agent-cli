package legacycli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	openaiprovider "github.com/Nickbohm555/deep-agent-cli/internal/provider/openai"
	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	toolregistry "github.com/Nickbohm555/deep-agent-cli/internal/tools/registry"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/safety"
	"github.com/joho/godotenv"
)

type Config struct {
	Banner       string
	Verbose      bool
	AllowedTools []string
}

type filteredRegistry struct {
	base    runtime.Registry
	allowed map[string]struct{}
}

func RunInteractive(ctx context.Context, stdin io.Reader, stdout io.Writer, cfg Config) error {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	configureLogging(cfg.Verbose)

	_ = godotenv.Load()

	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}

	runtimeCtx, err := runtime.WithRepoRoot(ctx, repoRoot)
	if err != nil {
		return err
	}
	runtimeCtx = runtime.WithToolSafetyMode(runtimeCtx, safety.ModeNormal)

	registry := newFilteredRegistry(toolregistry.New(), cfg.AllowedTools)
	executor := runtime.NewToolExecutor(registry, repoRoot, safety.ModeNormal)
	orchestrator := runtime.NewOrchestrator(providerFromEnv(), registry, executor)

	scanner := bufio.NewScanner(stdin)
	conversation := []runtime.Message{}

	fmt.Fprintln(stdout, cfg.banner())
	for {
		if err := runtimeCtx.Err(); err != nil {
			return err
		}

		fmt.Fprint(stdout, "You: ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return nil
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}

		if cfg.Verbose {
			log.Printf("processing user input with %d allowed tools", len(cfg.AllowedTools))
		}

		result, err := orchestrator.RunTurn(runtimeCtx, runtime.TurnInput{
			UserMessage:  userInput,
			Conversation: conversation,
			Config: runtime.ExecutionConfig{
				Mode:              runtime.ExecutionModeInteractive,
				MaxTurns:          8,
				MaxToolIterations: 8,
				Verbose:           cfg.Verbose,
			},
		})
		if err != nil {
			return err
		}

		printToolTrace(stdout, result)
		if result.AssistantText != "" {
			fmt.Fprintf(stdout, "Assistant: %s\n", result.AssistantText)
		}

		conversation = result.Messages
	}
}

func (r filteredRegistry) ListTools(ctx context.Context) ([]runtime.ToolDefinition, error) {
	tools, err := r.base.ListTools(ctx)
	if err != nil {
		return nil, err
	}

	filtered := make([]runtime.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if _, ok := r.allowed[tool.Name]; ok {
			filtered = append(filtered, tool)
		}
	}

	return filtered, nil
}

func (r filteredRegistry) LookupTool(ctx context.Context, name string) (runtime.ToolDefinition, bool, error) {
	if _, ok := r.allowed[name]; !ok {
		return runtime.ToolDefinition{}, false, nil
	}

	return r.base.LookupTool(ctx, name)
}

func newFilteredRegistry(base runtime.Registry, allowedTools []string) runtime.Registry {
	allowed := make(map[string]struct{}, len(allowedTools))
	for _, name := range allowedTools {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}

	return filteredRegistry{
		base:    base,
		allowed: allowed,
	}
}

func providerFromEnv() runtime.ProviderClient {
	if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
		return nil
	}

	return openaiprovider.NewClient(nil)
}

func printToolTrace(out io.Writer, result runtime.TurnOutput) {
	for idx, call := range result.ToolCalls {
		fmt.Fprintf(out, "tool: %s(%s)\n", call.Name, string(call.Arguments))
		if idx >= len(result.ToolResults) {
			continue
		}

		toolResult := result.ToolResults[idx]
		fmt.Fprintf(out, "result: %s\n", toolResult.Output)
		if toolResult.IsError {
			fmt.Fprintf(out, "error: %s\n", toolResult.Output)
		}
	}
}

func configureLogging(verbose bool) {
	if verbose {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		return
	}

	log.SetOutput(io.Discard)
	log.SetFlags(0)
}

func (c Config) banner() string {
	if strings.TrimSpace(c.Banner) == "" {
		return "Chat with OpenAI + tools (use 'ctrl-c' to quit)"
	}

	return c.Banner
}
