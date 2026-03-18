package status

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
)

type snapshotReader interface {
	LoadLatestSnapshot(context.Context, string, string) (*indexstore.SnapshotState, error)
}

type jobReader interface {
	ListJobs(context.Context, string, string) ([]JobRecord, error)
}

type Service struct {
	snapshots snapshotReader
	jobs      jobReader
}

func NewService(snapshots snapshotReader, jobs jobReader) *Service {
	return &Service{
		snapshots: snapshots,
		jobs:      jobs,
	}
}

func (s *Service) GetIndexSyncStatus(ctx context.Context, sessionID, repoRoot string) (Status, error) {
	scopeSessionID := strings.TrimSpace(sessionID)
	scopeRepoRoot := strings.TrimSpace(repoRoot)
	if scopeSessionID == "" {
		return Status{}, fmt.Errorf("session_id is required")
	}
	if scopeRepoRoot == "" {
		return Status{}, fmt.Errorf("repo_root is required")
	}

	status := Status{
		SessionID: scopeSessionID,
		RepoRoot:  scopeRepoRoot,
	}

	if s != nil && s.snapshots != nil {
		snapshotState, err := s.snapshots.LoadLatestSnapshot(ctx, scopeSessionID, scopeRepoRoot)
		if err != nil {
			return Status{}, fmt.Errorf("load latest snapshot status: %w", err)
		}
		if snapshotState != nil {
			status.LatestSnapshot = SnapshotInfo{
				ID:          snapshotState.Root.ID,
				RootHash:    snapshotState.Root.RootHash,
				Status:      string(snapshotState.Root.Status),
				CompletedAt: snapshotState.Root.CompletedAt,
			}
			if snapshotState.Root.CompletedAt != nil && snapshotState.Root.IsActive {
				status.LastSuccessfulSyncAt = snapshotState.Root.CompletedAt
			}
		}
	}

	if s == nil || s.jobs == nil {
		return status, nil
	}

	jobRecords, err := s.jobs.ListJobs(ctx, scopeSessionID, scopeRepoRoot)
	if err != nil {
		return Status{}, fmt.Errorf("load job status: %w", err)
	}

	for _, job := range jobRecords {
		switch job.State {
		case JobStatePending, JobStateRetryable:
			incrementPending(&status.Queue, job.Kind)
		case JobStateRunning:
			incrementRunning(&status.Queue, job.Kind)
			assignRunning(&status.Running, job)
		case JobStateSucceeded:
			assignSuccess(&status, job)
		case JobStateFailed:
			assignLatestError(&status, job)
		}

		if job.State == JobStateRetryable {
			assignLatestError(&status, job)
		}
	}

	return status, nil
}

func incrementPending(counts *QueueCounts, kind JobKind) {
	switch kind {
	case JobKindSync:
		counts.PendingSyncJobs++
	case JobKindIndex:
		counts.PendingIndexJobs++
	}
}

func incrementRunning(counts *QueueCounts, kind JobKind) {
	switch kind {
	case JobKindSync:
		counts.RunningSyncJobs++
	case JobKindIndex:
		counts.RunningIndexJobs++
	}
}

func assignRunning(running *RunningJobs, job JobRecord) {
	summary := jobSummary(job)
	switch job.Kind {
	case JobKindSync:
		if running.Sync == nil || laterJob(job, running.Sync) {
			running.Sync = &summary
		}
	case JobKindIndex:
		if running.Index == nil || laterJob(job, running.Index) {
			running.Index = &summary
		}
	}
}

func assignSuccess(status *Status, job JobRecord) {
	finishedAt := effectiveJobTime(job)
	if finishedAt == nil {
		return
	}

	switch job.Kind {
	case JobKindSync:
		if status.LastSuccessfulSyncAt == nil || finishedAt.After(*status.LastSuccessfulSyncAt) {
			status.LastSuccessfulSyncAt = finishedAt
		}
	case JobKindIndex:
		if status.LastSuccessfulIndexAt == nil || finishedAt.After(*status.LastSuccessfulIndexAt) {
			status.LastSuccessfulIndexAt = finishedAt
			status.LastDeltaSize = job.DeltaSize
		}
	}
}

func assignLatestError(status *Status, job JobRecord) {
	if strings.TrimSpace(job.Error) == "" {
		return
	}

	candidateTime := effectiveJobTime(job)
	if status.LatestError != nil {
		currentTime := status.LatestError.OccurredAt
		switch {
		case candidateTime == nil:
			return
		case currentTime != nil && !candidateTime.After(*currentTime):
			return
		}
	}

	status.LatestError = &ErrorSummary{
		JobID:      job.JobID,
		Kind:       job.Kind,
		State:      job.State,
		Message:    strings.TrimSpace(job.Error),
		OccurredAt: candidateTime,
		Attempt:    job.Attempt,
		SnapshotID: job.SnapshotID,
		RootHash:   strings.TrimSpace(job.RootHash),
	}
}

func jobSummary(job JobRecord) JobSummary {
	return JobSummary{
		JobID:      job.JobID,
		Kind:       job.Kind,
		State:      job.State,
		Attempt:    job.Attempt,
		EnqueuedAt: job.EnqueuedAt,
		StartedAt:  job.StartedAt,
		SnapshotID: job.SnapshotID,
		RootHash:   strings.TrimSpace(job.RootHash),
		DeltaSize:  job.DeltaSize,
	}
}

func laterJob(job JobRecord, existing *JobSummary) bool {
	return jobTimeOrZero(job.StartedAt, job.EnqueuedAt).After(jobTimeOrZero(existing.StartedAt, existing.EnqueuedAt))
}

func effectiveJobTime(job JobRecord) *time.Time {
	switch {
	case job.FinishedAt != nil:
		return job.FinishedAt
	case job.StartedAt != nil:
		return job.StartedAt
	default:
		return job.EnqueuedAt
	}
}

func jobTimeOrZero(primary, fallback *time.Time) time.Time {
	switch {
	case primary != nil:
		return *primary
	case fallback != nil:
		return *fallback
	default:
		return time.Time{}
	}
}
