package diff

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync/snapshot"
)

func TestDiffSnapshots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(t *testing.T, repoRoot string)
		want   []FileDelta
	}{
		{
			name: "unchanged tree returns empty delta",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
			},
			want: nil,
		},
		{
			name: "single file modify returns modify delta",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				writeFile(t, filepath.Join(repoRoot, "docs", "guide.txt"), "updated guide\n")
			},
			want: []FileDelta{
				{Op: DeltaOpModify, Path: "docs/guide.txt"},
			},
		},
		{
			name: "subtree add returns file add deltas",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				writeFile(t, filepath.Join(repoRoot, "docs", "api", "overview.md"), "overview\n")
				writeFile(t, filepath.Join(repoRoot, "docs", "api", "reference.md"), "reference\n")
			},
			want: []FileDelta{
				{Op: DeltaOpAdd, Path: "docs/api/overview.md"},
				{Op: DeltaOpAdd, Path: "docs/api/reference.md"},
			},
		},
		{
			name: "subtree delete returns file delete deltas",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				removeAll(t, filepath.Join(repoRoot, "docs"))
			},
			want: []FileDelta{
				{Op: DeltaOpDelete, Path: "docs/guide.txt"},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			beforeRoot := createRepoFixture(t)
			afterRoot := createRepoFixture(t)
			tc.mutate(t, afterRoot)

			beforeSnapshot, err := snapshot.BuildSnapshot(beforeRoot)
			if err != nil {
				t.Fatalf("BuildSnapshot(before) returned error: %v", err)
			}
			afterSnapshot, err := snapshot.BuildSnapshot(afterRoot)
			if err != nil {
				t.Fatalf("BuildSnapshot(after) returned error: %v", err)
			}

			got, err := DiffSnapshots(beforeSnapshot, afterSnapshot)
			if err != nil {
				t.Fatalf("DiffSnapshots returned error: %v", err)
			}

			if len(got.Changes) != len(tc.want) {
				t.Fatalf("delta length = %d, want %d; got=%#v", len(got.Changes), len(tc.want), got.Changes)
			}

			for i := range got.Changes {
				if got.Changes[i].Op != tc.want[i].Op || got.Changes[i].Path != tc.want[i].Path {
					t.Fatalf("delta[%d] = %#v, want op=%q path=%q", i, got.Changes[i], tc.want[i].Op, tc.want[i].Path)
				}
			}

			if tc.want == nil && !reflect.DeepEqual(got.Changes, tc.want) {
				t.Fatalf("changes = %#v, want nil", got.Changes)
			}

			for _, change := range got.Changes {
				switch change.Op {
				case DeltaOpAdd:
					if change.CurrentNodeHash == "" || change.CurrentContentHash == "" {
						t.Fatalf("add delta missing current hashes: %#v", change)
					}
				case DeltaOpModify:
					if change.PreviousNodeHash == "" || change.CurrentNodeHash == "" {
						t.Fatalf("modify delta missing node hashes: %#v", change)
					}
					if change.PreviousContentHash == "" || change.CurrentContentHash == "" {
						t.Fatalf("modify delta missing content hashes: %#v", change)
					}
				case DeltaOpDelete:
					if change.PreviousNodeHash == "" || change.PreviousContentHash == "" {
						t.Fatalf("delete delta missing previous hashes: %#v", change)
					}
				}
			}
		})
	}
}

func TestMaterializeChangedFiles_NoChangeProducesEmptyMaterialization(t *testing.T) {
	t.Parallel()

	beforeRoot := createRepoFixture(t)
	afterRoot := createRepoFixture(t)

	beforeSnapshot, err := snapshot.BuildSnapshot(beforeRoot)
	if err != nil {
		t.Fatalf("BuildSnapshot(before) returned error: %v", err)
	}
	afterSnapshot, err := snapshot.BuildSnapshot(afterRoot)
	if err != nil {
		t.Fatalf("BuildSnapshot(after) returned error: %v", err)
	}

	delta, err := DiffSnapshots(beforeSnapshot, afterSnapshot)
	if err != nil {
		t.Fatalf("DiffSnapshots returned error: %v", err)
	}

	got := MaterializeChangedFiles(delta)
	if len(got.FilesToUpsert) != 0 || len(got.FilesToRemove) != 0 {
		t.Fatalf("MaterializeChangedFiles() = %#v, want empty result", got)
	}
}

func TestMaterializeChangedFiles_IsStableAndDeduped(t *testing.T) {
	t.Parallel()

	delta := SyncDeltaSet{
		Changes: []FileDelta{
			{Op: DeltaOpModify, Path: "src/main.go"},
			{Op: DeltaOpDelete, Path: "docs/old.md"},
			{Op: DeltaOpAdd, Path: "docs/new.md"},
			{Op: DeltaOpModify, Path: "src/main.go"},
			{Op: DeltaOpDelete, Path: "docs/old.md"},
			{Op: DeltaOpAdd, Path: "docs/new.md"},
			{Op: DeltaOpModify, Path: " README.md "},
			{Op: DeltaOpDelete, Path: ""},
		},
	}

	want := ChangedFiles{
		FilesToUpsert: []string{"README.md", "docs/new.md", "src/main.go"},
		FilesToRemove: []string{"docs/old.md"},
	}

	first := MaterializeChangedFiles(delta)
	second := MaterializeChangedFiles(delta)

	if !reflect.DeepEqual(first, want) {
		t.Fatalf("first materialization = %#v, want %#v", first, want)
	}
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("second materialization = %#v, want %#v", second, want)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("materialization not stable: first=%#v second=%#v", first, second)
	}
}

