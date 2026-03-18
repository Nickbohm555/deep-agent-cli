package status

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
)

func TestGetIndexSyncStatusEmptyState(t *testing.T) {
	t.Parallel()

	service := NewService(&stubSnapshotReader{}, &stubJobReader{})

	got, err := service.GetIndexSyncStatus(context.Background(), "session-1", "/repo/project")
	if err != nil {
		t.Fatalf("GetIndexSyncStatus returned error: %v", err)
	}

	if got.SessionID != "session-1" || got.RepoRoot != "/repo/project" {
		t.Fatalf("scope = %+v, want bound session/repo", got)
	}
	if got.LastSuccessfulSyncAt != nil || got.LastSuccessfulIndexAt != nil {
		t.Fatalf("success timestamps = %+v, want nil", got)
	}
	if got.Queue != (QueueCounts{}) {
		t.Fatalf("queue = %+v, want zero counts", got.Queue)
	}
	if got.Running.Sync != nil || got.Running.Index != nil {
		t.Fatalf("running = %+v, want nil jobs", got.Running)
	}
	if got.LatestError != nil {
		t.Fatalf("latest error = %+v, want nil", got.LatestError)
	}
}

func TestGetIndexSyncStatusAggregatesActiveJobs(t *testing.T) {
	t.Parallel()

	enqueuedAt := time.Unix(1700000100, 0).UTC()
	startedAt := enqueuedAt.Add(10 * time.Second)
	snapshotID := int64(17)

	service := NewService(
		&stubSnapshotReader{},
		&stubJobReader{
			jobs: []JobRecord{
				{JobID: 11, Kind: JobKindSync, State: JobStatePending, Attempt: 1, EnqueuedAt: &enqueuedAt},
				{JobID: 12, Kind: JobKindSync, State: JobStateRunning, Attempt: 2, EnqueuedAt: &enqueuedAt, StartedAt: &startedAt, RootHash: "root-17"},
				{JobID: 13, Kind: JobKindIndex, State: JobStatePending, Attempt: 1, EnqueuedAt: &enqueuedAt},
				{JobID: 14, Kind: JobKindIndex, State: JobStateRunning, Attempt: 1, EnqueuedAt: &enqueuedAt, StartedAt: &startedAt, SnapshotID: &snapshotID, RootHash: "root-17", DeltaSize: 3},
			},
		},
	)

	got, err := service.GetIndexSyncStatus(context.Background(), "session-1", "/repo/project")
	if err != nil {
		t.Fatalf("GetIndexSyncStatus returned error: %v", err)
	}

	if got.Queue.PendingSyncJobs != 1 || got.Queue.RunningSyncJobs != 1 {
		t.Fatalf("sync queue counts = %+v, want 1 pending + 1 running", got.Queue)
	}
	if got.Queue.PendingIndexJobs != 1 || got.Queue.RunningIndexJobs != 1 {
		t.Fatalf("index queue counts = %+v, want 1 pending + 1 running", got.Queue)
	}
	if got.Running.Sync == nil || got.Running.Sync.JobID != 12 || got.Running.Sync.RootHash != "root-17" {
		t.Fatalf("running sync = %+v, want job 12", got.Running.Sync)
	}
	if got.Running.Index == nil || got.Running.Index.JobID != 14 || got.Running.Index.DeltaSize != 3 {
		t.Fatalf("running index = %+v, want job 14 with delta size", got.Running.Index)
	}
}

