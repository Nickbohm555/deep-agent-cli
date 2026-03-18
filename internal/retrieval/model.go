package retrieval

import (
	"fmt"
	"strings"
	"time"
)

const (
	DefaultTopK        = 5
	MaxSnippetLength   = 400
	IndexStatusUnknown = "unknown"
)

type SemanticQueryRequest struct {
	SessionID string `json:"session_id"`
	RepoRoot  string `json:"repo_root"`
	Query     string `json:"query"`
	TopK      int    `json:"top_k"`
}

type SemanticIndexReadiness struct {
	Ready      bool       `json:"ready"`
	Status     string     `json:"status"`
	SnapshotID *int64     `json:"snapshot_id,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

type SemanticQueryResult struct {
	Rank     int     `json:"rank"`
	FilePath string  `json:"file_path"`
	ChunkID  string  `json:"chunk_id"`
	Score    float64 `json:"score"`
	Snippet  string  `json:"snippet"`
}

type SemanticQueryResponse struct {
	SessionID string                 `json:"session_id"`
	RepoRoot  string                 `json:"repo_root"`
	Query     string                 `json:"query"`
	TopK      int                    `json:"top_k"`
	Index     SemanticIndexReadiness `json:"index"`
	Results   []SemanticQueryResult  `json:"results"`
}

func NormalizeSemanticQueryRequest(req SemanticQueryRequest) SemanticQueryRequest {
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.RepoRoot = strings.TrimSpace(req.RepoRoot)
	req.Query = strings.TrimSpace(req.Query)
	return req
}

func ValidateSemanticQueryRequest(req SemanticQueryRequest) error {
	req = NormalizeSemanticQueryRequest(req)

	switch {
	case req.SessionID == "":
		return fmt.Errorf("session_id is required")
	case req.RepoRoot == "":
		return fmt.Errorf("repo_root is required")
	case req.Query == "":
		return fmt.Errorf("query is required")
	case req.TopK <= 0:
		return fmt.Errorf("top_k must be positive")
	default:
		return nil
	}
}

func BoundSnippet(snippet string) string {
	if len(snippet) <= MaxSnippetLength {
		return snippet
	}
	return snippet[:MaxSnippetLength]
}
