package indexing

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/embeddings"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

func TestApplyDeltaToIndexOperations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, repoRoot, "keep.md", "keep content\n")
	writeTestFile(t, repoRoot, "modify.md", "new line one\nnew line two\n")
	writeTestFile(t, repoRoot, "add.md", "added content\n")

	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		t.Fatalf("CanonicalizeRepoRoot returned error: %v", err)
	}

	baseRecords := []indexstore.ChunkRecord{
		newDeltaChunkRecord("session-1", canonicalRoot, "delete.md", 0, "old delete", "hash-delete"),
		newDeltaChunkRecord("session-1", canonicalRoot, "keep.md", 0, "keep content", "hash-keep"),
		newDeltaChunkRecord("session-1", canonicalRoot, "modify.md", 0, "old modify", "hash-modify-old"),
	}

	testCases := []struct {
		name             string
		delta            indexsync.SyncDelta
		wantPaths        []string
		wantDeletedPaths []string
		wantTouchedReads []string
		wantContents     map[string][]string
	}{
		{
			name: "add",
			delta: indexsync.SyncDelta{
				SessionID: "session-1",
				RepoRoot:  canonicalRoot,
				Changes: []indexsync.DeltaRecord{
					{Path: "add.md", Action: indexsync.DeltaActionAdd, NodeType: indexsync.NodeTypeFile},
				},
			},
			wantPaths:        []string{"add.md"},
			wantTouchedReads: []string{"add.md"},
			wantContents: map[string][]string{
				"add.md":    {"added content"},
				"delete.md": {"old delete"},
				"keep.md":   {"keep content"},
				"modify.md": {"old modify"},
			},
		},
		{
			name: "modify",
			delta: indexsync.SyncDelta{
				SessionID: "session-1",
				RepoRoot:  canonicalRoot,
				Changes: []indexsync.DeltaRecord{
					{Path: "modify.md", Action: indexsync.DeltaActionModify, NodeType: indexsync.NodeTypeFile},
				},
			},
			wantPaths:        []string{"modify.md"},
			wantTouchedReads: []string{"modify.md"},
			wantContents: map[string][]string{
				"delete.md": {"old delete"},
				"keep.md":   {"keep content"},
				"modify.md": {"new line one\nnew line two"},
			},
		},
		{
			name: "delete",
			delta: indexsync.SyncDelta{
				SessionID: "session-1",
				RepoRoot:  canonicalRoot,
				Changes: []indexsync.DeltaRecord{
					{Path: "delete.md", Action: indexsync.DeltaActionDelete, NodeType: indexsync.NodeTypeFile},
				},
			},
			wantDeletedPaths: []string{"delete.md"},
			wantContents: map[string][]string{
				"keep.md":   {"keep content"},
				"modify.md": {"old modify"},
			},
		},
		{
			name: "mixed",
			delta: indexsync.SyncDelta{
				SessionID: "session-1",
				RepoRoot:  canonicalRoot,
				Changes: []indexsync.DeltaRecord{
					{Path: "modify.md", Action: indexsync.DeltaActionModify, NodeType: indexsync.NodeTypeFile},
					{Path: "add.md", Action: indexsync.DeltaActionAdd, NodeType: indexsync.NodeTypeFile},
					{Path: "delete.md", Action: indexsync.DeltaActionDelete, NodeType: indexsync.NodeTypeFile},
				},
			},
			wantPaths:        []string{"add.md", "modify.md"},
			wantDeletedPaths: []string{"delete.md"},
			wantTouchedReads: []string{"add.md", "modify.md"},
			wantContents: map[string][]string{
				"add.md":    {"added content"},
				"keep.md":   {"keep content"},
				"modify.md": {"new line one\nnew line two"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubDeltaApplyStore{
				listed: cloneChunkRecords(baseRecords),
			}

			applier := NewDeltaApplier(store)
			applier.embedder = &stubDeltaApplyEmbedder{
				result: embeddings.Result{
					Model:      "test-embedding-model",
					Dimensions: 3,
					Vectors:    [][]float32{{1, 2, 3}, {4, 5, 6}},
				},
			}
			var reads []string
			applier.readFile = func(path string) ([]byte, error) {
				reads = append(reads, filepath.Base(path))
				return os.ReadFile(path)
			}

			result, err := applier.ApplyDeltaToIndex(context.Background(), "session-1", repoRoot, tc.delta)
			if err != nil {
				t.Fatalf("ApplyDeltaToIndex returned error: %v", err)
			}

			if !reflect.DeepEqual(result.UpsertedPaths, tc.wantPaths) {
				t.Fatalf("UpsertedPaths = %#v, want %#v", result.UpsertedPaths, tc.wantPaths)
			}
			if !reflect.DeepEqual(result.DeletedPaths, tc.wantDeletedPaths) {
				t.Fatalf("DeletedPaths = %#v, want %#v", result.DeletedPaths, tc.wantDeletedPaths)
			}

			slices.Sort(reads)
			if !reflect.DeepEqual(reads, tc.wantTouchedReads) {
				t.Fatalf("read paths = %#v, want %#v", reads, tc.wantTouchedReads)
			}

			if store.replaceCalls != 1 {
				t.Fatalf("ReplaceRepoIndex calls = %d, want 1", store.replaceCalls)
			}

			got := chunkContentsByPath(store.replaced)
			if !reflect.DeepEqual(got, tc.wantContents) {
				t.Fatalf("final chunk contents = %#v, want %#v", got, tc.wantContents)
			}
		})
	}
}

