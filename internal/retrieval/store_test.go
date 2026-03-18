package retrieval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/embeddings"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func TestEmbedQueryUsesPinnedModel(t *testing.T) {
	t.Parallel()

	service := &captureEmbeddingService{
		response: &openai.CreateEmbeddingResponse{
			Model: string(embeddings.DefaultModel),
			Data: []openai.Embedding{
				{Embedding: floatVector(indexstore.DefaultEmbeddingDimensions)},
			},
		},
	}

	vector, err := EmbedQuery(context.Background(), NewQueryEmbeddingClientWithService(service), "  where is the registry  ")
	if err != nil {
		t.Fatalf("EmbedQuery returned error: %v", err)
	}

	if len(vector) != indexstore.DefaultEmbeddingDimensions {
		t.Fatalf("vector dimensions = %d, want %d", len(vector), indexstore.DefaultEmbeddingDimensions)
	}
	if service.captured.Model != embeddings.DefaultModel {
		t.Fatalf("model = %q, want %q", service.captured.Model, embeddings.DefaultModel)
	}
	if service.captured.Dimensions.Value != int64(indexstore.DefaultEmbeddingDimensions) {
		t.Fatalf("dimensions = %d, want %d", service.captured.Dimensions.Value, indexstore.DefaultEmbeddingDimensions)
	}
	if got := service.captured.Input.OfArrayOfStrings; len(got) != 1 || got[0] != "where is the registry" {
		t.Fatalf("input = %#v, want trimmed single query", got)
	}
}

func TestQueryTopKScopedBySessionAndRepo(t *testing.T) {
	t.Parallel()

	longContent := strings.Repeat("snippet-", 80)
	store := &Store{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if sql != queryTopKSQL {
				return nil, fmt.Errorf("unexpected query: %s", sql)
			}
			if !strings.Contains(sql, "WHERE session_id = $1") || !strings.Contains(sql, "repo_root = $2") {
				return nil, fmt.Errorf("query missing scoped filters: %s", sql)
			}
			if args[0] != "session-1" || args[1] != "/repo/project" {
				return nil, fmt.Errorf("unexpected scope args: %#v", args[:2])
			}
			if args[2] != "[0.1,0.2,0.3]" {
				return nil, fmt.Errorf("unexpected vector arg: %v", args[2])
			}
			if args[3] != MaxStoreTopK {
				return nil, fmt.Errorf("limit = %v, want %d", args[3], MaxStoreTopK)
			}

			return &fakeRetrievalRows{
				values: [][]any{
					{"internal/tools/registry/static.go", 12, longContent, 0.1},
					{"internal/runtime/orchestrator.go", 3, "orchestrator snippet", 0.25},
				},
			}, nil
		},
	}

	results, err := store.QueryTopK(context.Background(), SemanticQueryRequest{
		SessionID: " session-1 ",
		RepoRoot:  " /repo/project ",
		TopK:      MaxStoreTopK + 10,
	}, []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Fatalf("QueryTopK returned error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].FilePath != "internal/tools/registry/static.go" || results[0].ChunkID != "internal/tools/registry/static.go#12" {
		t.Fatalf("results[0] identifiers = %+v", results[0])
	}
	if results[0].Score != 0.9 {
		t.Fatalf("results[0].Score = %v, want 0.9", results[0].Score)
	}
	if len(results[0].Snippet) != MaxSnippetLength {
		t.Fatalf("results[0].Snippet length = %d, want %d", len(results[0].Snippet), MaxSnippetLength)
	}
	if results[1].FilePath != "internal/runtime/orchestrator.go" || results[1].Score != 0.75 {
		t.Fatalf("results[1] = %+v, want ordered second row with converted score", results[1])
	}
}

type captureEmbeddingService struct {
	response *openai.CreateEmbeddingResponse
	err      error
	captured openai.EmbeddingNewParams
}

func (c *captureEmbeddingService) New(_ context.Context, params openai.EmbeddingNewParams, _ ...option.RequestOption) (*openai.CreateEmbeddingResponse, error) {
	c.captured = params
	return c.response, c.err
}

type fakeRetrievalRows struct {
	values [][]any
	index  int
	err    error
}

func (f *fakeRetrievalRows) Close() {}

func (f *fakeRetrievalRows) Err() error {
	return f.err
}

func (f *fakeRetrievalRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (f *fakeRetrievalRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (f *fakeRetrievalRows) Next() bool {
	if f.index >= len(f.values) {
		return false
	}
	f.index++
	return true
}

func (f *fakeRetrievalRows) Scan(dest ...any) error {
	row := f.values[f.index-1]
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = row[i].(string)
		case *int:
			*target = row[i].(int)
		case *float64:
			*target = row[i].(float64)
		case *int64:
			*target = row[i].(int64)
		case *time.Time:
			*target = row[i].(time.Time)
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	return nil
}

func (f *fakeRetrievalRows) Values() ([]any, error) {
	if f.index == 0 || f.index > len(f.values) {
		return nil, errors.New("no current row")
	}
	return f.values[f.index-1], nil
}

func (f *fakeRetrievalRows) RawValues() [][]byte {
	return nil
}

func (f *fakeRetrievalRows) Conn() *pgx.Conn {
	return nil
}

func floatVector(size int) []float64 {
	vector := make([]float64, size)
	for i := range vector {
		vector[i] = float64(i + 1)
	}
	return vector
}
