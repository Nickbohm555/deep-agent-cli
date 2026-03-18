package river

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexing"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync/snapshot"
	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	riverqueue "github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestSyncWorkerRetriesAndAvoidsDuplicateLogicalUpdates(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	store := &memorySyncStore{}
	enqueuer := &stubIndexEnqueuer{}
	worker := NewSyncWorker(store, enqueuer, nil)

	buildCalls := 0
	worker.buildSnapshot = func(root string) (*snapshot.Snapshot, error) {
		buildCalls++
		if buildCalls == 1 {
			return nil, errors.New("transient snapshot failure")
		}
		return snapshot.BuildSnapshot(root)
	}

	job := &riverqueue.Job[syncJobArgs]{
		JobRow: &rivertype.JobRow{Attempt: 1},
		Args: newSyncJobArgs(contracts.SyncJobPayload{
			SessionID:   "session-1",
			RepoRoot:    repoRoot,
			RequestedAt: time.Unix(1700000000, 0).UTC(),
			Trigger:     "manual",
		}),
	}

	if err := worker.Work(context.Background(), job); err == nil {
		t.Fatal("first Work returned nil error, want retryable error")
	}

	job.JobRow.Attempt = 2
	if err := worker.Work(context.Background(), job); err != nil {
		t.Fatalf("second Work returned error: %v", err)
	}

	job.JobRow.Attempt = 3
	if err := worker.Work(context.Background(), job); err != nil {
		t.Fatalf("third Work returned error: %v", err)
	}

	if store.logicalUpdates != 1 {
		t.Fatalf("logicalUpdates = %d, want 1", store.logicalUpdates)
	}
	if len(enqueuer.payloads) != 1 {
		t.Fatalf("enqueued payload count = %d, want 1", len(enqueuer.payloads))
	}
	if got := enqueuer.payloads[0].Delta.ChangedPaths(); len(got) != 1 || got[0] != "README.md" {
		t.Fatalf("changed paths = %#v, want [README.md]", got)
	}
}

func TestIndexWorkerRetriesAndRepeatedExecutionRemainIdempotent(t *testing.T) {
	t.Parallel()

	runner := &stubIndexRunner{
		failCalls: map[int]error{
			1: errors.New("transient delta apply failure"),
		},
	}
	worker := NewIndexWorker(runner.Run, nil)

	job := &riverqueue.Job[indexJobArgs]{
		JobRow: &rivertype.JobRow{Attempt: 1},
		Args: newIndexJobArgs(contracts.IndexJobPayload{
			SessionID:  "session-2",
			RepoRoot:   "/repo/project",
			SnapshotID: 17,
			RootHash:   "root-17",
			Delta: indexsync.SyncDelta{
				Changes: []indexsync.DeltaRecord{
					{Path: "docs/guide.md", Action: indexsync.DeltaActionModify},
				},
			},
		}),
	}

	if err := worker.Work(context.Background(), job); err == nil {
		t.Fatal("first Work returned nil error, want retryable error")
	}

	job.JobRow.Attempt = 2
	if err := worker.Work(context.Background(), job); err != nil {
		t.Fatalf("second Work returned error: %v", err)
	}

	job.JobRow.Attempt = 3
	if err := worker.Work(context.Background(), job); err != nil {
		t.Fatalf("third Work returned error: %v", err)
	}

	if runner.calls != 2 {
		t.Fatalf("runner calls = %d, want 2", runner.calls)
	}
	if runner.logicalUpdates != 1 {
		t.Fatalf("logicalUpdates = %d, want 1", runner.logicalUpdates)
	}
}