func TestApplyDeltaIdempotent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, repoRoot, "docs/guide.md", "guide\ncontent\n")

	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		t.Fatalf("CanonicalizeRepoRoot returned error: %v", err)
	}

	store := &stubDeltaApplyStore{
		listed: []indexstore.ChunkRecord{
			newDeltaChunkRecord("session-1", canonicalRoot, "stale.md", 0, "stale", "hash-stale"),
		},
	}

	delta := indexsync.SyncDelta{
		SessionID: "session-1",
		RepoRoot:  canonicalRoot,
		Changes: []indexsync.DeltaRecord{
			{Path: "docs/guide.md", Action: indexsync.DeltaActionAdd, NodeType: indexsync.NodeTypeFile},
			{Path: "stale.md", Action: indexsync.DeltaActionDelete, NodeType: indexsync.NodeTypeFile},
		},
	}

	applier := NewDeltaApplier(store)
	applier.embedder = &stubDeltaApplyEmbedder{
		result: embeddings.Result{
			Model:      "test-embedding-model",
			Dimensions: 3,
			Vectors:    [][]float32{{1, 2, 3}},
		},
	}
	first, err := applier.ApplyDeltaToIndex(context.Background(), "session-1", repoRoot, delta)
	if err != nil {
		t.Fatalf("first ApplyDeltaToIndex returned error: %v", err)
	}
	second, err := applier.ApplyDeltaToIndex(context.Background(), "session-1", repoRoot, delta)
	if err != nil {
		t.Fatalf("second ApplyDeltaToIndex returned error: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("results differ across reapply: first=%#v second=%#v", first, second)
	}
	if store.replaceCalls != 2 {
		t.Fatalf("ReplaceRepoIndex calls = %d, want 2", store.replaceCalls)
	}

	got := chunkContentsByPath(store.replaced)
	want := map[string][]string{
		"docs/guide.md": {"guide\ncontent"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("final chunk contents = %#v, want %#v", got, want)
	}
}

func TestRefreshEmbeddingsForChangedChunks_PreservesUnchangedChunks(t *testing.T) {
	t.Parallel()

	existing := []indexstore.ChunkRecord{
		{
			SessionID:     "session-1",
			RepoRoot:      "/repo",
			RelPath:       "multi.md",
			ChunkIndex:    0,
			Content:       "alpha",
			ContentHash:   chunkContentHash("multi.md", 0, "alpha"),
			Model:         "existing-model",
			EmbeddingDims: 2,
			Embedding:     []float32{0.1, 0.2},
		},
		{
			SessionID:     "session-1",
			RepoRoot:      "/repo",
			RelPath:       "multi.md",
			ChunkIndex:    1,
			Content:       "beta",
			ContentHash:   chunkContentHash("multi.md", 1, "beta"),
			Model:         "existing-model",
			EmbeddingDims: 2,
			Embedding:     []float32{0.3, 0.4},
		},
	}
	records := []indexstore.ChunkRecordInput{
		{
			SessionID:   "session-1",
			RepoRoot:    "/repo",
			RelPath:     "multi.md",
			ChunkIndex:  0,
			Content:     "alpha",
			ContentHash: chunkContentHash("multi.md", 0, "alpha"),
		},
		{
			SessionID:   "session-1",
			RepoRoot:    "/repo",
			RelPath:     "multi.md",
			ChunkIndex:  1,
			Content:     "beta updated",
			ContentHash: chunkContentHash("multi.md", 1, "beta updated"),
		},
	}
	embedder := &stubDeltaApplyEmbedder{
		result: embeddings.Result{
			Model:      "test-embedding-model",
			Dimensions: 3,
			Vectors:    [][]float32{{9, 8, 7}},
		},
	}

	refreshed, err := RefreshEmbeddingsForChangedChunks(context.Background(), existing, records, embedder)
	if err != nil {
		t.Fatalf("RefreshEmbeddingsForChangedChunks returned error: %v", err)
	}

	if !reflect.DeepEqual(embedder.calls, [][]string{{"beta updated"}}) {
		t.Fatalf("embedder calls = %#v, want only changed chunk", embedder.calls)
	}

	if len(refreshed) != 2 {
		t.Fatalf("refreshed chunk count = %d, want 2", len(refreshed))
	}

	first := refreshed[0]
	second := refreshed[1]
	if first.Model != "existing-model" || first.EmbeddingDims != 2 || !reflect.DeepEqual(first.Embedding, []float32{0.1, 0.2}) {
		t.Fatalf("unchanged chunk embedding = %#v, want preserved embedding", first)
	}
	if second.Model != "test-embedding-model" || second.EmbeddingDims != 3 || !reflect.DeepEqual(second.Embedding, []float32{9, 8, 7}) {
		t.Fatalf("changed chunk embedding = %#v, want refreshed embedding", second)
	}
}

func TestApplyDeltaToIndex_DeleteOnlySkipsEmbeddingAndRemovesChunks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		t.Fatalf("CanonicalizeRepoRoot returned error: %v", err)
	}

	store := &stubDeltaApplyStore{
		listed: []indexstore.ChunkRecord{
			newDeltaChunkRecord("session-1", canonicalRoot, "keep.md", 0, "keep", "hash-keep"),
			newDeltaChunkRecord("session-1", canonicalRoot, "delete.md", 0, "delete", "hash-delete"),
		},
	}
	embedder := &stubDeltaApplyEmbedder{}

	applier := NewDeltaApplier(store)
	applier.embedder = embedder

	_, err = applier.ApplyDeltaToIndex(context.Background(), "session-1", repoRoot, indexsync.SyncDelta{
		SessionID: "session-1",
		RepoRoot:  canonicalRoot,
		Changes: []indexsync.DeltaRecord{
			{Path: "delete.md", Action: indexsync.DeltaActionDelete, NodeType: indexsync.NodeTypeFile},
		},
	})
	if err != nil {
		t.Fatalf("ApplyDeltaToIndex returned error: %v", err)
	}

	if len(embedder.calls) != 0 {
		t.Fatalf("embedder calls = %#v, want zero calls", embedder.calls)
	}

	got := chunkContentsByPath(store.replaced)
	want := map[string][]string{"keep.md": {"keep"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("final chunk contents = %#v, want %#v", got, want)
	}
}

