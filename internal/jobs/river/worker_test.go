package river

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexing"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync/snapshot"
	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	"github.com/Nickbohm555/deep-agent-cli/internal/observability"
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

func TestSyncWorkerEmitsMetricsAndStructuredLogs(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	store := &memorySyncStore{}
	enqueuer := &stubIndexEnqueuer{}
	metrics := observability.NewIndexSyncMetrics()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	worker := NewSyncWorker(store, enqueuer, logger)
	worker.metrics = metrics

	job := &riverqueue.Job[syncJobArgs]{
		JobRow: &rivertype.JobRow{ID: 42, Attempt: 2},
		Args: newSyncJobArgs(contracts.SyncJobPayload{
			SessionID:   "session-obs",
			RepoRoot:    repoRoot,
			RequestedAt: time.Now().UTC().Add(-2 * time.Second),
			Trigger:     "manual",
		}),
	}

	if err := worker.Work(context.Background(), job); err != nil {
		t.Fatalf("Work returned error: %v", err)
	}

	snapshot := metrics.Snapshot()
	if got := snapshot.Counters[observability.MetricJobCompletionsTotal+"|index_sync|success"]; got != 1 {
		t.Fatalf("completion counter = %v, want 1", got)
	}
	if got := snapshot.Counters[observability.MetricSyncJobDurationSeconds+"|index_sync|success"]; got <= 0 {
		t.Fatalf("duration counter = %v, want > 0", got)
	}
	if got := snapshot.Counters[observability.MetricJobQueueLatencySeconds+"|index_sync|success"]; got < 1.5 {
		t.Fatalf("queue latency counter = %v, want >= 1.5", got)
	}
	if got := snapshot.Counters[observability.MetricDeltaSize+"|index_sync|success"]; got != 1 {
		t.Fatalf("delta size counter = %v, want 1", got)
	}

	logOutput := logs.String()
	for _, want := range []string{
		"\"kind\":\"index_sync\"",
		"\"job_id\":42",
		"\"session_id\":\"session-obs\"",
		"\"root_hash\":",
		"\"queue_latency_ms\":",
		"\"duration_ms\":",
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("log output missing %q: %s", want, logOutput)
		}
	}
}

func TestIndexWorkerEmitsRetryMetricsAndErrorLogs(t *testing.T) {
	t.Parallel()

	runner := &stubIndexRunner{
		failCalls: map[int]error{
			1: errors.New("transient delta apply failure"),
		},
	}
	metrics := observability.NewIndexSyncMetrics()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	worker := NewIndexWorker(runner.Run, logger)
	worker.metrics = metrics

	job := &riverqueue.Job[indexJobArgs]{
		JobRow: &rivertype.JobRow{ID: 84, Attempt: 3},
		Args: newIndexJobArgs(contracts.IndexJobPayload{
			SessionID:   "session-retry",
			RepoRoot:    "/repo/project",
			SnapshotID:  17,
			RootHash:    "root-17",
			RequestedAt: time.Now().UTC().Add(-3 * time.Second),
			Delta: indexsync.SyncDelta{
				Changes: []indexsync.DeltaRecord{
					{Path: "docs/guide.md", Action: indexsync.DeltaActionModify},
				},
			},
		}),
	}

	if err := worker.Work(context.Background(), job); err == nil {
		t.Fatal("Work returned nil error, want retryable error")
	}

	snapshot := metrics.Snapshot()
	if got := snapshot.Counters[observability.MetricJobRetriesTotal+"|index_apply_delta|retry"]; got != 1 {
		t.Fatalf("retry counter = %v, want 1", got)
	}
	if got := snapshot.Counters[observability.MetricIndexJobDurationSeconds+"|index_apply_delta|retry"]; got <= 0 {
		t.Fatalf("duration counter = %v, want > 0", got)
	}
	if got := snapshot.Counters[observability.MetricJobQueueLatencySeconds+"|index_apply_delta|retry"]; got < 2.5 {
		t.Fatalf("queue latency counter = %v, want >= 2.5", got)
	}
	if got := snapshot.Counters[observability.MetricDeltaSize+"|index_apply_delta|retry"]; got != 1 {
		t.Fatalf("delta size counter = %v, want 1", got)
	}

	logOutput := logs.String()
	for _, want := range []string{
		"\"level\":\"WARN\"",
		"\"kind\":\"index_apply_delta\"",
		"\"job_id\":84",
		"\"snapshot_id\":17",
		"\"root_hash\":\"root-17\"",
		"\"error\":\"run index apply: transient delta apply failure\"",
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("log output missing %q: %s", want, logOutput)
		}
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
