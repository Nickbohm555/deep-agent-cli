package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/retrieval"
)

func TestRetrievalHookInjectsRankedEvidence(t *testing.T) {
	provider := &stubProvider{
		responses: []ProviderResponse{{
			AssistantMessage: Message{
				Role:    MessageRoleAssistant,
				Content: "Here is what I found.",
			},
			StopReason: StopReasonComplete,
		}},
	}

	retriever := retrieval.NewService(func(_ context.Context, req retrieval.SemanticQueryRequest) (retrieval.SemanticQueryResponse, error) {
		if req.TopK != retrieval.DefaultTopK {
			t.Fatalf("retrieval top_k = %d, want %d", req.TopK, retrieval.DefaultTopK)
		}
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
					Score:    0.742,
					Snippet:  "orchestrator evidence",
				},
			},
		}, nil
	})

	repoRoot := t.TempDir()
	ctx, err := WithRepoRoot(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}

	orchestrator := &Orchestrator{
		Provider:          provider,
		SemanticRetriever: retriever,
	}

	_, err = orchestrator.RunTurn(WithSessionID(ctx, "session-1"), TurnInput{
		SessionID:   "session-1",
		UserMessage: "where is the registry defined?",
		Config:      TurnConfigForTest(1),
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	conversation := provider.requests[0].Conversation
	if len(conversation) != 2 {
		t.Fatalf("provider conversation len = %d, want 2", len(conversation))
	}
	if conversation[0].Role != MessageRoleSystem {
		t.Fatalf("retrieval message role = %q, want %q", conversation[0].Role, MessageRoleSystem)
	}
	if conversation[1].Role != MessageRoleUser {
		t.Fatalf("final conversation role = %q, want %q", conversation[1].Role, MessageRoleUser)
	}

	content := conversation[0].Content
	if !strings.Contains(content, "Semantic retrieval evidence for the current repository:") {
		t.Fatalf("retrieval content missing header: %q", content)
	}
	if !strings.Contains(content, "Index status: ready.") {
		t.Fatalf("retrieval content missing ready status: %q", content)
	}
	if !strings.Contains(content, "1. internal/tools/registry/static.go | internal/tools/registry/static.go#12 | score=0.8731") {
		t.Fatalf("retrieval content missing ranked evidence: %q", content)
	}
	if !strings.Contains(content, "Snippet: registry definition snippet") {
		t.Fatalf("retrieval content missing normalized snippet: %q", content)
	}
}

func TestRetrievalHookHandlesIndexNotReady(t *testing.T) {
	provider := &stubProvider{
		responses: []ProviderResponse{{
			AssistantMessage: Message{
				Role:    MessageRoleAssistant,
				Content: "Index is still building.",
			},
			StopReason: StopReasonComplete,
		}},
	}

	retriever := retrieval.NewService(func(_ context.Context, req retrieval.SemanticQueryRequest) (retrieval.SemanticQueryResponse, error) {
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

	repoRoot := t.TempDir()
	ctx, err := WithRepoRoot(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}

	orchestrator := &Orchestrator{
		Provider:          provider,
		SemanticRetriever: retriever,
	}

	_, err = orchestrator.RunTurn(WithSessionID(ctx, "session-2"), TurnInput{
		SessionID:   "session-2",
		UserMessage: "show me the runtime architecture",
		Config:      TurnConfigForTest(1),
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	conversation := provider.requests[0].Conversation
	if len(conversation) != 2 {
		t.Fatalf("provider conversation len = %d, want 2", len(conversation))
	}

	content := conversation[0].Content
	if !strings.Contains(content, "Index status: index_not_ready.") {
		t.Fatalf("retrieval content missing not-ready status: %q", content)
	}
	if !strings.Contains(content, "No retrieval evidence was attached because the semantic index is not ready.") {
		t.Fatalf("retrieval content missing not-ready explanation: %q", content)
	}
}
