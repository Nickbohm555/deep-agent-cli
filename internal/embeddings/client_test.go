package embeddings

import (
	"context"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func TestEmbedTextsSuccess(t *testing.T) {
	client := NewClientWithService(fakeEmbeddingService{
		response: &openai.CreateEmbeddingResponse{
			Model: string(DefaultModel),
			Data: []openai.Embedding{
				{Embedding: []float64{1, 2, 3}},
				{Embedding: []float64{4, 5, 6}},
			},
		},
	}, Config{
		Model:      DefaultModel,
		Dimensions: 3,
	})

	result, err := client.EmbedTexts(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("EmbedTexts returned error: %v", err)
	}

	if result.Model != string(DefaultModel) {
		t.Fatalf("result model = %q, want %q", result.Model, DefaultModel)
	}
	if result.Dimensions != 3 {
		t.Fatalf("result dimensions = %d, want 3", result.Dimensions)
	}
	if len(result.Vectors) != 2 {
		t.Fatalf("result vector count = %d, want 2", len(result.Vectors))
	}
	if got := len(result.Vectors[0]); got != 3 {
		t.Fatalf("first vector dimensions = %d, want 3", got)
	}
}

func TestEmbedTextsDimensionMismatch(t *testing.T) {
	client := NewClientWithService(fakeEmbeddingService{
		response: &openai.CreateEmbeddingResponse{
			Model: string(DefaultModel),
			Data: []openai.Embedding{
				{Embedding: []float64{1, 2}},
			},
		},
	}, Config{
		Model:      DefaultModel,
		Dimensions: 3,
	})

	_, err := client.EmbedTexts(context.Background(), []string{"one"})
	if err == nil {
		t.Fatal("EmbedTexts returned nil error")
	}
	if !strings.Contains(err.Error(), "embedding dimensions mismatch") {
		t.Fatalf("error = %q, want dimension mismatch", err)
	}
}

type fakeEmbeddingService struct {
	response *openai.CreateEmbeddingResponse
	err      error
}

func (f fakeEmbeddingService) New(context.Context, openai.EmbeddingNewParams, ...option.RequestOption) (*openai.CreateEmbeddingResponse, error) {
	return f.response, f.err
}
