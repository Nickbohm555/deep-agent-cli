package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	riverjobs "github.com/Nickbohm555/deep-agent-cli/internal/jobs/river"
	"github.com/Nickbohm555/deep-agent-cli/internal/platform/db"
	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IndexRepoInput struct{}

type indexRepoJobClient interface {
	EnqueueSyncJob(context.Context, contracts.SyncJobPayload) (riverjobs.EnqueueResult, error)
}

var (
	newIndexRepoPool      = db.NewPoolFromEnv
	newIndexRepoJobClient = func(pool *pgxpool.Pool) (indexRepoJobClient, error) {
		return riverjobs.NewClient(pool, riverjobs.Config{})
	}
	closeIndexRepoPool = func(pool *pgxpool.Pool) {
		if pool != nil {
			pool.Close()
		}
	}
)

func IndexRepo(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
	result := runtime.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	var input IndexRepoInput
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		result.IsError = true
		return result, err
	}

	sessionID, err := runtime.SessionIDFromContext(ctx)
	if err != nil {
		result.IsError = true
		return result, err
	}

	repoRoot, err := runtime.RepoRootFromContext(ctx)
	if err != nil {
		result.IsError = true
		return result, err
	}

	pool, err := newIndexRepoPool(ctx)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("initialize index_repo dependencies: %w", err)
	}
	defer closeIndexRepoPool(pool)

	jobClient, err := newIndexRepoJobClient(pool)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("initialize index_repo queue client: %w", err)
	}

	enqueueResult, err := jobClient.EnqueueSyncJob(ctx, contracts.SyncJobPayload{
		SessionID: sessionID,
		RepoRoot:  repoRoot,
		Purpose:   contracts.JobPurposeSyncScan,
		Trigger:   "tool:index_repo",
	})
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("enqueue index_repo sync job: %w", err)
	}

	output, err := json.Marshal(struct {
		JobID     int64  `json:"job_id"`
		Duplicate bool   `json:"duplicate"`
		Status    string `json:"status"`
		Message   string `json:"message"`
	}{
		JobID:     enqueueResult.JobID,
		Duplicate: enqueueResult.Duplicate,
		Status:    queuedStatus(enqueueResult.Duplicate),
		Message:   queuedMessage(enqueueResult.Duplicate),
	})
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("marshal index_repo result: %w", err)
	}

	result.Output = string(output)
	return result, nil
}

func queuedStatus(duplicate bool) string {
	if duplicate {
		return "already_queued"
	}
	return "queued"
}

func queuedMessage(duplicate bool) string {
	if duplicate {
		return "Repository sync is already queued in the background."
	}
	return "Repository sync started in the background."
}
