package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Nickbohm555/deep-agent-cli/internal/embeddings"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexing"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

func TestIndexRepoRequiresBoundSessionScope(t *testing.T) {
	t.Parallel()

	ctx, err := runtime.WithRepoRoot(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}

	_, err = IndexRepo(ctx, toolCall(t, "index_repo", IndexRepoInput{}))
	if err == nil {
		t.Fatal("IndexRepo returned nil error without session scope")
	}
	if err.Error() != "tool execution requires a bound session ID" {
		t.Fatalf("IndexRepo error = %q, want missing session scope", err)
	}
}

func TestInspectIndexRequiresBoundSessionScope(t *testing.T) {
	t.Parallel()

	ctx, err := runtime.WithRepoRoot(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}

	_, err = InspectIndex(ctx, toolCall(t, "inspect_index", InspectIndexInput{}))
	if err == nil {
		t.Fatal("InspectIndex returned nil error without session scope")
	}
	if err.Error() != "tool execution requires a bound session ID" {
		t.Fatalf("InspectIndex error = %q, want missing session scope", err)
	}
}

func TestIndexRepoReturnsRebuildStats(t *testing.T) {
	restorePool := newIndexRepoPool
	restoreStore := newIndexRepoStore
	restoreEmbedder := newIndexRepoEmbedder
	restoreClose := closeIndexRepoPool
	restoreRunner := runIndexRepo
	t.Cleanup(func() {
		newIndexRepoPool = restorePool
		newIndexRepoStore = restoreStore
		newIndexRepoEmbedder = restoreEmbedder
		closeIndexRepoPool = restoreClose
		runIndexRepo = restoreRunner
	})

	newIndexRepoPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	newIndexRepoStore = func(*pgxpool.Pool) indexRepoStore {
		return stubIndexRepoStore{}
	}
	newIndexRepoEmbedder = func() indexRepoEmbedder {
		return stubIndexRepoEmbedder{}
	}
	closeIndexRepoPool = func(*pgxpool.Pool) {}

	var gotSessionID string
	var gotRepoRoot string
	runIndexRepo = func(_ context.Context, _ indexRepoStore, _ indexRepoEmbedder, sessionID, repoRoot string) (indexing.FullRebuildResult, error) {
		gotSessionID = sessionID
		gotRepoRoot = repoRoot
		return indexing.FullRebuildResult{
			FilesIndexed:   3,
			ChunksEmbedded: 7,
			Model:          "text-embedding-3-small",
			Dimensions:     1536,
		}, nil
	}

	ctx := mustBindSessionScope(t, "session-123", t.TempDir())
	result, err := IndexRepo(ctx, toolCall(t, "index_repo", IndexRepoInput{}))
	if err != nil {
		t.Fatalf("IndexRepo returned error: %v", err)
	}
	if result.IsError {
		t.Fatal("IndexRepo unexpectedly marked result as error")
	}

	if gotSessionID != "session-123" {
		t.Fatalf("sessionID = %q, want session-123", gotSessionID)
	}
	repoRoot, err := runtime.RepoRootFromContext(ctx)
	if err != nil {
		t.Fatalf("RepoRootFromContext returned error: %v", err)
	}
	if gotRepoRoot != repoRoot {
		t.Fatalf("repoRoot = %q, want %q", gotRepoRoot, repoRoot)
	}

	var output struct {
		FilesIndexed   int    `json:"files_indexed"`
		ChunksEmbedded int    `json:"chunks_embedded"`
		Model          string `json:"model"`
		Dims           int    `json:"dims"`
	}
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatalf("IndexRepo output is not valid JSON: %v", err)
	}
	if output.FilesIndexed != 3 || output.ChunksEmbedded != 7 || output.Model != "text-embedding-3-small" || output.Dims != 1536 {
		t.Fatalf("IndexRepo output = %+v, want expected rebuild stats", output)
	}
}

