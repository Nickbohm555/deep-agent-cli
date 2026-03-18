package runtime_test

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	_ "unsafe"

	"github.com/Nickbohm555/deep-agent-cli/internal/retrieval"
	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:linkname linkedNewSemanticSearchPool github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers.newSemanticSearchPool
var linkedNewSemanticSearchPool func(context.Context) (*pgxpool.Pool, error)

//go:linkname linkedNewSemanticRetriever github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers.newSemanticRetriever
var linkedNewSemanticRetriever func(*pgxpool.Pool) (retrieval.SemanticRetriever, error)

//go:linkname linkedCloseSemanticSearchPool github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers.closeSemanticSearchPool
var linkedCloseSemanticSearchPool func(*pgxpool.Pool)

var retrievalLinePattern = regexp.MustCompile(`^(\d+)\. (.+) \| (.+) \| score=([0-9]+\.[0-9]+)$`)

func TestInternalAndToolRetrievalParity(t *testing.T) {
	restoreSemanticSearchFactories(t)

	sharedRetriever := retrieval.NewService(func(_ context.Context, req retrieval.SemanticQueryRequest) (retrieval.SemanticQueryResponse, error) {
		return retrieval.SemanticQueryResponse{
			SessionID: req.SessionID,
			RepoRoot:  req.RepoRoot,
			Query:     req.Query,
			TopK:      req.TopK,
			Index: retrieval.SemanticIndexReadiness{
				Ready:  true,
				Status: "ready",
			},
			Results: []retrieval.SemanticQueryResult{
				{
					Rank:     1,
					FilePath: "internal/tools/registry/static.go",
					ChunkID:  "internal/tools/registry/static.go#12",
					Score:    0.87312,
					Snippet:  "registry definition\nsnippet",
				},
				{
					Rank:     2,
					FilePath: "internal/runtime/orchestrator.go",
					ChunkID:  "internal/runtime/orchestrator.go#20",
					Score:    0.74203,
					Snippet:  "orchestrator evidence",
				},
			},
		}, nil
	})
	linkedNewSemanticSearchPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	linkedCloseSemanticSearchPool = func(*pgxpool.Pool) {}
	linkedNewSemanticRetriever = func(*pgxpool.Pool) (retrieval.SemanticRetriever, error) {
		return sharedRetriever, nil
	}

	ctx := bindSessionScope(t, "session-parity", t.TempDir())
	internalContext, ok, err := runtime.BuildRetrievalContext(ctx, sharedRetriever, " where is the registry defined? ")
	if err != nil {
		t.Fatalf("BuildRetrievalContext returned error: %v", err)
	}
	if !ok {
		t.Fatal("BuildRetrievalContext returned ok=false, want retrieval context")
	}

	toolResult, err := handlers.SemanticSearch(ctx, semanticSearchToolCall(t, handlers.SemanticSearchInput{
		Query: " where is the registry defined? ",
	}))
	if err != nil {
		t.Fatalf("SemanticSearch returned error: %v", err)
	}
	if toolResult.IsError {
		t.Fatalf("SemanticSearch marked result as error: %s", toolResult.Output)
	}

	var output semanticSearchParityOutput
	if err := json.Unmarshal([]byte(toolResult.Output), &output); err != nil {
		t.Fatalf("SemanticSearch output is not valid JSON: %v", err)
	}

	internalResults := parseInternalRetrievalResults(t, internalContext)
	if len(internalResults) != len(output.Results) {
		t.Fatalf("internal results len = %d, tool results len = %d", len(internalResults), len(output.Results))
	}

	for i := range output.Results {
		internal := internalResults[i]
		tool := output.Results[i]
		if internal.Rank != tool.Rank {
			t.Fatalf("result[%d] rank = %d, want %d", i, internal.Rank, tool.Rank)
		}
		if internal.ChunkID != tool.ChunkID {
			t.Fatalf("result[%d] chunk_id = %q, want %q", i, internal.ChunkID, tool.ChunkID)
		}
		if internal.FilePath != tool.FilePath {
			t.Fatalf("result[%d] file_path = %q, want %q", i, internal.FilePath, tool.FilePath)
		}
		if math.Abs(internal.Score-tool.Score) > 0.0001 {
			t.Fatalf("result[%d] score = %.4f, want %.4f within tolerance", i, internal.Score, tool.Score)
		}
	}
}

