package river

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	indexdiff "github.com/Nickbohm555/deep-agent-cli/internal/indexsync/diff"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync/snapshot"
	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	"github.com/Nickbohm555/deep-agent-cli/internal/observability"
	riverqueue "github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type jobOutcomeStatus string

const (
	jobOutcomeStatusSuccess jobOutcomeStatus = "success"
	jobOutcomeStatusRetry   jobOutcomeStatus = "retry"
	jobOutcomeStatusFailure jobOutcomeStatus = "failure"
)

type JobOutcome struct {
	Status        jobOutcomeStatus `json:"status"`
	Kind          string           `json:"kind"`
	JobID         int64            `json:"job_id,omitempty"`
	SessionID     string           `json:"session_id"`
	RepoRoot      string           `json:"repo_root"`
	Attempt       int              `json:"attempt"`
	SnapshotID    int64            `json:"snapshot_id,omitempty"`
	RootHash      string           `json:"root_hash,omitempty"`
	EnqueuedJobID int64            `json:"enqueued_job_id,omitempty"`
	Duplicate     bool             `json:"duplicate,omitempty"`
	ChangedCount  int              `json:"changed_count,omitempty"`
	QueueLatency  time.Duration    `json:"queue_latency,omitempty"`
	Duration      time.Duration    `json:"duration,omitempty"`
	Message       string           `json:"message,omitempty"`
	Error         string           `json:"error,omitempty"`
}

type syncSnapshotStore interface {
	LoadLatestSnapshot(context.Context, string, string) (*indexstore.SnapshotState, error)
	SaveSnapshotState(context.Context, indexsync.SnapshotRoot, []indexsync.MerkleNode, []indexsync.FileState) (indexsync.SnapshotRoot, error)
}

type indexJobEnqueuer interface {
	EnqueueIndexJob(context.Context, contracts.IndexJobPayload) (EnqueueResult, error)
}

type syncSnapshotBuilder func(string) (*snapshot.Snapshot, error)

type SyncWorker struct {
	riverqueue.WorkerDefaults[syncJobArgs]

	store         syncSnapshotStore
	enqueuer      indexJobEnqueuer
	buildSnapshot syncSnapshotBuilder
	logger        *slog.Logger
	metrics       *observability.IndexSyncMetrics
}

func NewSyncWorker(store syncSnapshotStore, enqueuer indexJobEnqueuer, logger *slog.Logger) *SyncWorker {
	return &SyncWorker{
		store:         store,
		enqueuer:      enqueuer,
		buildSnapshot: snapshot.BuildSnapshot,
		logger:        logger,
		metrics:       observability.DefaultIndexSyncMetrics(),
	}
}

func (w *SyncWorker) Work(ctx context.Context, job *riverqueue.Job[syncJobArgs]) error {
	startedAt := time.Now().UTC()
	outcome, err := w.run(ctx, job)
	w.finishOutcome(&outcome, startedAt)
	switch {
	case err == nil:
		w.logOutcome(outcome)
		w.recordMetrics(outcome)
		return nil
	case isRetryableWorkerError(err):
		outcome.Status = jobOutcomeStatusRetry
		outcome.Error = err.Error()
		w.logOutcome(outcome)
		w.recordMetrics(outcome)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, rivertype.ErrJobCancelledRemotely) {
			return riverqueue.JobSnooze(0)
		}
		return err
	default:
		outcome.Status = jobOutcomeStatusFailure
		outcome.Error = err.Error()
		w.logOutcome(outcome)
		w.recordMetrics(outcome)
		return riverqueue.JobCancel(err)
	}
}

