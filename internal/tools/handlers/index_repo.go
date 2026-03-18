package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openai/openai-go/v3"

	"github.com/Nickbohm555/deep-agent-cli/internal/embeddings"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexing"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/platform/db"
	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

type IndexRepoInput struct{}

type indexRepoRunner func(context.Context, indexRepoStore, indexRepoEmbedder, string, string) (indexing.FullRebuildResult, error)

type indexRepoStore interface {
	ReplaceRepoIndex(context.Context, string, string, []indexstore.ChunkRecordInput) error
}

type indexRepoEmbedder interface {
	EmbedTexts(context.Context, []string) (embeddings.Result, error)
}

var (
	newIndexRepoPool     = db.NewPoolFromEnv
	newIndexRepoStore    = func(pool *pgxpool.Pool) indexRepoStore { return indexstore.New(pool) }
	newIndexRepoEmbedder = func() indexRepoEmbedder {
		client := openai.NewClient()
		return embeddings.NewClient(&client)
	}
	closeIndexRepoPool = func(pool *pgxpool.Pool) {
		if pool != nil {
			pool.Close()
		}
	}
	runIndexRepo indexRepoRunner = func(ctx context.Context, store indexRepoStore, embedder indexRepoEmbedder, sessionID, repoRoot string) (indexing.FullRebuildResult, error) {
		return indexing.RunFullRebuild(ctx, store, embedder, sessionID, repoRoot)
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

	rebuildResult, err := runIndexRepo(ctx, newIndexRepoStore(pool), newIndexRepoEmbedder(), sessionID, repoRoot)
	if err != nil {
		result.IsError = true
		return result, err
	}

	output, err := json.Marshal(struct {
		FilesIndexed   int    `json:"files_indexed"`
		ChunksEmbedded int    `json:"chunks_embedded"`
		Model          string `json:"model"`
		Dims           int    `json:"dims"`
	}{
		FilesIndexed:   rebuildResult.FilesIndexed,
		ChunksEmbedded: rebuildResult.ChunksEmbedded,
		Model:          rebuildResult.Model,
		Dims:           rebuildResult.Dimensions,
	})
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("marshal index_repo result: %w", err)
	}

	result.Output = string(output)
	return result, nil
}
