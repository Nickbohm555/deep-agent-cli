package indexing

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestDiscoverIndexableFiles(t *testing.T) {
	repoRoot := copyDiscoveryFixture(t)

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "escape.md")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", outsideFile, err)
	}

	escapeLink := filepath.Join(repoRoot, "docs", "escape-link.md")
	if err := os.Symlink(outsideFile, escapeLink); err != nil {
		t.Fatalf("Symlink(%q -> %q) returned error: %v", escapeLink, outsideFile, err)
	}

	insideTarget := filepath.Join(repoRoot, "README.md")
	insideLink := filepath.Join(repoRoot, "docs", "inside-link.md")
	if err := os.Symlink(insideTarget, insideLink); err != nil {
		t.Fatalf("Symlink(%q -> %q) returned error: %v", insideLink, insideTarget, err)
	}

	got, err := DiscoverIndexableFiles(repoRoot)
	if err != nil {
		t.Fatalf("DiscoverIndexableFiles returned error: %v", err)
	}

	want := []string{
		"README.md",
		"config/app.yaml",
		"docs/guide.txt",
		"docs/inside-link.md",
		"src/main.go",
		"z-last.toml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoverIndexableFiles() = %#v, want %#v", got, want)
	}

	gotRepeat, err := DiscoverIndexableFiles(repoRoot)
	if err != nil {
		t.Fatalf("DiscoverIndexableFiles repeat returned error: %v", err)
	}
	if !reflect.DeepEqual(gotRepeat, want) {
		t.Fatalf("DiscoverIndexableFiles repeat = %#v, want %#v", gotRepeat, want)
	}
}

func TestDiscoverIndexableFilesRejectsNonDirectoryRoot(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", filePath, err)
	}

	_, err := DiscoverIndexableFiles(filePath)
	if err == nil {
		t.Fatalf("DiscoverIndexableFiles(%q) error = nil, want error", filePath)
	}
}

func copyDiscoveryFixture(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned ok=false")
	}

	sourceRoot := filepath.Join(filepath.Dir(currentFile), "testdata", "discovery_fixture")
	destinationRoot := filepath.Join(t.TempDir(), "repo")
	copyTree(t, sourceRoot, destinationRoot)
	return destinationRoot
}

func copyTree(t *testing.T, sourceRoot, destinationRoot string) {
	t.Helper()

	if err := filepath.Walk(sourceRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(destinationRoot, relPath)
		if info.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		return os.WriteFile(targetPath, data, 0o644)
	}); err != nil {
		t.Fatalf("copyTree(%q, %q) returned error: %v", sourceRoot, destinationRoot, err)
	}
}
