package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	riverjobs "github.com/Nickbohm555/deep-agent-cli/internal/jobs/river"
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

func TestIndexRepoEnqueuesBackgroundSyncJob(t *testing.T) {
	restorePool := newIndexRepoPool
	restoreJobClient := newIndexRepoJobClient
	restoreClose := closeIndexRepoPool
	t.Cleanup(func() {
		newIndexRepoPool = restorePool
		newIndexRepoJobClient = restoreJobClient
		closeIndexRepoPool = restoreClose
	})

	newIndexRepoPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	closeIndexRepoPool = func(*pgxpool.Pool) {}

	client := &stubIndexRepoJobClient{
		result: riverjobs.EnqueueResult{JobID: 41},
	}
	newIndexRepoJobClient = func(*pgxpool.Pool) (indexRepoJobClient, error) { return client, nil }

	ctx := mustBindSessionScope(t, "session-123", t.TempDir())
	result, err := IndexRepo(ctx, toolCall(t, "index_repo", IndexRepoInput{}))
	if err != nil {
		t.Fatalf("IndexRepo returned error: %v", err)
	}
	if result.IsError {
		t.Fatal("IndexRepo unexpectedly marked result as error")
	}

	if len(client.payloads) != 1 {
		t.Fatalf("enqueued payload count = %d, want 1", len(client.payloads))
	}
	repoRoot, err := runtime.RepoRootFromContext(ctx)
	if err != nil {
		t.Fatalf("RepoRootFromContext returned error: %v", err)
	}
	if client.payloads[0].SessionID != "session-123" {
		t.Fatalf("sessionID = %q, want session-123", client.payloads[0].SessionID)
	}
	if client.payloads[0].RepoRoot != repoRoot {
		t.Fatalf("repoRoot = %q, want %q", client.payloads[0].RepoRoot, repoRoot)
	}
	if client.payloads[0].Purpose != contracts.JobPurposeSyncScan {
		t.Fatalf("purpose = %q, want %q", client.payloads[0].Purpose, contracts.JobPurposeSyncScan)
	}
	if client.payloads[0].Trigger != "tool:index_repo" {
		t.Fatalf("trigger = %q, want tool:index_repo", client.payloads[0].Trigger)
	}

	var output struct {
		JobID     int64  `json:"job_id"`
		Duplicate bool   `json:"duplicate"`
		Status    string `json:"status"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatalf("IndexRepo output is not valid JSON: %v", err)
	}
	if output.JobID != 41 || output.Duplicate || output.Status != "queued" || output.Message != "Repository sync started in the background." {
		t.Fatalf("IndexRepo output = %+v, want queued background sync status", output)
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

func TestIndexRepoSurfacesEnqueueFailures(t *testing.T) {
	restorePool := newIndexRepoPool
	restoreJobClient := newIndexRepoJobClient
	restoreClose := closeIndexRepoPool
	t.Cleanup(func() {
		newIndexRepoPool = restorePool
		newIndexRepoJobClient = restoreJobClient
		closeIndexRepoPool = restoreClose
	})

	newIndexRepoPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	closeIndexRepoPool = func(*pgxpool.Pool) {}
	newIndexRepoJobClient = func(*pgxpool.Pool) (indexRepoJobClient, error) {
		return &stubIndexRepoJobClient{err: errors.New("enqueue failed")}, nil
	}

	ctx := mustBindSessionScope(t, "session-123", t.TempDir())
	_, err := IndexRepo(ctx, toolCall(t, "index_repo", IndexRepoInput{}))
	if err == nil {
		t.Fatal("IndexRepo returned nil error on enqueue failure")
	}
	if err.Error() != "enqueue index_repo sync job: enqueue failed" {
		t.Fatalf("IndexRepo error = %q, want enqueue failure", err)
	}
}

func TestIndexRepoDeduplicatedQueueStatusIsReturned(t *testing.T) {
	restoreIndexPool := newIndexRepoPool
	restoreIndexJobClient := newIndexRepoJobClient
	restoreIndexClose := closeIndexRepoPool
	t.Cleanup(func() {
		newIndexRepoPool = restoreIndexPool
		newIndexRepoJobClient = restoreIndexJobClient
		closeIndexRepoPool = restoreIndexClose
	})

	newIndexRepoPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	closeIndexRepoPool = func(*pgxpool.Pool) {}
	client := &stubIndexRepoJobClient{
		result: riverjobs.EnqueueResult{JobID: 52, Duplicate: true},
	}
	newIndexRepoJobClient = func(*pgxpool.Pool) (indexRepoJobClient, error) { return client, nil }

	ctx := mustBindSessionScope(t, "session-one", t.TempDir())
	indexResult, err := IndexRepo(ctx, toolCall(t, "index_repo", IndexRepoInput{}))
	if err != nil {
		t.Fatalf("IndexRepo returned error: %v", err)
	}

	var output struct {
		JobID     int64  `json:"job_id"`
		Duplicate bool   `json:"duplicate"`
		Status    string `json:"status"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal([]byte(indexResult.Output), &output); err != nil {
		t.Fatalf("IndexRepo output is not valid JSON: %v", err)
	}
	if output.JobID != 52 || !output.Duplicate || output.Status != "already_queued" || output.Message != "Repository sync is already queued in the background." {
		t.Fatalf("IndexRepo output = %+v, want duplicate queue status", output)
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

type stubIndexRepoJobClient struct {
	payloads []contracts.SyncJobPayload
	result   riverjobs.EnqueueResult
	err      error
}

func (s *stubIndexRepoJobClient) EnqueueSyncJob(_ context.Context, payload contracts.SyncJobPayload) (riverjobs.EnqueueResult, error) {
	s.payloads = append(s.payloads, payload)
	if s.err != nil {
		return riverjobs.EnqueueResult{}, s.err
	}
	return s.result, nil
}

type inspectIndexStoreStub struct {
	listFn func(context.Context, string, string) ([]indexstore.ChunkRecord, error)
}

func (s inspectIndexStoreStub) ListRepoIndex(ctx context.Context, sessionID, repoRoot string) ([]indexstore.ChunkRecord, error) {
	return s.listFn(ctx, sessionID, repoRoot)
}
