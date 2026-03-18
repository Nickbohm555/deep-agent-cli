package indexing

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/embeddings"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

func TestRunFullRebuildSuccess(t *testing.T) {
	repoRoot := copyDiscoveryFixture(t)
	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		t.Fatalf("CanonicalizeRepoRoot(%q) returned error: %v", repoRoot, err)
	}

	store := &stubRebuildStore{}
	embedder := &stubRebuildEmbedder{
		result: embeddings.Result{
			Model:      "test-embedding-model",
			Dimensions: 3,
			Vectors: [][]float32{
				{1, 2, 3},
				{4, 5, 6},
			},
		},
	}

	rebuilder := NewRebuilder(store, embedder)
	rebuilder.discoverFiles = func(root string) ([]string, error) {
		if root != canonicalRoot {
			t.Fatalf("discoverFiles root = %q, want %q", root, canonicalRoot)
		}
		return []string{"docs/guide.txt", "src/main.go"}, nil
	}

	result, err := rebuilder.RunFullRebuild(context.Background(), "session-123", repoRoot)
	if err != nil {
		t.Fatalf("RunFullRebuild returned error: %v", err)
	}

	if result.FilesIndexed != 2 {
		t.Fatalf("FilesIndexed = %d, want 2", result.FilesIndexed)
	}
	if result.ChunksEmbedded != 2 {
		t.Fatalf("ChunksEmbedded = %d, want 2", result.ChunksEmbedded)
	}
	if result.Model != "test-embedding-model" {
		t.Fatalf("Model = %q, want %q", result.Model, "test-embedding-model")
	}
	if result.Dimensions != 3 {
		t.Fatalf("Dimensions = %d, want 3", result.Dimensions)
	}

	wantTexts := []string{
		"guide",
		"package main\n\nfunc main() {}",
	}
	if !reflect.DeepEqual(embedder.texts, wantTexts) {
		t.Fatalf("EmbedTexts payload = %#v, want %#v", embedder.texts, wantTexts)
	}

	if store.calls != 1 {
		t.Fatalf("ReplaceRepoIndex calls = %d, want 1", store.calls)
	}
	if store.sessionID != "session-123" {
		t.Fatalf("ReplaceRepoIndex sessionID = %q, want %q", store.sessionID, "session-123")
	}
	if store.repoRoot != canonicalRoot {
		t.Fatalf("ReplaceRepoIndex repoRoot = %q, want %q", store.repoRoot, canonicalRoot)
	}
	if len(store.chunks) != 2 {
		t.Fatalf("ReplaceRepoIndex chunk count = %d, want 2", len(store.chunks))
	}

	gotPaths := []string{store.chunks[0].RelPath, store.chunks[1].RelPath}
	wantPaths := []string{"docs/guide.txt", "src/main.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("stored rel paths = %#v, want %#v", gotPaths, wantPaths)
	}

	for i, chunk := range store.chunks {
		if chunk.SessionID != "session-123" {
			t.Fatalf("chunk %d session_id = %q, want %q", i, chunk.SessionID, "session-123")
		}
		if chunk.RepoRoot != canonicalRoot {
			t.Fatalf("chunk %d repo_root = %q, want %q", i, chunk.RepoRoot, canonicalRoot)
		}
		if chunk.Model != "test-embedding-model" {
			t.Fatalf("chunk %d model = %q, want %q", i, chunk.Model, "test-embedding-model")
		}
		if chunk.EmbeddingDims != 3 {
			t.Fatalf("chunk %d dims = %d, want 3", i, chunk.EmbeddingDims)
		}
		if len(chunk.Embedding) != 3 {
			t.Fatalf("chunk %d embedding len = %d, want 3", i, len(chunk.Embedding))
		}
	}
}

func TestRunFullRebuildAbortOnEmbedErrorWithoutReplace(t *testing.T) {
	repoRoot := copyDiscoveryFixture(t)

	store := &stubRebuildStore{}
	embedder := &stubRebuildEmbedder{
		err: errors.New("boom"),
	}

	rebuilder := NewRebuilder(store, embedder)
	rebuilder.discoverFiles = func(string) ([]string, error) {
		return []string{"docs/guide.txt"}, nil
	}

	_, err := rebuilder.RunFullRebuild(context.Background(), "session-123", repoRoot)
	if err == nil {
		t.Fatal("RunFullRebuild error = nil, want error")
	}
	if !strings.Contains(err.Error(), "embed records") {
		t.Fatalf("RunFullRebuild error = %q, want embed failure", err)
	}
	if store.calls != 0 {
		t.Fatalf("ReplaceRepoIndex calls = %d, want 0", store.calls)
	}
}

func TestRunFullRebuildAbortOnDimensionMismatchWithoutReplace(t *testing.T) {
	repoRoot := copyDiscoveryFixture(t)

	store := &stubRebuildStore{}
	embedder := &stubRebuildEmbedder{
		result: embeddings.Result{
			Model:      "test-embedding-model",
			Dimensions: 3,
			Vectors: [][]float32{
				{1, 2},
			},
		},
	}

	rebuilder := NewRebuilder(store, embedder)
	rebuilder.discoverFiles = func(string) ([]string, error) {
		return []string{"docs/guide.txt"}, nil
	}

	_, err := rebuilder.RunFullRebuild(context.Background(), "session-123", repoRoot)
	if err == nil {
		t.Fatal("RunFullRebuild error = nil, want error")
	}
	if !strings.Contains(err.Error(), "embedding dimensions mismatch") {
		t.Fatalf("RunFullRebuild error = %q, want dimension mismatch", err)
	}
	if store.calls != 0 {
		t.Fatalf("ReplaceRepoIndex calls = %d, want 0", store.calls)
	}
}

type stubRebuildEmbedder struct {
	result embeddings.Result
	err    error
	texts  []string
}

func (s *stubRebuildEmbedder) EmbedTexts(_ context.Context, texts []string) (embeddings.Result, error) {
	s.texts = append([]string(nil), texts...)
	if s.err != nil {
		return embeddings.Result{}, s.err
	}
	return s.result, nil
}

type stubRebuildStore struct {
	calls     int
	sessionID string
	repoRoot  string
	chunks    []indexstore.ChunkRecordInput
}

func (s *stubRebuildStore) ReplaceRepoIndex(_ context.Context, sessionID, repoRoot string, chunks []indexstore.ChunkRecordInput) error {
	s.calls++
	s.sessionID = sessionID
	s.repoRoot = repoRoot
	s.chunks = append([]indexstore.ChunkRecordInput(nil), chunks...)
	return nil
}
