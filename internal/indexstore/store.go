package indexstore

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	deleteRepoIndexSQL = `
		DELETE FROM index_chunks
		WHERE session_id = $1 AND repo_root = $2
	`
	insertChunkSQL = `
		INSERT INTO index_chunks (
			session_id,
			repo_root,
			rel_path,
			chunk_index,
			content,
			content_hash,
			model,
			embedding_dims,
			embedding
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::vector)
	`
	listRepoIndexSQL = `
		SELECT
			id,
			session_id::text,
			repo_root,
			rel_path,
			chunk_index,
			content,
			content_hash,
			model,
			embedding_dims,
			embedding::text,
			created_at
		FROM index_chunks
		WHERE session_id = $1 AND repo_root = $2
		ORDER BY rel_path ASC, chunk_index ASC
	`
)

type Store struct {
	beginTx  func(context.Context) (pgx.Tx, error)
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{
		beginTx:  pool.Begin,
		queryRow: pool.QueryRow,
		query:    pool.Query,
	}
}

func (s *Store) ReplaceRepoIndex(ctx context.Context, sessionID, repoRoot string, chunks []ChunkRecordInput) (err error) {
	if err := validateScope(sessionID, repoRoot); err != nil {
		return err
	}

	tx, err := s.beginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin replace repo index tx: %w", err)
	}
	defer rollbackTx(ctx, tx, &err)

	if _, err = tx.Exec(ctx, deleteRepoIndexSQL, sessionID, repoRoot); err != nil {
		return fmt.Errorf("delete repo index rows: %w", err)
	}

	for i, chunk := range chunks {
		if err := validateChunkScope(sessionID, repoRoot, chunk); err != nil {
			return fmt.Errorf("validate chunk %d: %w", i, err)
		}
		if _, err = tx.Exec(
			ctx,
			insertChunkSQL,
			chunk.SessionID,
			chunk.RepoRoot,
			chunk.RelPath,
			chunk.ChunkIndex,
			chunk.Content,
			chunk.ContentHash,
			chunk.Model,
			chunk.EmbeddingDims,
			vectorLiteral(chunk.Embedding),
		); err != nil {
			return fmt.Errorf("insert chunk %d: %w", i, err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit replace repo index tx: %w", err)
	}

	return nil
}

func (s *Store) ListRepoIndex(ctx context.Context, sessionID, repoRoot string) ([]ChunkRecord, error) {
	if err := validateScope(sessionID, repoRoot); err != nil {
		return nil, err
	}

	rows, err := s.query(ctx, listRepoIndexSQL, sessionID, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("query repo index rows: %w", err)
	}
	defer rows.Close()

	var records []ChunkRecord
	for rows.Next() {
		var (
			record           ChunkRecord
			embeddingLiteral string
		)
		if err := rows.Scan(
			&record.ID,
			&record.SessionID,
			&record.RepoRoot,
			&record.RelPath,
			&record.ChunkIndex,
			&record.Content,
			&record.ContentHash,
			&record.Model,
			&record.EmbeddingDims,
			&embeddingLiteral,
			&record.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan repo index row: %w", err)
		}

		record.Embedding, err = parseVectorLiteral(embeddingLiteral)
		if err != nil {
			return nil, fmt.Errorf("parse repo index embedding for %s[%d]: %w", record.RelPath, record.ChunkIndex, err)
		}

		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repo index rows: %w", err)
	}

	return records, nil
}

func validateScope(sessionID, repoRoot string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(repoRoot) == "" {
		return fmt.Errorf("repo_root is required")
	}
	return nil
}

func validateChunkScope(sessionID, repoRoot string, chunk ChunkRecordInput) error {
	if err := validateScope(chunk.SessionID, chunk.RepoRoot); err != nil {
		return err
	}
	if chunk.SessionID != sessionID {
		return fmt.Errorf("chunk session_id %q does not match scope %q", chunk.SessionID, sessionID)
	}
	if chunk.RepoRoot != repoRoot {
		return fmt.Errorf("chunk repo_root %q does not match scope %q", chunk.RepoRoot, repoRoot)
	}
	return nil
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

func parseVectorLiteral(input string) ([]float32, error) {
	trimmed := strings.TrimSpace(input)
	trimmed = strings.TrimPrefix(trimmed, "[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	if trimmed == "" {
		return nil, nil
	}

	parts := strings.Split(trimmed, ",")
	values := make([]float32, 0, len(parts))
	for _, part := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(part), 32)
		if err != nil {
			return nil, err
		}
		values = append(values, float32(parsed))
	}

	return values, nil
}

func rollbackTx(ctx context.Context, tx pgx.Tx, errPtr *error) {
	if *errPtr == nil {
		return
	}
	_ = tx.Rollback(ctx)
}
