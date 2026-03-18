package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/Nickbohm555/deep-agent-cli/internal/platform/db"
	"github.com/Nickbohm555/deep-agent-cli/internal/retrieval"
	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SemanticSearchInput struct {
	Query string `json:"query" jsonschema_description:"Natural-language semantic search query for the current session-bound repository."`
	TopK  int    `json:"top_k,omitempty" jsonschema_description:"Optional number of ranked results to return. Defaults to 5 and is capped at 50."`
}

type semanticIndexSnapshotReader interface {
	LoadLatestSnapshot(context.Context, string, string) (*indexstore.SnapshotState, error)
}

type semanticSearchOutput struct {
	Status    string                           `json:"status"`
	SessionID string                           `json:"session_id"`
	RepoRoot  string                           `json:"repo_root"`
	Query     string                           `json:"query"`
	TopK      int                              `json:"top_k"`
	Index     retrieval.SemanticIndexReadiness `json:"index"`
	Results   []retrieval.SemanticQueryResult  `json:"results"`
}

type semanticIndexReadinessChecker struct {
	snapshots semanticIndexSnapshotReader
}

var (
	newSemanticSearchPool = db.NewPoolFromEnv
	newSemanticRetriever  = func(pool *pgxpool.Pool) (retrieval.SemanticRetriever, error) {
		if pool == nil {
			return nil, fmt.Errorf("database pool is required")
		}

		store := indexstore.New(pool)
		openAIClient := openai.NewClient()
		return retrieval.NewOrchestratedService(
			semanticIndexReadinessChecker{snapshots: store},
			retrieval.NewQueryEmbeddingClient(&openAIClient),
			retrieval.NewStore(pool),
		), nil
	}
	closeSemanticSearchPool = func(pool *pgxpool.Pool) {
		if pool != nil {
			pool.Close()
		}
	}
)

func SemanticSearch(ctx context.Context, call runtime.ToolCall) (runtime.ToolResult, error) {
	result := runtime.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	var input SemanticSearchInput
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		result.IsError = true
		return result, err
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		err := fmt.Errorf("query is required")
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

	pool, err := newSemanticSearchPool(ctx)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("initialize semantic_search dependencies: %w", err)
	}
	defer closeSemanticSearchPool(pool)

	retriever, err := newSemanticRetriever(pool)
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("initialize semantic_search retriever: %w", err)
	}

	resp, err := retriever.Query(ctx, retrieval.SemanticQueryRequest{
		SessionID: sessionID,
		RepoRoot:  repoRoot,
		Query:     query,
		TopK:      normalizeSemanticSearchTopK(input.TopK),
	})
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("run semantic_search query: %w", err)
	}

	output, err := json.Marshal(semanticSearchOutput{
		Status:    resp.Index.Status,
		SessionID: resp.SessionID,
		RepoRoot:  resp.RepoRoot,
		Query:     resp.Query,
		TopK:      resp.TopK,
		Index:     resp.Index,
		Results:   resp.Results,
	})
	if err != nil {
		result.IsError = true
		return result, fmt.Errorf("marshal semantic_search result: %w", err)
	}

	result.Output = string(output)
	return result, nil
}

func (c semanticIndexReadinessChecker) GetReadiness(ctx context.Context, sessionID, repoRoot string) (retrieval.SemanticIndexReadiness, error) {
	if c.snapshots == nil {
		return retrieval.SemanticIndexReadiness{}, fmt.Errorf("snapshot store is not configured")
	}

	state, err := c.snapshots.LoadLatestSnapshot(ctx, sessionID, repoRoot)
	if err != nil {
		return retrieval.SemanticIndexReadiness{}, fmt.Errorf("load latest snapshot: %w", err)
	}
	if state == nil {
		return retrieval.SemanticIndexReadiness{
			Ready:  false,
			Status: "index_not_ready",
		}, nil
	}

	readiness := retrieval.SemanticIndexReadiness{}
	if state.Root.ID != 0 {
		readiness.SnapshotID = &state.Root.ID
	}
	if state.Root.CompletedAt != nil {
		readiness.UpdatedAt = state.Root.CompletedAt
	}

	if state.Root.IsActive && state.Root.CompletedAt != nil && state.Root.Status == indexsync.SnapshotStatusActive {
		readiness.Ready = true
		readiness.Status = "ready"
		return readiness, nil
	}

	readiness.Ready = false
	readiness.Status = strings.TrimSpace(string(state.Root.Status))
	if readiness.Status == "" {
		readiness.Status = "index_not_ready"
	}
	return readiness, nil
}

func normalizeSemanticSearchTopK(topK int) int {
	switch {
	case topK <= 0:
		return retrieval.DefaultTopK
	case topK > retrieval.MaxStoreTopK:
		return retrieval.MaxStoreTopK
	default:
		return topK
	}
}
