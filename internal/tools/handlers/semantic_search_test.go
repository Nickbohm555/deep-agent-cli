package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/retrieval"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSemanticSearchToolReturnsRankedEvidence(t *testing.T) {
	restorePool := newSemanticSearchPool
	restoreRetriever := newSemanticRetriever
	restoreClose := closeSemanticSearchPool
	t.Cleanup(func() {
		newSemanticSearchPool = restorePool
		newSemanticRetriever = restoreRetriever
		closeSemanticSearchPool = restoreClose
	})

	newSemanticSearchPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	closeSemanticSearchPool = func(*pgxpool.Pool) {}

	updatedAt := time.Unix(1700000000, 0).UTC()
	var captured retrieval.SemanticQueryRequest
	newSemanticRetriever = func(*pgxpool.Pool) (retrieval.SemanticRetriever, error) {
		return retrieval.NewService(func(_ context.Context, req retrieval.SemanticQueryRequest) (retrieval.SemanticQueryResponse, error) {
			captured = req
			return retrieval.SemanticQueryResponse{
				SessionID: req.SessionID,
				RepoRoot:  req.RepoRoot,
				Query:     req.Query,
				TopK:      req.TopK,
				Index: retrieval.SemanticIndexReadiness{
					Ready:      true,
					Status:     "ready",
					SnapshotID: int64Ptr(44),
					UpdatedAt:  &updatedAt,
				},
				Results: []retrieval.SemanticQueryResult{
					{
						Rank:     1,
						FilePath: "internal/tools/registry/static.go",
						ChunkID:  "internal/tools/registry/static.go#12",
						Score:    0.87,
						Snippet:  "registry definition snippet",
					},
					{
						Rank:     2,
						FilePath: "internal/runtime/orchestrator.go",
						ChunkID:  "internal/runtime/orchestrator.go#8",
						Score:    0.74,
						Snippet:  "orchestrator snippet",
					},
				},
			}, nil
		}), nil
	}

	ctx := mustBindSessionScope(t, "session-semantic", t.TempDir())
	result, err := SemanticSearch(ctx, toolCall(t, "semantic_search", SemanticSearchInput{
		Query: " where is the tool registry defined ",
		TopK:  retrieval.MaxStoreTopK + 5,
	}))
	if err != nil {
		t.Fatalf("SemanticSearch returned error: %v", err)
	}
	if result.IsError {
		t.Fatal("SemanticSearch unexpectedly marked result as error")
	}

	if captured.SessionID != "session-semantic" {
		t.Fatalf("captured session_id = %q, want session-semantic", captured.SessionID)
	}
	if captured.Query != "where is the tool registry defined" {
		t.Fatalf("captured query = %q, want trimmed query", captured.Query)
	}
	if captured.TopK != retrieval.MaxStoreTopK {
		t.Fatalf("captured top_k = %d, want capped %d", captured.TopK, retrieval.MaxStoreTopK)
	}

	var output semanticSearchOutput
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatalf("SemanticSearch output is not valid JSON: %v", err)
	}
	if output.Status != "ready" {
		t.Fatalf("status = %q, want ready", output.Status)
	}
	if output.Query != "where is the tool registry defined" {
		t.Fatalf("query = %q, want trimmed query", output.Query)
	}
	if output.TopK != retrieval.MaxStoreTopK {
		t.Fatalf("top_k = %d, want capped %d", output.TopK, retrieval.MaxStoreTopK)
	}
	if !output.Index.Ready || output.Index.SnapshotID == nil || *output.Index.SnapshotID != 44 {
		t.Fatalf("index readiness = %+v, want ready snapshot metadata", output.Index)
	}
	if len(output.Results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(output.Results))
	}
	if output.Results[0].Rank != 1 || output.Results[1].Rank != 2 {
		t.Fatalf("result ranks = %+v, want stable ranked evidence", output.Results)
	}
}

func TestSemanticSearchToolIncludesFilePathAndScore(t *testing.T) {
	restorePool := newSemanticSearchPool
	restoreRetriever := newSemanticRetriever
	restoreClose := closeSemanticSearchPool
	t.Cleanup(func() {
		newSemanticSearchPool = restorePool
		newSemanticRetriever = restoreRetriever
		closeSemanticSearchPool = restoreClose
	})

	newSemanticSearchPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	closeSemanticSearchPool = func(*pgxpool.Pool) {}
	newSemanticRetriever = func(*pgxpool.Pool) (retrieval.SemanticRetriever, error) {
		return retrieval.NewService(func(_ context.Context, req retrieval.SemanticQueryRequest) (retrieval.SemanticQueryResponse, error) {
			return retrieval.SemanticQueryResponse{
				SessionID: req.SessionID,
				RepoRoot:  req.RepoRoot,
				Query:     req.Query,
				TopK:      req.TopK,
				Index: retrieval.SemanticIndexReadiness{
					Ready:  false,
					Status: "index_not_ready",
				},
				Results: []retrieval.SemanticQueryResult{
					{
						Rank:     1,
						FilePath: "docs/runtime-architecture.md",
						ChunkID:  "docs/runtime-architecture.md#3",
						Score:    0.63,
						Snippet:  "retrieval architecture",
					},
				},
			}, nil
		}), nil
	}

	ctx := mustBindSessionScope(t, "session-semantic", t.TempDir())
	result, err := SemanticSearch(ctx, toolCall(t, "semantic_search", SemanticSearchInput{
		Query: "architecture",
	}))
	if err != nil {
		t.Fatalf("SemanticSearch returned error: %v", err)
	}

	var output semanticSearchOutput
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatalf("SemanticSearch output is not valid JSON: %v", err)
	}
	if output.TopK != retrieval.DefaultTopK {
		t.Fatalf("top_k = %d, want default %d", output.TopK, retrieval.DefaultTopK)
	}
	if len(output.Results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(output.Results))
	}

	evidence := output.Results[0]
	if evidence.FilePath != "docs/runtime-architecture.md" {
		t.Fatalf("file_path = %q, want docs/runtime-architecture.md", evidence.FilePath)
	}
	if evidence.Score != 0.63 {
		t.Fatalf("score = %v, want 0.63", evidence.Score)
	}
	if evidence.ChunkID == "" || evidence.Snippet == "" {
		t.Fatalf("evidence = %+v, want non-empty chunk_id and snippet", evidence)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
