package retrieval

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	MaxStoreTopK = 50
	queryTopKSQL = `
		SELECT
			rel_path,
			chunk_index,
			content,
			(embedding <=> $3::vector) AS distance
		FROM index_chunks
		WHERE session_id = $1
		  AND repo_root = $2
		ORDER BY embedding <=> $3::vector ASC, rel_path ASC, chunk_index ASC
		LIMIT $4
	`
)

type topKQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type Store struct {
	query func(context.Context, string, ...any) (pgx.Rows, error)
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{query: pool.Query}
}

func (s *Store) QueryTopK(ctx context.Context, req SemanticQueryRequest, queryVector []float32) ([]SemanticQueryResult, error) {
	if s == nil || s.query == nil {
		return nil, fmt.Errorf("retrieval store is not configured")
	}

	req = NormalizeSemanticQueryRequest(req)
	switch {
	case req.SessionID == "":
		return nil, fmt.Errorf("session_id is required")
	case req.RepoRoot == "":
		return nil, fmt.Errorf("repo_root is required")
	case req.TopK <= 0:
		return nil, fmt.Errorf("top_k must be positive")
	case len(queryVector) == 0:
		return nil, fmt.Errorf("query vector is required")
	}

	topK := req.TopK
	if topK > MaxStoreTopK {
		topK = MaxStoreTopK
	}

	rows, err := s.query(ctx, queryTopKSQL, req.SessionID, req.RepoRoot, vectorLiteral(queryVector), topK)
	if err != nil {
		return nil, fmt.Errorf("query top-k chunks: %w", err)
	}
	defer rows.Close()

	results := make([]SemanticQueryResult, 0, topK)
	for rows.Next() {
		var (
			filePath   string
			chunkIndex int
			content    string
			distance   float64
		)
		if err := rows.Scan(&filePath, &chunkIndex, &content, &distance); err != nil {
			return nil, fmt.Errorf("scan top-k chunk: %w", err)
		}

		results = append(results, SemanticQueryResult{
			FilePath: strings.TrimSpace(filePath),
			ChunkID:  fmt.Sprintf("%s#%d", strings.TrimSpace(filePath), chunkIndex),
			Score:    ScoreFromCosineDistance(distance),
			Snippet:  BoundSnippet(strings.TrimSpace(content)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top-k chunks: %w", err)
	}

	return results, nil
}

func vectorLiteral(values []float32) string {
	if len(values) == 0 {
		return "[]"
	}

	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatFloat(float64(value), 'f', -1, 32))
	}

	return "[" + strings.Join(parts, ",") + "]"
}
