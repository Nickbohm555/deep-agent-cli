//go:build ignore

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/Nickbohm555/deep-agent-cli/internal/legacycli"
)

func main() {
	verbose := flag.Bool("verbose", false, "enable verbose logging")
	flag.Parse()

	if err := legacycli.RunInteractive(context.Background(), os.Stdin, os.Stdout, legacycli.Config{
		Banner:       "Chat with OpenAI + tools (use 'ctrl-c' to quit)",
		Verbose:      *verbose,
		AllowedTools: []string{"read_file", "list_files", "bash", "code_search"},
	}); err != nil {
		fmt.Printf("Error: %s\n", err.Error())
	}
}