func TestInspectIndexReturnsScopedRowsAndHonorsLimit(t *testing.T) {
	restorePool := newInspectIndexPool
	restoreStore := newInspectIndexStore
	restoreClose := closeInspectIndexPool
	t.Cleanup(func() {
		newInspectIndexPool = restorePool
		newInspectIndexStore = restoreStore
		closeInspectIndexPool = restoreClose
	})

	newInspectIndexPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	closeInspectIndexPool = func(*pgxpool.Pool) {}

	var gotSessionID string
	var gotRepoRoot string
	newInspectIndexStore = func(*pgxpool.Pool) inspectIndexStore {
		return inspectIndexStoreStub{
			listFn: func(_ context.Context, sessionID, repoRoot string) ([]indexstore.ChunkRecord, error) {
				gotSessionID = sessionID
				gotRepoRoot = repoRoot
				return []indexstore.ChunkRecord{
					{RepoRoot: repoRoot, RelPath: "a.go", ChunkIndex: 0, Model: "m", EmbeddingDims: 3, ContentHash: "h1"},
					{RepoRoot: repoRoot, RelPath: "b.go", ChunkIndex: 1, Model: "m", EmbeddingDims: 3, ContentHash: "h2"},
				}, nil
			},
		}
	}

	ctx := mustBindSessionScope(t, "session-456", t.TempDir())
	result, err := InspectIndex(ctx, toolCall(t, "inspect_index", InspectIndexInput{Limit: 1}))
	if err != nil {
		t.Fatalf("InspectIndex returned error: %v", err)
	}
	if result.IsError {
		t.Fatal("InspectIndex unexpectedly marked result as error")
	}

	if gotSessionID != "session-456" {
		t.Fatalf("sessionID = %q, want session-456", gotSessionID)
	}
	repoRoot, err := runtime.RepoRootFromContext(ctx)
	if err != nil {
		t.Fatalf("RepoRootFromContext returned error: %v", err)
	}
	if gotRepoRoot != repoRoot {
		t.Fatalf("repoRoot = %q, want %q", gotRepoRoot, repoRoot)
	}

	var rows []struct {
		RepoRoot      string `json:"repo_root"`
		RelPath       string `json:"rel_path"`
		ChunkIndex    int    `json:"chunk_index"`
		Model         string `json:"model"`
		EmbeddingDims int    `json:"embedding_dims"`
		ContentHash   string `json:"content_hash"`
	}
	if err := json.Unmarshal([]byte(result.Output), &rows); err != nil {
		t.Fatalf("InspectIndex output is not valid JSON: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("InspectIndex returned %d rows, want 1", len(rows))
	}
	if rows[0].RepoRoot != repoRoot || rows[0].RelPath != "a.go" || rows[0].ChunkIndex != 0 || rows[0].EmbeddingDims != 3 || rows[0].ContentHash != "h1" {
		t.Fatalf("InspectIndex first row = %+v, want scoped metadata", rows[0])
	}
}

func TestInspectIndexRejectsNegativeLimit(t *testing.T) {
	t.Parallel()

	ctx := mustBindSessionScope(t, "session-789", t.TempDir())
	_, err := InspectIndex(ctx, toolCall(t, "inspect_index", InspectIndexInput{Limit: -1}))
	if err == nil {
		t.Fatal("InspectIndex returned nil error for negative limit")
	}
	if err.Error() != "limit must be greater than or equal to 0" {
		t.Fatalf("InspectIndex error = %q, want negative limit validation", err)
	}
}

func TestIndexRepoSurfacesRebuildFailures(t *testing.T) {
	restorePool := newIndexRepoPool
	restoreStore := newIndexRepoStore
	restoreEmbedder := newIndexRepoEmbedder
	restoreClose := closeIndexRepoPool
	restoreRunner := runIndexRepo
	t.Cleanup(func() {
		newIndexRepoPool = restorePool
		newIndexRepoStore = restoreStore
		newIndexRepoEmbedder = restoreEmbedder
		closeIndexRepoPool = restoreClose
		runIndexRepo = restoreRunner
	})

	newIndexRepoPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	newIndexRepoStore = func(*pgxpool.Pool) indexRepoStore {
		return stubIndexRepoStore{}
	}
	newIndexRepoEmbedder = func() indexRepoEmbedder {
		return stubIndexRepoEmbedder{}
	}
	closeIndexRepoPool = func(*pgxpool.Pool) {}
	runIndexRepo = func(context.Context, indexRepoStore, indexRepoEmbedder, string, string) (indexing.FullRebuildResult, error) {
		return indexing.FullRebuildResult{}, errors.New("rebuild failed")
	}

	ctx := mustBindSessionScope(t, "session-123", t.TempDir())
	_, err := IndexRepo(ctx, toolCall(t, "index_repo", IndexRepoInput{}))
	if err == nil {
		t.Fatal("IndexRepo returned nil error on rebuild failure")
	}
	if err.Error() != "rebuild failed" {
		t.Fatalf("IndexRepo error = %q, want rebuild failure", err)
	}
}

func TestIndexRepoAndInspectIndexAcceptanceFlowIsScopedToBoundRepo(t *testing.T) {
	restoreIndexPool := newIndexRepoPool
	restoreIndexStore := newIndexRepoStore
	restoreIndexEmbedder := newIndexRepoEmbedder
	restoreIndexClose := closeIndexRepoPool
	restoreInspectPool := newInspectIndexPool
	restoreInspectStore := newInspectIndexStore
	restoreInspectClose := closeInspectIndexPool
	t.Cleanup(func() {
		newIndexRepoPool = restoreIndexPool
		newIndexRepoStore = restoreIndexStore
		newIndexRepoEmbedder = restoreIndexEmbedder
		closeIndexRepoPool = restoreIndexClose
		newInspectIndexPool = restoreInspectPool
		newInspectIndexStore = restoreInspectStore
		closeInspectIndexPool = restoreInspectClose
	})

	newIndexRepoPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	newInspectIndexPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	closeIndexRepoPool = func(*pgxpool.Pool) {}
	closeInspectIndexPool = func(*pgxpool.Pool) {}

	store := newScopedIndexStore()
	newIndexRepoStore = func(*pgxpool.Pool) indexRepoStore {
		return store
	}
	newInspectIndexStore = func(*pgxpool.Pool) inspectIndexStore {
		return store
	}
	newIndexRepoEmbedder = func() indexRepoEmbedder {
		return acceptanceEmbedder{}
	}

	repoOne := t.TempDir()
	writeAcceptanceRepoFile(t, repoOne, "docs/guide.md", "# Guide\nline one\nline two\n")
	writeAcceptanceRepoFile(t, repoOne, "src/main.go", "package main\n\nfunc main() {}\n")

	repoTwo := t.TempDir()
	writeAcceptanceRepoFile(t, repoTwo, "notes/todo.md", "other repo\n")

	ctxOne := mustBindSessionScope(t, "session-one", repoOne)
	indexResult, err := IndexRepo(ctxOne, toolCall(t, "index_repo", IndexRepoInput{}))
	if err != nil {
		t.Fatalf("IndexRepo returned error: %v", err)
	}
	if indexResult.IsError {
		t.Fatal("IndexRepo unexpectedly marked result as error")
	}

	var indexOutput struct {
		FilesIndexed   int    `json:"files_indexed"`
		ChunksEmbedded int    `json:"chunks_embedded"`
		Model          string `json:"model"`
		Dims           int    `json:"dims"`
	}
	if err := json.Unmarshal([]byte(indexResult.Output), &indexOutput); err != nil {
		t.Fatalf("IndexRepo output is not valid JSON: %v", err)
	}
	if indexOutput.FilesIndexed != 2 {
		t.Fatalf("files_indexed = %d, want 2", indexOutput.FilesIndexed)
	}
	if indexOutput.ChunksEmbedded != 2 {
		t.Fatalf("chunks_embedded = %d, want 2", indexOutput.ChunksEmbedded)
	}
	if indexOutput.Model != "acceptance-embedding-model" || indexOutput.Dims != 3 {
		t.Fatalf("IndexRepo output = %+v, want acceptance embedding metadata", indexOutput)
	}

	otherCtx := mustBindSessionScope(t, "session-two", repoTwo)
	if _, err := IndexRepo(otherCtx, toolCall(t, "index_repo", IndexRepoInput{})); err != nil {
		t.Fatalf("IndexRepo for second repo returned error: %v", err)
	}

	inspectResult, err := InspectIndex(ctxOne, toolCall(t, "inspect_index", InspectIndexInput{}))
	if err != nil {
		t.Fatalf("InspectIndex returned error: %v", err)
	}
	if inspectResult.IsError {
		t.Fatal("InspectIndex unexpectedly marked result as error")
	}

	var rows []struct {
		RepoRoot      string `json:"repo_root"`
		RelPath       string `json:"rel_path"`
		ChunkIndex    int    `json:"chunk_index"`
		Model         string `json:"model"`
		EmbeddingDims int    `json:"embedding_dims"`
		ContentHash   string `json:"content_hash"`
	}
	if err := json.Unmarshal([]byte(inspectResult.Output), &rows); err != nil {
		t.Fatalf("InspectIndex output is not valid JSON: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("InspectIndex returned %d rows, want 2", len(rows))
	}

	repoOneRoot, err := runtime.RepoRootFromContext(ctxOne)
	if err != nil {
		t.Fatalf("RepoRootFromContext returned error: %v", err)
	}
	wantPaths := []string{"docs/guide.md", "src/main.go"}
	gotPaths := []string{rows[0].RelPath, rows[1].RelPath}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("inspect rel paths = %#v, want %#v", gotPaths, wantPaths)
	}
	for i, row := range rows {
		if row.RepoRoot != repoOneRoot {
			t.Fatalf("row %d repo_root = %q, want %q", i, row.RepoRoot, repoOneRoot)
		}
		if row.Model != "acceptance-embedding-model" {
			t.Fatalf("row %d model = %q, want acceptance-embedding-model", i, row.Model)
		}
		if row.EmbeddingDims != 3 {
			t.Fatalf("row %d embedding_dims = %d, want 3", i, row.EmbeddingDims)
		}
		if row.ContentHash == "" {
			t.Fatalf("row %d content_hash is empty", i)
		}
		if row.ChunkIndex != 0 {
			t.Fatalf("row %d chunk_index = %d, want 0", i, row.ChunkIndex)
		}
	}

	otherInspectResult, err := InspectIndex(otherCtx, toolCall(t, "inspect_index", InspectIndexInput{}))
	if err != nil {
		t.Fatalf("InspectIndex for second repo returned error: %v", err)
	}

	var otherRows []struct {
		RepoRoot string `json:"repo_root"`
		RelPath  string `json:"rel_path"`
	}
	if err := json.Unmarshal([]byte(otherInspectResult.Output), &otherRows); err != nil {
		t.Fatalf("InspectIndex second output is not valid JSON: %v", err)
	}
	if len(otherRows) != 1 {
		t.Fatalf("second InspectIndex returned %d rows, want 1", len(otherRows))
	}
	if otherRows[0].RelPath != "notes/todo.md" {
		t.Fatalf("second InspectIndex rel_path = %q, want notes/todo.md", otherRows[0].RelPath)
	}
	for _, row := range otherRows {
		if row.RepoRoot == repoOneRoot {
			t.Fatalf("unexpected cross-repo row returned for second scope: %+v", row)
		}
	}
}

func mustBindSessionScope(t *testing.T, sessionID, repoRoot string) context.Context {
	t.Helper()

	ctx, err := runtime.WithRepoRoot(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}

	return runtime.WithSessionID(ctx, sessionID)
}

type stubIndexRepoStore struct{}

func (stubIndexRepoStore) ReplaceRepoIndex(context.Context, string, string, []indexstore.ChunkRecordInput) error {
	return nil
}

type stubIndexRepoEmbedder struct{}

func (stubIndexRepoEmbedder) EmbedTexts(context.Context, []string) (embeddings.Result, error) {
	return embeddings.Result{}, nil
}

type inspectIndexStoreStub struct {
	listFn func(context.Context, string, string) ([]indexstore.ChunkRecord, error)
}

func (s inspectIndexStoreStub) ListRepoIndex(ctx context.Context, sessionID, repoRoot string) ([]indexstore.ChunkRecord, error) {
	return s.listFn(ctx, sessionID, repoRoot)
}

type acceptanceEmbedder struct{}

func (acceptanceEmbedder) EmbedTexts(_ context.Context, texts []string) (embeddings.Result, error) {
	vectors := make([][]float32, 0, len(texts))
	for i, text := range texts {
		vectors = append(vectors, []float32{float32(i + 1), float32(len(text)), float32((i + 1) * 10)})
	}

	return embeddings.Result{
		Model:      "acceptance-embedding-model",
		Dimensions: 3,
		Vectors:    vectors,
	}, nil
}

type scopedIndexStore struct {
	rows map[string][]indexstore.ChunkRecord
}

func newScopedIndexStore() *scopedIndexStore {
	return &scopedIndexStore{
		rows: make(map[string][]indexstore.ChunkRecord),
	}
}

func (s *scopedIndexStore) ReplaceRepoIndex(_ context.Context, sessionID, repoRoot string, chunks []indexstore.ChunkRecordInput) error {
	key := scopedIndexStoreKey(sessionID, repoRoot)
	records := make([]indexstore.ChunkRecord, 0, len(chunks))
	for _, chunk := range chunks {
		records = append(records, indexstore.ChunkRecord{
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
	s.rows[key] = records
	return nil
}

func (s *scopedIndexStore) ListRepoIndex(_ context.Context, sessionID, repoRoot string) ([]indexstore.ChunkRecord, error) {
	key := scopedIndexStoreKey(sessionID, repoRoot)
	rows := s.rows[key]
	cloned := make([]indexstore.ChunkRecord, 0, len(rows))
	for _, row := range rows {
		copyRow := row
		copyRow.Embedding = append([]float32(nil), row.Embedding...)
		cloned = append(cloned, copyRow)
	}
	return cloned, nil
}

func scopedIndexStoreKey(sessionID, repoRoot string) string {
	return sessionID + "\n" + repoRoot
}

func writeAcceptanceRepoFile(t *testing.T, repoRoot, relPath, content string) {
	t.Helper()

	fullPath := filepath.Join(repoRoot, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll %q returned error: %v", relPath, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %q returned error: %v", relPath, err)
	}
}