func (w *SyncWorker) run(ctx context.Context, job *riverqueue.Job[syncJobArgs]) (JobOutcome, error) {
	outcome := newJobOutcome(kindSyncJob, job.Args.Payload.SessionID, job.Args.Payload.RepoRoot, job)

	if w == nil {
		return outcome, errWorkerConfiguration("sync worker is nil")
	}
	if w.store == nil {
		return outcome, errWorkerConfiguration("sync worker store is required")
	}
	if w.enqueuer == nil {
		return outcome, errWorkerConfiguration("sync worker enqueuer is required")
	}
	if w.buildSnapshot == nil {
		return outcome, errWorkerConfiguration("sync worker snapshot builder is required")
	}

	payload, err := normalizeSyncPayload(job.Args.Payload)
	if err != nil {
		return outcome, err
	}
	outcome.SessionID = payload.SessionID
	outcome.RepoRoot = payload.RepoRoot
	if !payload.RequestedAt.IsZero() {
		outcome.QueueLatency = time.Since(payload.RequestedAt)
	}

	current, err := w.buildSnapshot(payload.RepoRoot)
	if err != nil {
		return outcome, fmt.Errorf("build sync snapshot: %w", err)
	}
	outcome.RootHash = current.RootHash

	latest, err := w.store.LoadLatestSnapshot(ctx, payload.SessionID, payload.RepoRoot)
	if err != nil {
		return outcome, fmt.Errorf("load latest snapshot: %w", err)
	}

	if latest != nil && strings.TrimSpace(latest.Root.RootHash) == current.RootHash {
		outcome.Message = "snapshot already current"
		return outcome, nil
	}

	previousSnapshot, parentSnapshotID, err := latestSnapshotForDiff(latest)
	if err != nil {
		return outcome, fmt.Errorf("prepare previous snapshot: %w", err)
	}

	deltaSet, err := indexdiff.DiffSnapshots(previousSnapshot, current)
	if err != nil {
		return outcome, fmt.Errorf("diff snapshots: %w", err)
	}
	outcome.ChangedCount = len(deltaSet.Changes)

	now := time.Now().UTC()
	savedRoot, err := w.store.SaveSnapshotState(
		ctx,
		indexsync.SnapshotRoot{
			SessionID:        payload.SessionID,
			RepoRoot:         payload.RepoRoot,
			RootHash:         current.RootHash,
			ParentSnapshotID: parentSnapshotID,
			Status:           indexsync.SnapshotStatusActive,
			IsActive:         true,
			CompletedAt:      &now,
		},
		snapshotEntriesToMerkleNodes(current.Entries),
		snapshotEntriesToFileStates(payload.SessionID, payload.RepoRoot, current.Entries),
	)
	if err != nil {
		return outcome, fmt.Errorf("save snapshot state: %w", err)
	}
	outcome.SnapshotID = savedRoot.ID

	if len(deltaSet.Changes) == 0 {
		outcome.Message = "snapshot saved with no file deltas"
		return outcome, nil
	}

	enqueueResult, err := w.enqueuer.EnqueueIndexJob(ctx, contracts.IndexJobPayload{
		SessionID:   payload.SessionID,
		RepoRoot:    payload.RepoRoot,
		Purpose:     contracts.JobPurposeIndexApplyDelta,
		SnapshotID:  savedRoot.ID,
		RootHash:    savedRoot.RootHash,
		Delta:       syncDeltaFromSet(payload.SessionID, payload.RepoRoot, parentSnapshotID, savedRoot.ID, deltaSet),
		RequestedAt: payload.RequestedAt,
	})
	if err != nil {
		return outcome, fmt.Errorf("enqueue index job: %w", err)
	}

	outcome.EnqueuedJobID = enqueueResult.JobID
	outcome.Duplicate = enqueueResult.Duplicate
	if enqueueResult.Duplicate {
		outcome.Message = "index job already queued"
	} else {
		outcome.Message = "index job enqueued"
	}

	return outcome, nil
}

