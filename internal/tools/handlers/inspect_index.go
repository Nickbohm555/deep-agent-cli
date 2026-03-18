package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/platform/db"
	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

type InspectIndexInput struct {
	Limit int `json:"limit" jsonschema_description:"Maximum number of index rows to return. Use 0 to return all rows."`
}

type inspectIndexStore interface {
	ListRepoIndex(context.Context, string, string) ([]indexstore.ChunkRecord, error)
}

var (
	newInspectIndexPool   = db.NewPoolFromEnv
	newInspectIndexStore  = func(pool *pgxpool.Pool) inspectIndexStore { return indexstore.New(pool) }
	closeInspectIndexPool = func(pool *pgxpool.Pool) {
		if pool != nil {
			pool.Close()
		}
	}
)

func InspectIndex(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
	result := runtime.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	var input InspectIndexInput
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		result.IsError = true
		return result, err
	}
	if input.Limit < 0 {
		result.IsError = true
		return result, fmt.Errorf("limit must be greater than or equal to 0")
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

	pool, err := newInspectIndexPool(ctx)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("initialize inspect_index dependencies: %w", err)
	}
	defer closeInspectIndexPool(pool)

	rows, err := newInspectIndexStore(pool).ListRepoIndex(ctx, sessionID, repoRoot)
	if err != nil {
		result.IsError = true
		return result, err
	}

	if input.Limit > 0 && len(rows) > input.Limit {
		rows = rows[:input.Limit]
	}

	type inspectIndexRow struct {
		RepoRoot      string `json:"repo_root"`
		RelPath       string `json:"rel_path"`
		ChunkIndex    int    `json:"chunk_index"`
		Model         string `json:"model"`
		EmbeddingDims int    `json:"embedding_dims"`
		ContentHash   string `json:"content_hash"`
	}

	outputRows := make([]inspectIndexRow, 0, len(rows))
	for _, row := range rows {
		outputRows = append(outputRows, inspectIndexRow{
			RepoRoot:      row.RepoRoot,
			RelPath:       row.RelPath,
			ChunkIndex:    row.ChunkIndex,
			Model:         row.Model,
			EmbeddingDims: row.EmbeddingDims,
			ContentHash:   row.ContentHash,
		})
	}

	output, err := json.Marshal(outputRows)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("marshal inspect_index result: %w", err)
	}

	result.Output = string(output)
	return result, nil
}
