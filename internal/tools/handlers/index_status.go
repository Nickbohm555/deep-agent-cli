package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	indexstatus "github.com/Nickbohm555/deep-agent-cli/internal/indexsync/status"
	riverjobs "github.com/Nickbohm555/deep-agent-cli/internal/jobs/river"
	"github.com/Nickbohm555/deep-agent-cli/internal/platform/db"
	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IndexStatusInput struct{}

type indexStatusService interface {
	GetIndexSyncStatus(context.Context, string, string) (indexstatus.Status, error)
}

var (
	newIndexStatusPool    = db.NewPoolFromEnv
	newIndexStatusService = func(pool *pgxpool.Pool) (indexStatusService, error) {
		jobClient, err := riverjobs.NewClient(pool, riverjobs.Config{})
		if err != nil {
			return nil, err
		}
		return indexstatus.NewService(indexstore.New(pool), jobClient), nil
	}
	closeIndexStatusPool = func(pool *pgxpool.Pool) {
		if pool != nil {
			pool.Close()
		}
	}
)

func IndexStatus(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
	result := runtime.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	var input IndexStatusInput
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

	pool, err := newIndexStatusPool(ctx)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("initialize index_status dependencies: %w", err)
	}
	defer closeIndexStatusPool(pool)

	service, err := newIndexStatusService(pool)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("initialize index_status service: %w", err)
	}

	statusPayload, err := service.GetIndexSyncStatus(ctx, sessionID, repoRoot)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("load index_status state: %w", err)
	}

	output, err := json.Marshal(struct {
		Summary string             `json:"summary"`
		Status  indexstatus.Status `json:"status"`
	}{
		Summary: runtime.RenderIndexStatusSummary(statusPayload),
		Status:  statusPayload,
	})
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("marshal index_status result: %w", err)
	}

	result.Output = string(output)
	return result, nil
}
