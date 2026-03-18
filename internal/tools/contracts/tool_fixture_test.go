package contracts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolFixtureSanity(t *testing.T) {
	t.Parallel()

	fixtureRoot := filepath.Join(contractsRootDir(t), "fixtures", "fixture_repo")

	readmePath := filepath.Join(fixtureRoot, "README.md")
	readmeContent, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README fixture: %v", err)
	}

	if got := string(readmeContent); !strings.Contains(got, "README_SEARCH_TOKEN") {
		t.Fatalf("README fixture missing search token: %q", got)
	}

	examplePath := filepath.Join(fixtureRoot, "src", "example.go")
	exampleContent, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read example fixture: %v", err)
	}

	exampleText := string(exampleContent)
	if !strings.Contains(exampleText, `const SearchToken = "EXAMPLE_SEARCH_TOKEN"`) {
		t.Fatalf("example fixture missing search constant: %q", exampleText)
	}
	if !strings.Contains(exampleText, `return "alpha-123-beta"`) {
		t.Fatalf("example fixture missing regex target: %q", exampleText)
	}

	entries, err := os.ReadDir(filepath.Join(fixtureRoot, "src"))
	if err != nil {
		t.Fatalf("read src fixture directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "example.go" {
		t.Fatalf("unexpected src fixture contents: %+v", entries)
	}
}