func TestIndexWorkerCrashAfterApplySkipsReplayOnRetry(t *testing.T) {
	t.Parallel()

	runner := &stubIndexRunner{}
	checkpoint := newMemoryIndexApplyCheckpoint()
	worker := NewIndexWorkerWithCheckpoint(runner.Run, checkpoint, nil)
	worker.afterApply = func(context.Context, contracts.IndexJobPayload, indexing.DeltaApplyResult) error {
		return errors.New("worker crashed after checkpoint")
	}

	job := &riverqueue.Job[indexJobArgs]{
		JobRow: &rivertype.JobRow{Attempt: 1},
		Args: newIndexJobArgs(contracts.IndexJobPayload{
			SessionID:  "session-3",
			RepoRoot:   "/repo/project",
			SnapshotID: 18,
			RootHash:   "root-18",
			Delta: indexsync.SyncDelta{
				Changes: []indexsync.DeltaRecord{
					{Path: "docs/guide.md", Action: indexsync.DeltaActionModify},
				},
			},
		}),
	}

	if err := worker.Work(context.Background(), job); err == nil {
		t.Fatal("first Work returned nil error, want retryable error")
	}

	retryWorker := NewIndexWorkerWithCheckpoint(runner.Run, checkpoint, nil)
	job.JobRow.Attempt = 2
	if err := retryWorker.Work(context.Background(), job); err != nil {
		t.Fatalf("second Work returned error: %v", err)
	}

	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if runner.logicalUpdates != 1 {
		t.Fatalf("logicalUpdates = %d, want 1", runner.logicalUpdates)
	}
}

type memorySyncStore struct {
	latest         *indexstore.SnapshotState
	nextSnapshotID int64
	logicalUpdates int
}

func (s *memorySyncStore) LoadLatestSnapshot(_ context.Context, _, _ string) (*indexstore.SnapshotState, error) {
	if s.latest == nil {
		return nil, nil
	}
	return cloneSnapshotState(s.latest), nil
}

func (s *memorySyncStore) SaveSnapshotState(_ context.Context, root indexsync.SnapshotRoot, nodes []indexsync.MerkleNode, fileStates []indexsync.FileState) (indexsync.SnapshotRoot, error) {
	if s.latest != nil && s.latest.Root.RootHash == root.RootHash {
		return s.latest.Root, nil
	}

	s.nextSnapshotID++
	root.ID = s.nextSnapshotID
	root.IsActive = true
	root.Status = indexsync.SnapshotStatusActive
	if root.CompletedAt == nil {
		now := time.Now().UTC()
		root.CompletedAt = &now
	}

	storedNodes := append([]indexsync.MerkleNode(nil), nodes...)
	for i := range storedNodes {
		storedNodes[i].SnapshotID = root.ID
	}
	storedFileStates := append([]indexsync.FileState(nil), fileStates...)
	for i := range storedFileStates {
		storedFileStates[i].LastSnapshotID = &root.ID
	}

	s.latest = &indexstore.SnapshotState{
		Root:       root,
		Nodes:      storedNodes,
		FileStates: storedFileStates,
	}
	s.logicalUpdates++
	return root, nil
}

func cloneSnapshotState(state *indexstore.SnapshotState) *indexstore.SnapshotState {
	if state == nil {
		return nil
	}
	cloned := *state
	cloned.Nodes = append([]indexsync.MerkleNode(nil), state.Nodes...)
	cloned.FileStates = append([]indexsync.FileState(nil), state.FileStates...)
	return &cloned
}

type stubIndexEnqueuer struct {
	payloads []contracts.IndexJobPayload
}

func (e *stubIndexEnqueuer) EnqueueIndexJob(_ context.Context, payload contracts.IndexJobPayload) (EnqueueResult, error) {
	e.payloads = append(e.payloads, payload)
	return EnqueueResult{JobID: int64(len(e.payloads))}, nil
}

type stubIndexRunner struct {
	calls          int
	logicalUpdates int
	failCalls      map[int]error
	seen           map[string]struct{}
}

func (r *stubIndexRunner) Run(_ context.Context, payload contracts.IndexJobPayload) (indexing.DeltaApplyResult, error) {
	r.calls++
	if err, ok := r.failCalls[r.calls]; ok {
		return indexing.DeltaApplyResult{}, err
	}

	if r.seen == nil {
		r.seen = make(map[string]struct{})
	}

	key := fmt.Sprintf(
		"%s::%s::%d::%s::%v",
		payload.SessionID,
		payload.RepoRoot,
		payload.SnapshotID,
		payload.RootHash,
		payload.Delta.ChangedPaths(),
	)
	if _, ok := r.seen[key]; !ok {
		r.seen[key] = struct{}{}
		r.logicalUpdates++
	}

	return indexing.DeltaApplyResult{
		UpsertedPaths:  payload.Delta.ChangedPaths(),
		FilesTouched:   len(payload.Delta.ChangedPaths()),
		ChunksReplaced: len(payload.Delta.ChangedPaths()),
	}, nil
}
