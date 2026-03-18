package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/retrieval"
)

func BuildRetrievalContext(ctx context.Context, retriever retrieval.SemanticRetriever, userMessage string) (string, bool, error) {
	if retriever == nil {
		return "", false, nil
	}

	query := strings.TrimSpace(userMessage)
	if query == "" {
		return "", false, nil
	}

	sessionID, err := SessionIDFromContext(ctx)
	if err != nil {
		return "", false, nil
	}
	repoRoot, err := RepoRootFromContext(ctx)
	if err != nil {
		return "", false, nil
	}

	resp, err := retriever.Query(ctx, retrieval.SemanticQueryRequest{
		SessionID: sessionID,
		RepoRoot:  repoRoot,
		Query:     query,
		TopK:      retrieval.DefaultTopK,
	})
	if err != nil {
		return "", false, fmt.Errorf("build retrieval context: %w", err)
	}

	return formatRetrievalContext(resp), true, nil
}

func formatRetrievalContext(resp retrieval.SemanticQueryResponse) string {
	lines := []string{
		"Semantic retrieval evidence for the current repository:",
		fmt.Sprintf("Index status: %s.", formatRetrievalStatus(resp.Index.Status)),
	}

	if !resp.Index.Ready {
		lines = append(lines, "No retrieval evidence was attached because the semantic index is not ready.")
		return strings.Join(lines, "\n")
	}
	if len(resp.Results) == 0 {
		lines = append(lines, "No matching indexed chunks were found for this query.")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Ranked chunks:")
	for _, result := range resp.Results {
		lines = append(lines, fmt.Sprintf("%d. %s | %s | score=%.4f", result.Rank, result.FilePath, result.ChunkID, result.Score))
		lines = append(lines, "Snippet: "+singleLineSnippet(result.Snippet))
	}
	return strings.Join(lines, "\n")
}

func formatRetrievalStatus(status string) string {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return retrieval.IndexStatusUnknown
	}
	return trimmed
}

func singleLineSnippet(snippet string) string {
	parts := strings.Fields(strings.TrimSpace(snippet))
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}