func TestGetIndexSyncStatusTracksSuccessfulCompletion(t *testing.T) {
	t.Parallel()

	syncCompletedAt := time.Unix(1700000200, 0).UTC()
	indexCompletedAt := syncCompletedAt.Add(45 * time.Second)
	snapshotCompletedAt := syncCompletedAt.Add(30 * time.Second)

	service := NewService(
		&stubSnapshotReader{
			state: &indexstore.SnapshotState{
				Root: indexsync.SnapshotRoot{
					ID:          22,
					SessionID:   "session-1",
					RepoRoot:    "/repo/project",
					RootHash:    "root-22",
					Status:      indexsync.SnapshotStatusActive,
					IsActive:    true,
					CompletedAt: &snapshotCompletedAt,
				},
			},
		},
		&stubJobReader{
			jobs: []JobRecord{
				{JobID: 21, Kind: JobKindSync, State: JobStateSucceeded, FinishedAt: &syncCompletedAt, RootHash: "root-21"},
				{JobID: 22, Kind: JobKindIndex, State: JobStateSucceeded, FinishedAt: &indexCompletedAt, RootHash: "root-22", DeltaSize: 4},
			},
		},
	)

	got, err := service.GetIndexSyncStatus(context.Background(), "session-1", "/repo/project")
	if err != nil {
		t.Fatalf("GetIndexSyncStatus returned error: %v", err)
	}

	if got.LatestSnapshot.ID != 22 || got.LatestSnapshot.RootHash != "root-22" || got.LatestSnapshot.Status != "active" {
		t.Fatalf("latest snapshot = %+v, want active root 22", got.LatestSnapshot)
	}
	if got.LastSuccessfulSyncAt == nil || !got.LastSuccessfulSyncAt.Equal(snapshotCompletedAt) {
		t.Fatalf("last successful sync = %v, want snapshot completion %v", got.LastSuccessfulSyncAt, snapshotCompletedAt)
	}
	if got.LastSuccessfulIndexAt == nil || !got.LastSuccessfulIndexAt.Equal(indexCompletedAt) {
		t.Fatalf("last successful index = %v, want %v", got.LastSuccessfulIndexAt, indexCompletedAt)
	}
	if got.LastDeltaSize != 4 {
		t.Fatalf("last delta size = %d, want 4", got.LastDeltaSize)
	}
}

func TestGetIndexSyncStatusPrefersLatestFailure(t *testing.T) {
	t.Parallel()

	failedAt := time.Unix(1700000300, 0).UTC()
	retryAt := failedAt.Add(2 * time.Minute)
	snapshotID := int64(31)

	service := NewService(
		&stubSnapshotReader{},
		&stubJobReader{
			jobs: []JobRecord{
				{JobID: 30, Kind: JobKindSync, State: JobStateFailed, FinishedAt: &failedAt, Error: "snapshot builder crashed"},
				{JobID: 31, Kind: JobKindIndex, State: JobStateRetryable, FinishedAt: &retryAt, Attempt: 2, SnapshotID: &snapshotID, RootHash: "root-31", Error: "embedding refresh timeout"},
			},
		},
	)

	got, err := service.GetIndexSyncStatus(context.Background(), "session-1", "/repo/project")
	if err != nil {
		t.Fatalf("GetIndexSyncStatus returned error: %v", err)
	}

	if got.LatestError == nil {
		t.Fatal("LatestError is nil, want retryable failure summary")
	}
	if got.LatestError.JobID != 31 || got.LatestError.Kind != JobKindIndex || got.LatestError.State != JobStateRetryable {
		t.Fatalf("latest error = %+v, want latest index retry", got.LatestError)
	}
	if got.LatestError.Message != "embedding refresh timeout" {
		t.Fatalf("latest error message = %q, want retry error", got.LatestError.Message)
	}
	if got.LatestError.OccurredAt == nil || !got.LatestError.OccurredAt.Equal(retryAt) {
		t.Fatalf("latest error occurred at = %v, want %v", got.LatestError.OccurredAt, retryAt)
	}
}

func TestGetIndexSyncStatusValidatesScopeAndDependencies(t *testing.T) {
	t.Parallel()

	service := NewService(&stubSnapshotReader{err: errors.New("boom")}, &stubJobReader{})
	if _, err := service.GetIndexSyncStatus(context.Background(), "", "/repo/project"); err == nil || err.Error() != "session_id is required" {
		t.Fatalf("empty session error = %v, want session_id is required", err)
	}
	if _, err := service.GetIndexSyncStatus(context.Background(), "session-1", ""); err == nil || err.Error() != "repo_root is required" {
		t.Fatalf("empty repo error = %v, want repo_root is required", err)
	}
	if _, err := service.GetIndexSyncStatus(context.Background(), "session-1", "/repo/project"); err == nil || err.Error() != "load latest snapshot status: boom" {
		t.Fatalf("snapshot read error = %v, want wrapped boom", err)
	}
}

type stubSnapshotReader struct {
	state *indexstore.SnapshotState
	err   error
}

func (s *stubSnapshotReader) LoadLatestSnapshot(context.Context, string, string) (*indexstore.SnapshotState, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.state, nil
}

type stubJobReader struct {
	jobs []JobRecord
	err  error
}

func (s *stubJobReader) ListJobs(context.Context, string, string) ([]JobRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]JobRecord(nil), s.jobs...), nil
}
