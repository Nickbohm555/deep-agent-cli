package snapshot

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
)

func TestNormalizeSnapshotPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "normalizes windows separators", input: `docs\guide.txt`, want: "docs/guide.txt"},
		{name: "cleans current dir", input: "./docs/guide.txt", want: "docs/guide.txt"},
		{name: "rejects absolute", input: filepath.Join(string(filepath.Separator), "tmp", "file.md"), wantErr: true},
		{name: "rejects escaping", input: "../secret.md", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeSnapshotPath(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeSnapshotPath(%q) error = nil, want error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeSnapshotPath(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeSnapshotPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestShouldIndexPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: "src/main.go", want: true},
		{path: "docs/guide.txt", want: true},
		{path: "docs/.hidden.md", want: false},
		{path: ".git/config", want: false},
		{path: "dist/generated.md", want: false},
		{path: "vendor/lib.go", want: false},
		{path: "src/app.bin", want: false},
	}

	for _, tc := range tests {
		if got := ShouldIndexPath(tc.path); got != tc.want {
			t.Fatalf("ShouldIndexPath(%q) = %t, want %t", tc.path, got, tc.want)
		}
	}
}

func TestBuildSnapshotDeterministic(t *testing.T) {
	t.Parallel()

	repoRootA := copyDiscoveryFixture(t)
	repoRootB := copyDiscoveryFixture(t)

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "escape.md")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", outsideFile, err)
	}

	insideTargetA := filepath.Join(repoRootA, "README.md")
	insideLinkA := filepath.Join(repoRootA, "docs", "inside-link.md")
	if err := os.Symlink(insideTargetA, insideLinkA); err != nil {
		t.Fatalf("Symlink(%q -> %q) returned error: %v", insideLinkA, insideTargetA, err)
	}
	escapeLinkA := filepath.Join(repoRootA, "docs", "escape-link.md")
	if err := os.Symlink(outsideFile, escapeLinkA); err != nil {
		t.Fatalf("Symlink(%q -> %q) returned error: %v", escapeLinkA, outsideFile, err)
	}

	insideTargetB := filepath.Join(repoRootB, "README.md")
	insideLinkB := filepath.Join(repoRootB, "docs", "inside-link.md")
	if err := os.Symlink(insideTargetB, insideLinkB); err != nil {
		t.Fatalf("Symlink(%q -> %q) returned error: %v", insideLinkB, insideTargetB, err)
	}
	escapeLinkB := filepath.Join(repoRootB, "docs", "escape-link.md")
	if err := os.Symlink(outsideFile, escapeLinkB); err != nil {
		t.Fatalf("Symlink(%q -> %q) returned error: %v", escapeLinkB, outsideFile, err)
	}

	snapshotA, err := BuildSnapshot(repoRootA)
	if err != nil {
		t.Fatalf("BuildSnapshot(%q) returned error: %v", repoRootA, err)
	}
	snapshotB, err := BuildSnapshot(repoRootB)
	if err != nil {
		t.Fatalf("BuildSnapshot(%q) returned error: %v", repoRootB, err)
	}

	wantOrderedPaths := []string{
		"README.md",
		"config",
		"config/app.yaml",
		"docs",
		"docs/guide.txt",
		"docs/inside-link.md",
		"src",
		"src/main.go",
		"z-last.toml",
	}
	if got := snapshotA.OrderedPaths(); !reflect.DeepEqual(got, wantOrderedPaths) {
		t.Fatalf("snapshotA.OrderedPaths() = %#v, want %#v", got, wantOrderedPaths)
	}
	if got := snapshotB.OrderedPaths(); !reflect.DeepEqual(got, wantOrderedPaths) {
		t.Fatalf("snapshotB.OrderedPaths() = %#v, want %#v", got, wantOrderedPaths)
	}

	if !reflect.DeepEqual(snapshotEntriesComparable(snapshotA.Entries), snapshotEntriesComparable(snapshotB.Entries)) {
		t.Fatalf("BuildSnapshot entries differ between identical fixtures:\nA=%#v\nB=%#v", snapshotEntriesComparable(snapshotA.Entries), snapshotEntriesComparable(snapshotB.Entries))
	}
	if snapshotA.RootHash == "" {
		t.Fatal("snapshotA.RootHash is empty")
	}
	if snapshotB.RootHash == "" {
		t.Fatal("snapshotB.RootHash is empty")
	}
	if snapshotA.RootHash != snapshotB.RootHash {
		t.Fatalf("root hash mismatch for identical fixtures: %q vs %q", snapshotA.RootHash, snapshotB.RootHash)
	}

	repeatSnapshot, err := BuildSnapshot(repoRootA)
	if err != nil {
		t.Fatalf("BuildSnapshot(%q) repeat returned error: %v", repoRootA, err)
	}
	if snapshotA.RootHash != repeatSnapshot.RootHash {
		t.Fatalf("repeat root hash mismatch: first=%q repeat=%q", snapshotA.RootHash, repeatSnapshot.RootHash)
	}
}

func TestBuildSnapshotRootHashChangeDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(t *testing.T, repoRoot string)
		wantChange bool
	}{
		{
			name: "ignored file add does not change root hash",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(repoRoot, "dist", "new-generated.md"), "ignored output\n")
			},
			wantChange: false,
		},
		{
			name: "ignored file delete does not change root hash",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				path := filepath.Join(repoRoot, "dist", "generated.md")
				if err := os.Remove(path); err != nil {
					t.Fatalf("Remove(%q) returned error: %v", path, err)
				}
			},
			wantChange: false,
		},
		{
			name: "indexable file add changes root hash",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(repoRoot, "docs", "new.md"), "new doc\n")
			},
			wantChange: true,
		},
		{
			name: "indexable file modify changes root hash",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(repoRoot, "docs", "guide.txt"), "updated guide\n")
			},
			wantChange: true,
		},
		{
			name: "indexable file delete changes root hash",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				path := filepath.Join(repoRoot, "README.md")
				if err := os.Remove(path); err != nil {
					t.Fatalf("Remove(%q) returned error: %v", path, err)
				}
			},
			wantChange: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			baselineRepo := copyDiscoveryFixture(t)
			mutatedRepo := copyDiscoveryFixture(t)

			baselineSnapshot, err := BuildSnapshot(baselineRepo)
			if err != nil {
				t.Fatalf("BuildSnapshot(%q) baseline returned error: %v", baselineRepo, err)
			}

			tc.mutate(t, mutatedRepo)

			mutatedSnapshot, err := BuildSnapshot(mutatedRepo)
			if err != nil {
				t.Fatalf("BuildSnapshot(%q) mutated returned error: %v", mutatedRepo, err)
			}

			gotChange := baselineSnapshot.RootHash != mutatedSnapshot.RootHash
			if gotChange != tc.wantChange {
				t.Fatalf("root hash change = %t, want %t (baseline=%q mutated=%q)", gotChange, tc.wantChange, baselineSnapshot.RootHash, mutatedSnapshot.RootHash)
			}
		})
	}
}

func TestHashNodeTreeIgnoresEntryOrder(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(repoRoot, "docs", "guide.txt"), "guide\n")
	mustWriteFile(t, filepath.Join(repoRoot, "src", "main.go"), "package main\n")

	sizeGuide := int64(len("guide\n"))
	sizeMain := int64(len("package main\n"))
	mtime := int64(10)

	build := func(entries []Entry) string {
		t.Helper()

		root, err := BuildNodeTree(entries)
		if err != nil {
			t.Fatalf("BuildNodeTree returned error: %v", err)
		}

		hash, err := HashNodeTree(repoRoot, root)
		if err != nil {
			t.Fatalf("HashNodeTree returned error: %v", err)
		}
		return hash
	}

	hashA := build([]Entry{
		{Path: "src", NodeType: indexsync.NodeTypeDir},
		{Path: "docs/guide.txt", NodeType: indexsync.NodeTypeFile, SizeBytes: &sizeGuide, MTimeNS: &mtime},
		{Path: "docs", NodeType: indexsync.NodeTypeDir},
		{Path: "src/main.go", NodeType: indexsync.NodeTypeFile, SizeBytes: &sizeMain, MTimeNS: &mtime},
	})
	hashB := build([]Entry{
		{Path: "src/main.go", NodeType: indexsync.NodeTypeFile, SizeBytes: &sizeMain, MTimeNS: &mtime},
		{Path: "docs", NodeType: indexsync.NodeTypeDir},
		{Path: "src", NodeType: indexsync.NodeTypeDir},
		{Path: "docs/guide.txt", NodeType: indexsync.NodeTypeFile, SizeBytes: &sizeGuide, MTimeNS: &mtime},
	})

	if hashA != hashB {
		t.Fatalf("HashNodeTree root hash depends on input order: %q vs %q", hashA, hashB)
	}
}

func TestBuildNodeTreeSortsSiblingsLexicographically(t *testing.T) {
	t.Parallel()

	size := int64(1)
	mtime := int64(2)
	root, err := BuildNodeTree([]Entry{
		{Path: "docs/zeta.md", NodeType: indexsync.NodeTypeFile, SizeBytes: &size, MTimeNS: &mtime},
		{Path: "docs", NodeType: indexsync.NodeTypeDir},
		{Path: "docs/alpha.md", NodeType: indexsync.NodeTypeFile, SizeBytes: &size, MTimeNS: &mtime},
		{Path: "config", NodeType: indexsync.NodeTypeDir},
		{Path: "config/app.yaml", NodeType: indexsync.NodeTypeFile, SizeBytes: &size, MTimeNS: &mtime},
	})
	if err != nil {
		t.Fatalf("BuildNodeTree returned error: %v", err)
	}

	got := orderedPathsFromRoot(root)
	want := []string{
		"config",
		"config/app.yaml",
		"docs",
		"docs/alpha.md",
		"docs/zeta.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderedPathsFromRoot() = %#v, want %#v", got, want)
	}
}

func snapshotEntriesComparable(entries []Entry) []Entry {
	normalized := make([]Entry, len(entries))
	copy(normalized, entries)
	for i := range normalized {
		normalized[i].SizeBytes = nil
		normalized[i].MTimeNS = nil
	}
	return normalized
}

func orderedPathsFromRoot(root *Node) []string {
	paths := make([]string, 0)
	var walk func(node *Node)
	walk = func(node *Node) {
		for _, child := range node.Children {
			paths = append(paths, child.Entry.Path)
			walk(child)
		}
	}
	walk(root)
	return paths
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", path, err)
	}
}

func copyDiscoveryFixture(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned ok=false")
	}

	sourceRoot := filepath.Join(filepath.Dir(currentFile), "..", "..", "indexing", "testdata", "discovery_fixture")
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
