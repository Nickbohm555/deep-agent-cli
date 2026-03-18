package retrieval

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/Nickbohm555/deep-agent-cli/internal/embeddings"
)

type queryEmbeddingService interface {
	New(context.Context, openai.EmbeddingNewParams, ...option.RequestOption) (*openai.CreateEmbeddingResponse, error)
}

type queryTextEmbedder interface {
	EmbedTexts(context.Context, []string) (embeddings.Result, error)
}

func NewQueryEmbeddingClient(client *openai.Client) *embeddings.Client {
	return embeddings.NewClient(client)
}

func NewQueryEmbeddingClientWithService(service queryEmbeddingService) *embeddings.Client {
	return embeddings.NewClientWithService(service, embeddings.DefaultConfig())
}

func EmbedQuery(ctx context.Context, embedder queryTextEmbedder, query string) ([]float32, error) {
	if embedder == nil {
		return nil, fmt.Errorf("query embedder is not configured")
	}

	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return nil, fmt.Errorf("query is required")
	}

	result, err := embedder.EmbedTexts(ctx, []string{trimmedQuery})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(result.Vectors) != 1 {
		return nil, fmt.Errorf("embed query: embedding count mismatch: got %d vectors", len(result.Vectors))
	}
	if result.Dimensions <= 0 {
		return nil, fmt.Errorf("embed query: embedding dimensions must be positive")
	}

	vector := append([]float32(nil), result.Vectors[0]...)
	if len(vector) != result.Dimensions {
		return nil, fmt.Errorf(
			"embed query: embedding dimensions mismatch: got %d, want %d",
			len(vector),
			result.Dimensions,
		)
	}

	return vector, nil
}