func TestApplyDeltaToIndex_NoChangesSkipsEmbeddingRefresh(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		t.Fatalf("CanonicalizeRepoRoot returned error: %v", err)
	}

	store := &stubDeltaApplyStore{
		listed: []indexstore.ChunkRecord{
			newDeltaChunkRecord("session-1", canonicalRoot, "keep.md", 0, "keep", "hash-keep"),
		},
	}
	embedder := &stubDeltaApplyEmbedder{}

	applier := NewDeltaApplier(store)
	applier.embedder = embedder

	result, err := applier.ApplyDeltaToIndex(context.Background(), "session-1", repoRoot, indexsync.SyncDelta{
		SessionID: "session-1",
		RepoRoot:  canonicalRoot,
	})
	if err != nil {
		t.Fatalf("ApplyDeltaToIndex returned error: %v", err)
	}

	if result.FilesTouched != 0 {
		t.Fatalf("FilesTouched = %d, want 0", result.FilesTouched)
	}
	if len(embedder.calls) != 0 {
		t.Fatalf("embedder calls = %#v, want zero calls", embedder.calls)
	}
}

type stubDeltaApplyStore struct {
	listed       []indexstore.ChunkRecord
	replaced     []indexstore.ChunkRecordInput
	replaceCalls int
}

func (s *stubDeltaApplyStore) ListRepoIndex(context.Context, string, string) ([]indexstore.ChunkRecord, error) {
	return cloneChunkRecords(s.listed), nil
}

