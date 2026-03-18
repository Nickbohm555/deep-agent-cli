package handlers

import (
	"context"
	"encoding/json"
	"errors"
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
