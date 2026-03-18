package embeddings

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
)

const DefaultModel = openai.EmbeddingModelTextEmbedding3Small

type embeddingService interface {
	New(context.Context, openai.EmbeddingNewParams, ...option.RequestOption) (*openai.CreateEmbeddingResponse, error)
}

type Config struct {
	Model      openai.EmbeddingModel
	Dimensions int
}

type Client struct {
	service    embeddingService
	model      openai.EmbeddingModel
	dimensions int
}

type Result struct {
	Model      string
	Dimensions int
	Vectors    [][]float32
}

func DefaultConfig() Config {
	return Config{
		Model:      DefaultModel,
		Dimensions: indexstore.DefaultEmbeddingDimensions,
	}
}

func NewClient(client *openai.Client) *Client {
	if client == nil {
		newClient := openai.NewClient()
		client = &newClient
	}

	return NewClientWithService(&client.Embeddings, DefaultConfig())
}

func NewClientWithService(service embeddingService, cfg Config) *Client {
	resolved := DefaultConfig()
	if cfg.Model != "" {
		resolved.Model = cfg.Model
	}
	if cfg.Dimensions != 0 {
		resolved.Dimensions = cfg.Dimensions
	}

	return &Client{
		service:    service,
		model:      resolved.Model,
		dimensions: resolved.Dimensions,
	}
}

func (c *Client) EmbedTexts(ctx context.Context, texts []string) (Result, error) {
	if c == nil || c.service == nil {
		return Result{}, fmt.Errorf("embedding client is not configured")
	}
	if strings.TrimSpace(string(c.model)) == "" {
		return Result{}, fmt.Errorf("embedding model is required")
	}
	if c.dimensions <= 0 {
		return Result{}, fmt.Errorf("embedding dimensions must be positive")
	}
	if len(texts) == 0 {
		return Result{}, fmt.Errorf("at least one text is required")
	}

	response, err := c.service.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
		Model:          c.model,
		Dimensions:     openai.Int(int64(c.dimensions)),
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
	})
	if err != nil {
		return Result{}, fmt.Errorf("create embeddings: %w", err)
	}
	if len(response.Data) != len(texts) {
		return Result{}, fmt.Errorf("embedding count mismatch: got %d vectors for %d texts", len(response.Data), len(texts))
	}

	vectors := make([][]float32, 0, len(response.Data))
	for i, item := range response.Data {
		if len(item.Embedding) != c.dimensions {
			return Result{}, fmt.Errorf(
				"embedding dimensions mismatch for item %d: got %d, want %d",
				i,
				len(item.Embedding),
				c.dimensions,
			)
		}

		vector := make([]float32, len(item.Embedding))
		for j, value := range item.Embedding {
			vector[j] = float32(value)
		}
		vectors = append(vectors, vector)
	}

	return Result{
		Model:      response.Model,
		Dimensions: c.dimensions,
		Vectors:    vectors,
	}, nil
}