func TestDiffSnapshots_ComplexScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupBefore func(t *testing.T, repoRoot string)
		mutateAfter func(t *testing.T, repoRoot string)
		want        []FileDelta
	}{
		{
			name: "nested directory mutations are detected recursively",
			setupBefore: func(t *testing.T, repoRoot string) {
				t.Helper()
				writeFile(t, filepath.Join(repoRoot, "src", "pkg", "util", "helpers.go"), "package util\n\nfunc Helper() string { return \"v1\" }\n")
				writeFile(t, filepath.Join(repoRoot, "src", "pkg", "util", "deep", "config.json"), "{\n  \"enabled\": true\n}\n")
			},
			mutateAfter: func(t *testing.T, repoRoot string) {
				t.Helper()
				writeFile(t, filepath.Join(repoRoot, "src", "pkg", "util", "helpers.go"), "package util\n\nfunc Helper() string { return \"v2\" }\n")
				removeAll(t, filepath.Join(repoRoot, "src", "pkg", "util", "deep"))
				writeFile(t, filepath.Join(repoRoot, "src", "pkg", "util", "deep", "settings.yaml"), "enabled: false\n")
				writeFile(t, filepath.Join(repoRoot, "src", "pkg", "util", "new.go"), "package util\n")
			},
			want: []FileDelta{
				{Op: DeltaOpDelete, Path: "src/pkg/util/deep/config.json"},
				{Op: DeltaOpAdd, Path: "src/pkg/util/deep/settings.yaml"},
				{Op: DeltaOpModify, Path: "src/pkg/util/helpers.go"},
				{Op: DeltaOpAdd, Path: "src/pkg/util/new.go"},
			},
		},
		{
			name: "mixed add modify delete changes are emitted in one pass",
			setupBefore: func(t *testing.T, repoRoot string) {
				t.Helper()
				writeFile(t, filepath.Join(repoRoot, "notes", "todo.md"), "- item 1\n")
			},
			mutateAfter: func(t *testing.T, repoRoot string) {
				t.Helper()
				writeFile(t, filepath.Join(repoRoot, "src", "main.go"), "package main\n\nfunc main() {}\n")
				removeAll(t, filepath.Join(repoRoot, "docs", "guide.txt"))
				writeFile(t, filepath.Join(repoRoot, "notes", "todo.md"), "- item 1\n- item 2\n")
				writeFile(t, filepath.Join(repoRoot, "docs", "faq.md"), "# FAQ\n")
			},
			want: []FileDelta{
				{Op: DeltaOpAdd, Path: "docs/faq.md"},
				{Op: DeltaOpDelete, Path: "docs/guide.txt"},
				{Op: DeltaOpModify, Path: "notes/todo.md"},
				{Op: DeltaOpModify, Path: "src/main.go"},
			},
		},
		{
			name: "metadata-only mtime change does not create a false positive",
			setupBefore: func(t *testing.T, repoRoot string) {
				t.Helper()
				writeFile(t, filepath.Join(repoRoot, "notes", "todo.md"), "- item 1\n")
			},
			mutateAfter: func(t *testing.T, repoRoot string) {
				t.Helper()
				path := filepath.Join(repoRoot, "notes", "todo.md")
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("Stat(%q) returned error: %v", path, err)
				}
				newMTime := info.ModTime().Add(2 * time.Hour)
				if err := os.Chtimes(path, newMTime, newMTime); err != nil {
					t.Fatalf("Chtimes(%q) returned error: %v", path, err)
				}
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			beforeRoot := createRepoFixture(t)
			afterRoot := createRepoFixture(t)

			if tc.setupBefore != nil {
				tc.setupBefore(t, beforeRoot)
				tc.setupBefore(t, afterRoot)
			}
			if tc.mutateAfter != nil {
				tc.mutateAfter(t, afterRoot)
			}

			beforeSnapshot, err := snapshot.BuildSnapshot(beforeRoot)
			if err != nil {
				t.Fatalf("BuildSnapshot(before) returned error: %v", err)
			}
			afterSnapshot, err := snapshot.BuildSnapshot(afterRoot)
			if err != nil {
				t.Fatalf("BuildSnapshot(after) returned error: %v", err)
			}

			got, err := DiffSnapshots(beforeSnapshot, afterSnapshot)
			if err != nil {
				t.Fatalf("DiffSnapshots returned error: %v", err)
			}

			assertDeltaOpsAndPaths(t, got.Changes, tc.want)
		})
	}
}

func assertDeltaOpsAndPaths(t *testing.T, got, want []FileDelta) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("delta length = %d, want %d; got=%#v", len(got), len(want), got)
	}

	for i := range got {
		if got[i].Op != want[i].Op || got[i].Path != want[i].Path {
			t.Fatalf("delta[%d] = %#v, want op=%q path=%q", i, got[i], want[i].Op, want[i].Path)
		}
	}

	if want == nil && !reflect.DeepEqual(got, want) {
		t.Fatalf("changes = %#v, want nil", got)
	}
}

func createRepoFixture(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "docs", "guide.txt"), "guide\n")
	writeFile(t, filepath.Join(repoRoot, "notes", "todo.md"), "- item 1\n")
	writeFile(t, filepath.Join(repoRoot, "src", "main.go"), "package main\n")
	return repoRoot
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", path, err)
	}
}

func removeAll(t *testing.T, path string) {
	t.Helper()

	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll(%q) returned error: %v", path, err)
	}
}