func (w *SyncWorker) logOutcome(outcome JobOutcome) {
	logger := w.logger
	if logger == nil {
		return
	}

	attrs := []any{
		"kind", outcome.Kind,
		"status", outcome.Status,
		"job_id", outcome.JobID,
		"session_id", outcome.SessionID,
		"repo_root", outcome.RepoRoot,
		"attempt", outcome.Attempt,
		"snapshot_id", outcome.SnapshotID,
		"root_hash", outcome.RootHash,
		"changed_count", outcome.ChangedCount,
		"queue_latency_ms", outcome.QueueLatency.Milliseconds(),
		"duration_ms", outcome.Duration.Milliseconds(),
		"enqueued_job_id", outcome.EnqueuedJobID,
		"duplicate", outcome.Duplicate,
	}
	if outcome.Message != "" {
		attrs = append(attrs, "message", outcome.Message)
	}
	if outcome.Error != "" {
		attrs = append(attrs, "error", outcome.Error)
	}
	switch outcome.Status {
	case jobOutcomeStatusFailure:
		logger.Error("river worker outcome", attrs...)
	case jobOutcomeStatusRetry:
		logger.Warn("river worker outcome", attrs...)
	default:
		logger.Info("river worker outcome", attrs...)
	}
}

func (w *SyncWorker) finishOutcome(outcome *JobOutcome, startedAt time.Time) {
	if outcome == nil {
		return
	}

	outcome.Duration = time.Since(startedAt)
	if outcome.JobID == 0 {
		outcome.JobID = jobIDFromAttempt(outcome.Attempt)
	}
}

func (w *SyncWorker) recordMetrics(outcome JobOutcome) {
	if w == nil || w.metrics == nil {
		return
	}

	w.metrics.RecordJobLifecycle(observability.IndexSyncMetricEvent{
		Kind:       outcome.Kind,
		Status:     string(outcome.Status),
		SessionID:  outcome.SessionID,
		RepoRoot:   outcome.RepoRoot,
		JobID:      outcome.JobID,
		Attempt:    outcome.Attempt,
		SnapshotID: outcome.SnapshotID,
		RootHash:   outcome.RootHash,
		DeltaSize:  outcome.ChangedCount,
	}, outcome.QueueLatency, outcome.Duration)
}

type workerConfigurationError struct {
	message string
}

func (e workerConfigurationError) Error() string {
	return e.message
}

func errWorkerConfiguration(message string) error {
	return workerConfigurationError{message: message}
}

func isRetryableWorkerError(err error) bool {
	if err == nil {
		return false
	}

	var configErr workerConfigurationError
	return !errors.As(err, &configErr)
}

func newJobOutcome[T riverqueue.JobArgs](kind, sessionID, repoRoot string, job *riverqueue.Job[T]) JobOutcome {
	outcome := JobOutcome{
		Status:    jobOutcomeStatusSuccess,
		Kind:      kind,
		SessionID: strings.TrimSpace(sessionID),
		RepoRoot:  normalizeRepoRoot(repoRoot),
	}
	if job != nil && job.JobRow != nil {
		outcome.JobID = job.JobRow.ID
		outcome.Attempt = job.JobRow.Attempt
	}
	return outcome
}

func jobIDFromAttempt(attempt int) int64 {
	if attempt <= 0 {
		return 0
	}
	return int64(attempt)
}

func normalizeSyncPayload(payload contracts.SyncJobPayload) (contracts.SyncJobPayload, error) {
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.RepoRoot = normalizeRepoRoot(payload.RepoRoot)
	payload.Purpose = firstNonEmpty(payload.Purpose, contracts.JobPurposeSyncScan)
	payload.Trigger = strings.TrimSpace(payload.Trigger)
	if payload.RequestedAt.IsZero() {
		payload.RequestedAt = time.Now().UTC()
	}
	if payload.SessionID == "" {
		return payload, fmt.Errorf("sync job session_id is required")
	}
	if payload.RepoRoot == "" {
		return payload, fmt.Errorf("sync job repo_root is required")
	}
	return payload, nil
}

func latestSnapshotForDiff(state *indexstore.SnapshotState) (*snapshot.Snapshot, *int64, error) {
	if state == nil {
		return nil, nil, nil
	}
	if strings.TrimSpace(state.Root.RootHash) == "" {
		return nil, nil, fmt.Errorf("latest snapshot root_hash is required")
	}

	previous, err := snapshotFromMerkleNodes(state.Root.RepoRoot, state.Nodes, state.Root.RootHash)
	if err != nil {
		return nil, nil, err
	}

	parentSnapshotID := state.Root.ID
	return previous, &parentSnapshotID, nil
}

