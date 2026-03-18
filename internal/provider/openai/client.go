package openai

import (
	"context"
	"fmt"

	openaiapi "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

const defaultModel = openaiapi.ChatModelGPT5_2

type chatCompletionService interface {
	New(context.Context, openaiapi.ChatCompletionNewParams, ...option.RequestOption) (*openaiapi.ChatCompletion, error)
}

type Client struct {
	service chatCompletionService
}

func NewClient(client *openaiapi.Client) *Client {
	if client == nil {
		newClient := openaiapi.NewClient()
		client = &newClient
	}

	return &Client{
		service: &client.Chat.Completions,
	}
}

func NewClientWithService(service chatCompletionService) *Client {
	return &Client{service: service}
}

func (c *Client) CompleteTurn(ctx context.Context, request runtime.ProviderRequest) (runtime.ProviderResponse, error) {
	if c == nil || c.service == nil {
		return runtime.ProviderResponse{}, fmt.Errorf("openai client is not configured")
	}

	messages, err := mapMessages(request.Conversation)
	if err != nil {
		return runtime.ProviderResponse{}, err
	}

	params := openaiapi.ChatCompletionNewParams{
		Model:               modelName(request.Input.Config.Model),
		Messages:            messages,
		MaxCompletionTokens: openaiapi.Opt(int64(1024)),
		ParallelToolCalls:   openaiapi.Opt(false),
	}

	if len(request.Tools) > 0 {
		params.Tools = mapTools(request.Tools)
	}

	completion, err := c.service.New(ctx, params)
	if err != nil {
		return runtime.ProviderResponse{}, err
	}
	if len(completion.Choices) == 0 {
		return runtime.ProviderResponse{}, fmt.Errorf("openai returned no choices")
	}

	return mapResponse(completion.Choices[0].Message, completion.Usage), nil
}

func modelName(configured string) shared.ChatModel {
	if configured == "" {
		return defaultModel
	}

	return shared.ChatModel(configured)
}