func TestInternalAndToolReadinessParity(t *testing.T) {
	restoreSemanticSearchFactories(t)

	sharedRetriever := retrieval.NewService(func(_ context.Context, req retrieval.SemanticQueryRequest) (retrieval.SemanticQueryResponse, error) {
		return retrieval.SemanticQueryResponse{
			SessionID: req.SessionID,
			RepoRoot:  req.RepoRoot,
			Query:     req.Query,
			TopK:      req.TopK,
			Index: retrieval.SemanticIndexReadiness{
				Ready:  false,
				Status: "index_not_ready",
			},
		}, nil
	})
	linkedNewSemanticSearchPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	linkedCloseSemanticSearchPool = func(*pgxpool.Pool) {}
	linkedNewSemanticRetriever = func(*pgxpool.Pool) (retrieval.SemanticRetriever, error) {
		return sharedRetriever, nil
	}

	ctx := bindSessionScope(t, "session-readiness", t.TempDir())
	internalContext, ok, err := runtime.BuildRetrievalContext(ctx, sharedRetriever, "show me the runtime architecture")
	if err != nil {
		t.Fatalf("BuildRetrievalContext returned error: %v", err)
	}
	if !ok {
		t.Fatal("BuildRetrievalContext returned ok=false, want retrieval context")
	}

	toolResult, err := handlers.SemanticSearch(ctx, semanticSearchToolCall(t, handlers.SemanticSearchInput{
		Query: "show me the runtime architecture",
	}))
	if err != nil {
		t.Fatalf("SemanticSearch returned error: %v", err)
	}
	if toolResult.IsError {
		t.Fatalf("SemanticSearch marked result as error: %s", toolResult.Output)
	}

	var output semanticSearchParityOutput
	if err := json.Unmarshal([]byte(toolResult.Output), &output); err != nil {
		t.Fatalf("SemanticSearch output is not valid JSON: %v", err)
	}

	if internalStatus := parseInternalRetrievalStatus(t, internalContext); internalStatus != output.Index.Status {
		t.Fatalf("internal status = %q, want %q", internalStatus, output.Index.Status)
	}
	if !strings.Contains(internalContext, "No retrieval evidence was attached because the semantic index is not ready.") {
		t.Fatalf("internal context missing not-ready explanation: %q", internalContext)
	}
	if output.Index.Ready {
		t.Fatalf("tool readiness = %+v, want not ready", output.Index)
	}
	if output.Status != "index_not_ready" {
		t.Fatalf("tool status = %q, want index_not_ready", output.Status)
	}
	if len(output.Results) != 0 {
		t.Fatalf("tool results len = %d, want 0", len(output.Results))
	}
}

type semanticSearchParityOutput struct {
	Status  string                           `json:"status"`
	Index   retrieval.SemanticIndexReadiness `json:"index"`
	Results []retrieval.SemanticQueryResult  `json:"results"`
}

func restoreSemanticSearchFactories(t *testing.T) {
	t.Helper()

	originalPool := linkedNewSemanticSearchPool
	originalRetriever := linkedNewSemanticRetriever
	originalClose := linkedCloseSemanticSearchPool
	t.Cleanup(func() {
		linkedNewSemanticSearchPool = originalPool
		linkedNewSemanticRetriever = originalRetriever
		linkedCloseSemanticSearchPool = originalClose
	})
}

func bindSessionScope(t *testing.T, sessionID, repoRoot string) context.Context {
	t.Helper()

	ctx, err := runtime.WithRepoRoot(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}
	return runtime.WithSessionID(ctx, sessionID)
}

func semanticSearchToolCall(t *testing.T, input handlers.SemanticSearchInput) runtime.ToolCall {
	t.Helper()

	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	return runtime.ToolCall{
		ID:        "call-1",
		Name:      "semantic_search",
		Arguments: raw,
	}
}

func parseInternalRetrievalStatus(t *testing.T, retrievalContext string) string {
	t.Helper()

	for _, line := range strings.Split(retrievalContext, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Index status: ") {
			return strings.TrimSuffix(strings.TrimPrefix(trimmed, "Index status: "), ".")
		}
	}

	t.Fatalf("retrieval status line not found in %q", retrievalContext)
	return ""
}

func parseInternalRetrievalResults(t *testing.T, retrievalContext string) []retrieval.SemanticQueryResult {
	t.Helper()

	lines := strings.Split(retrievalContext, "\n")
	results := make([]retrieval.SemanticQueryResult, 0)
	for _, line := range lines {
		matches := retrievalLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if matches == nil {
			continue
		}

		rank, err := strconv.Atoi(matches[1])
		if err != nil {
			t.Fatalf("parse rank %q: %v", matches[1], err)
		}
		score, err := strconv.ParseFloat(matches[4], 64)
		if err != nil {
			t.Fatalf("parse score %q: %v", matches[4], err)
		}

		results = append(results, retrieval.SemanticQueryResult{
			Rank:     rank,
			FilePath: matches[2],
			ChunkID:  matches[3],
			Score:    score,
		})
	}
	return results
}