func snapshotFromMerkleNodes(repoRoot string, nodes []indexsync.MerkleNode, rootHash string) (*snapshot.Snapshot, error) {
	entries := make([]snapshot.Entry, 0, len(nodes))
	for _, node := range nodes {
		entries = append(entries, snapshot.Entry{
			Path:        node.Path,
			ParentPath:  node.ParentPath,
			NodeType:    node.NodeType,
			NodeHash:    node.NodeHash,
			ParentHash:  node.ParentHash,
			ContentHash: node.ContentHash,
			SizeBytes:   node.SizeBytes,
			MTimeNS:     node.MTimeNS,
		})
	}

	root, err := snapshot.BuildNodeTree(entries)
	if err != nil {
		return nil, fmt.Errorf("build previous node tree: %w", err)
	}

	return &snapshot.Snapshot{
		RepoRoot: repoRoot,
		Entries:  entries,
		Root:     root,
		RootHash: rootHash,
	}, nil
}

func snapshotEntriesToMerkleNodes(entries []snapshot.Entry) []indexsync.MerkleNode {
	nodes := make([]indexsync.MerkleNode, 0, len(entries))
	for _, entry := range entries {
		status := indexsync.FileStatusActive
		if entry.NodeType == indexsync.NodeTypeDir {
			status = indexsync.FileStatusActive
		}
		nodes = append(nodes, indexsync.MerkleNode{
			Path:        entry.Path,
			ParentPath:  entry.ParentPath,
			NodeType:    entry.NodeType,
			NodeHash:    entry.NodeHash,
			ParentHash:  entry.ParentHash,
			ContentHash: entry.ContentHash,
			SizeBytes:   entry.SizeBytes,
			MTimeNS:     entry.MTimeNS,
			Status:      status,
		})
	}
	return nodes
}

func snapshotEntriesToFileStates(sessionID, repoRoot string, entries []snapshot.Entry) []indexsync.FileState {
	fileStates := make([]indexsync.FileState, 0, len(entries))
	for _, entry := range entries {
		if entry.NodeType != indexsync.NodeTypeFile {
			continue
		}
		fileStates = append(fileStates, indexsync.FileState{
			SessionID:   sessionID,
			RepoRoot:    repoRoot,
			RelPath:     entry.Path,
			ContentHash: entry.ContentHash,
			NodeHash:    entry.NodeHash,
			ParentHash:  entry.ParentHash,
			SizeBytes:   entry.SizeBytes,
			MTimeNS:     entry.MTimeNS,
			Status:      indexsync.FileStatusActive,
		})
	}
	return fileStates
}

func syncDeltaFromSet(sessionID, repoRoot string, previousSnapshotID *int64, currentSnapshotID int64, deltaSet indexdiff.SyncDeltaSet) indexsync.SyncDelta {
	delta := indexsync.SyncDelta{
		SessionID:          sessionID,
		RepoRoot:           repoRoot,
		PreviousSnapshotID: previousSnapshotID,
		CurrentSnapshotID:  &currentSnapshotID,
		PreviousRootHash:   deltaSet.PreviousRootHash,
		CurrentRootHash:    deltaSet.CurrentRootHash,
		Changes:            make([]indexsync.DeltaRecord, 0, len(deltaSet.Changes)),
	}

	for _, change := range deltaSet.Changes {
		delta.Changes = append(delta.Changes, indexsync.DeltaRecord{
			Path:                change.Path,
			Action:              deltaActionFromOp(change.Op),
			NodeType:            indexsync.NodeTypeFile,
			PreviousNodeHash:    change.PreviousNodeHash,
			CurrentNodeHash:     change.CurrentNodeHash,
			PreviousContentHash: change.PreviousContentHash,
			CurrentContentHash:  change.CurrentContentHash,
		})
	}

	return delta
}

func deltaActionFromOp(op indexdiff.DeltaOp) indexsync.DeltaAction {
	switch op {
	case indexdiff.DeltaOpAdd:
		return indexsync.DeltaActionAdd
	case indexdiff.DeltaOpDelete:
		return indexsync.DeltaActionDelete
	default:
		return indexsync.DeltaActionModify
	}
}