func (s *stubDeltaApplyStore) ReplaceRepoIndex(_ context.Context, _ string, _ string, chunks []indexstore.ChunkRecordInput) error {
	s.replaceCalls++
	s.replaced = cloneChunkInputs(chunks)
	s.listed = make([]indexstore.ChunkRecord, 0, len(chunks))
	for _, chunk := range chunks {
		s.listed = append(s.listed, indexstore.ChunkRecord{
			SessionID:     chunk.SessionID,
			RepoRoot:      chunk.RepoRoot,
			RelPath:       chunk.RelPath,
			ChunkIndex:    chunk.ChunkIndex,
			Content:       chunk.Content,
			ContentHash:   chunk.ContentHash,
			Model:         chunk.Model,
			EmbeddingDims: chunk.EmbeddingDims,
			Embedding:     append([]float32(nil), chunk.Embedding...),
		})
	}
	return nil
}

func newDeltaChunkRecord(sessionID, repoRoot, relPath string, chunkIndex int, content, contentHash string) indexstore.ChunkRecord {
	return indexstore.ChunkRecord{
		SessionID:   sessionID,
		RepoRoot:    repoRoot,
		RelPath:     relPath,
		ChunkIndex:  chunkIndex,
		Content:     content,
		ContentHash: contentHash,
	}
}

func cloneChunkRecords(records []indexstore.ChunkRecord) []indexstore.ChunkRecord {
	cloned := make([]indexstore.ChunkRecord, 0, len(records))
	for _, record := range records {
		record.Embedding = append([]float32(nil), record.Embedding...)
		cloned = append(cloned, record)
	}
	return cloned
}

func cloneChunkInputs(records []indexstore.ChunkRecordInput) []indexstore.ChunkRecordInput {
	cloned := make([]indexstore.ChunkRecordInput, 0, len(records))
	for _, record := range records {
		record.Embedding = append([]float32(nil), record.Embedding...)
		cloned = append(cloned, record)
	}
	return cloned
}

func chunkContentsByPath(records []indexstore.ChunkRecordInput) map[string][]string {
	grouped := make(map[string][]string)
	for _, record := range records {
		grouped[record.RelPath] = append(grouped[record.RelPath], record.Content)
	}
	return grouped
}

func writeTestFile(t *testing.T, repoRoot, relPath, content string) {
	t.Helper()
	absPath := filepath.Join(repoRoot, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", relPath, err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", relPath, err)
	}
}

type stubDeltaApplyEmbedder struct {
	result embeddings.Result
	err    error
	calls  [][]string
}

func (s *stubDeltaApplyEmbedder) EmbedTexts(_ context.Context, texts []string) (embeddings.Result, error) {
	s.calls = append(s.calls, append([]string(nil), texts...))
	if s.err != nil {
		return embeddings.Result{}, s.err
	}
	if len(s.result.Vectors) != 0 && len(s.result.Vectors) != len(texts) {
		generated := embeddings.Result{
			Model:      s.result.Model,
			Dimensions: s.result.Dimensions,
			Vectors:    make([][]float32, len(texts)),
		}
		if generated.Model == "" {
			generated.Model = "test-embedding-model"
		}
		if generated.Dimensions <= 0 {
			generated.Dimensions = 3
		}
		for i := range generated.Vectors {
			vector := make([]float32, generated.Dimensions)
			for j := range vector {
				vector[j] = float32(i + j + 1)
			}
			generated.Vectors[i] = vector
		}
		return generated, nil
	}
	return s.result, nil
}
