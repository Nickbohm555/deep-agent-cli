package river

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexing"
	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	riverqueue "github.com/riverqueue/river"
)

type indexRunner func(context.Context, string, string) (indexing.FullRebuildResult, error)

type IndexWorker struct {
	riverqueue.WorkerDefaults[indexJobArgs]

	run    indexRunner
	logger *slog.Logger
}

func NewIndexWorker(run indexRunner, logger *slog.Logger) *IndexWorker {
	return &IndexWorker{
		run:    run,
		logger: logger,
	}
}

func (w *IndexWorker) Work(ctx context.Context, job *riverqueue.Job[indexJobArgs]) error {
	outcome, err := w.runJob(ctx, job)
	switch {
	case err == nil:
		w.logOutcome(outcome)
		return nil
	case isRetryableWorkerError(err):
		outcome.Status = jobOutcomeStatusRetry
		outcome.Error = err.Error()
		w.logOutcome(outcome)
		return err
	default:
		outcome.Status = jobOutcomeStatusFailure
		outcome.Error = err.Error()
		w.logOutcome(outcome)
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

	payload, err := normalizeIndexPayload(job.Args.Payload)
	if err != nil {
		return outcome, err
	}
	outcome.SessionID = payload.SessionID
	outcome.RepoRoot = payload.RepoRoot
	outcome.SnapshotID = payload.SnapshotID
	outcome.RootHash = payload.RootHash
	outcome.ChangedCount = len(payload.Delta.Changes)

	if len(payload.Delta.Changes) == 0 {
		outcome.Message = "delta is empty; skipping rebuild"
		return outcome, nil
	}

	result, err := w.run(ctx, payload.SessionID, payload.RepoRoot)
	if err != nil {
		return outcome, fmt.Errorf("run index apply: %w", err)
	}

	outcome.Message = fmt.Sprintf(
		"rebuilt index for %d files and %d chunks",
		result.FilesIndexed,
		result.ChunksEmbedded,
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
		"session_id", outcome.SessionID,
		"repo_root", outcome.RepoRoot,
		"attempt", outcome.Attempt,
		"snapshot_id", outcome.SnapshotID,
		"root_hash", outcome.RootHash,
		"changed_count", outcome.ChangedCount,
	}
	if outcome.Message != "" {
		attrs = append(attrs, "message", outcome.Message)
	}
	if outcome.Error != "" {
		attrs = append(attrs, "error", outcome.Error)
	}
	logger.Info("river worker outcome", attrs...)
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
