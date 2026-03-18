package river

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexing"
	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	"github.com/Nickbohm555/deep-agent-cli/internal/observability"
	riverqueue "github.com/riverqueue/river"
)

type indexRunner func(context.Context, contracts.IndexJobPayload) (indexing.DeltaApplyResult, error)

type indexApplyCheckpoint interface {
	IsApplied(context.Context, contracts.IndexJobPayload) (bool, error)
	MarkApplied(context.Context, contracts.IndexJobPayload) error
}

type indexCheckpointRecord struct {
	SnapshotID int64
	RootHash   string
}

type memoryIndexApplyCheckpoint struct {
	mu      sync.Mutex
	records map[string]indexCheckpointRecord
}

func newMemoryIndexApplyCheckpoint() *memoryIndexApplyCheckpoint {
	return &memoryIndexApplyCheckpoint{
		records: make(map[string]indexCheckpointRecord),
	}
}

func (c *memoryIndexApplyCheckpoint) IsApplied(_ context.Context, payload contracts.IndexJobPayload) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("index apply checkpoint is nil")
	}

	key, record, ok := checkpointRecordForPayload(payload)
	if !ok {
		return false, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	stored, exists := c.records[key]
	if !exists {
		return false, nil
	}

	return stored == record, nil
}

func (c *memoryIndexApplyCheckpoint) MarkApplied(_ context.Context, payload contracts.IndexJobPayload) error {
	if c == nil {
		return fmt.Errorf("index apply checkpoint is nil")
	}

	key, record, ok := checkpointRecordForPayload(payload)
	if !ok {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.records[key] = record
	return nil
}

func checkpointRecordForPayload(payload contracts.IndexJobPayload) (string, indexCheckpointRecord, bool) {
	record := indexCheckpointRecord{
		SnapshotID: payload.SnapshotID,
		RootHash:   strings.TrimSpace(payload.RootHash),
	}
	if record.SnapshotID == 0 && record.RootHash == "" {
		return "", indexCheckpointRecord{}, false
	}

	return contracts.JobIdentityKey(payload.Purpose, payload.SessionID, payload.RepoRoot), record, true
}

type IndexWorker struct {
	riverqueue.WorkerDefaults[indexJobArgs]

	run        indexRunner
	checkpoint indexApplyCheckpoint
	logger     *slog.Logger
	metrics    *observability.IndexSyncMetrics
	afterApply func(context.Context, contracts.IndexJobPayload, indexing.DeltaApplyResult) error
}

func NewIndexWorker(run indexRunner, logger *slog.Logger) *IndexWorker {
	return NewIndexWorkerWithCheckpoint(run, newMemoryIndexApplyCheckpoint(), logger)
}

func NewIndexWorkerWithCheckpoint(run indexRunner, checkpoint indexApplyCheckpoint, logger *slog.Logger) *IndexWorker {
	return &IndexWorker{
		run:        run,
		checkpoint: checkpoint,
		logger:     logger,
		metrics:    observability.DefaultIndexSyncMetrics(),
	}
}

func (w *IndexWorker) Work(ctx context.Context, job *riverqueue.Job[indexJobArgs]) error {
	startedAt := time.Now().UTC()
	outcome, err := w.runJob(ctx, job)
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
		return err
	default:
		outcome.Status = jobOutcomeStatusFailure
		outcome.Error = err.Error()
		w.logOutcome(outcome)
		w.recordMetrics(outcome)
		return riverqueue.JobCancel(err)
	}
}

func (w *IndexWorker) runJob(ctx context.Context, job *riverqueue.Job[indexJobArgs]) (JobOutcome, error) {
	outcome := newJobOutcome(kindIndexJob, job.Args.Payload.SessionID, job.Args.Payload.RepoRoot, job)

	if w == nil {
		return outcome, errWorkerConfiguration("index worker is nil")
	}
	if w.run == nil {
		return outcome, errWorkerConfiguration("index worker runner is required")
	}
	if w.checkpoint == nil {
		return outcome, errWorkerConfiguration("index worker checkpoint is required")
	}

	payload, err := normalizeIndexPayload(job.Args.Payload)
	if err != nil {
		return outcome, err
	}
	outcome.SessionID = payload.SessionID
	outcome.RepoRoot = payload.RepoRoot
	outcome.SnapshotID = payload.SnapshotID
	outcome.RootHash = payload.RootHash
	outcome.ChangedCount = len(payload.Delta.Changes)
	if !payload.RequestedAt.IsZero() {
		outcome.QueueLatency = time.Since(payload.RequestedAt)
	}

	if len(payload.Delta.Changes) == 0 {
		outcome.Message = "delta is empty; skipping apply"
		return outcome, nil
	}

	applied, err := w.checkpoint.IsApplied(ctx, payload)
	if err != nil {
		return outcome, fmt.Errorf("check applied delta checkpoint: %w", err)
	}
	if applied {
		outcome.Message = "delta already applied"
		return outcome, nil
	}

	result, err := w.run(ctx, payload)
	if err != nil {
		return outcome, fmt.Errorf("run index apply: %w", err)
	}
	if err := w.checkpoint.MarkApplied(ctx, payload); err != nil {
		return outcome, fmt.Errorf("mark applied delta checkpoint: %w", err)
	}
	if w.afterApply != nil {
		if err := w.afterApply(ctx, payload, result); err != nil {
			return outcome, fmt.Errorf("finalize index apply: %w", err)
		}
	}

	outcome.Message = fmt.Sprintf(
		"applied delta for %d files and %d chunks",
		result.FilesTouched,
		result.ChunksReplaced,
	)
	return outcome, nil
}

func (w *IndexWorker) logOutcome(outcome JobOutcome) {
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

func (w *IndexWorker) finishOutcome(outcome *JobOutcome, startedAt time.Time) {
	if outcome == nil {
		return
	}

	outcome.Duration = time.Since(startedAt)
	if outcome.JobID == 0 {
		outcome.JobID = jobIDFromAttempt(outcome.Attempt)
	}
}

func (w *IndexWorker) recordMetrics(outcome JobOutcome) {
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

func normalizeIndexPayload(payload contracts.IndexJobPayload) (contracts.IndexJobPayload, error) {
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.RepoRoot = normalizeRepoRoot(payload.RepoRoot)
	payload.Purpose = firstNonEmpty(payload.Purpose, contracts.JobPurposeIndexApplyDelta)
	payload.RootHash = strings.TrimSpace(payload.RootHash)
	if payload.RequestedAt.IsZero() {
		payload.RequestedAt = time.Now().UTC()
	}
	if payload.SessionID == "" {
		return payload, fmt.Errorf("index job session_id is required")
	}
	if payload.RepoRoot == "" {
		return payload, fmt.Errorf("index job repo_root is required")
	}
	return payload, nil
}
