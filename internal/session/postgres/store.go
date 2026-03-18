package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	insertSessionWithThreadIDSQL = `
		INSERT INTO sessions (thread_id, repo_root)
		VALUES ($1, $2)
		RETURNING thread_id, repo_root, created_at
	`
	insertSessionSQL = `
		INSERT INTO sessions (repo_root)
		VALUES ($1)
		RETURNING thread_id, repo_root, created_at
	`
	selectSessionSQL = `
		SELECT thread_id, repo_root, created_at
		FROM sessions
		WHERE thread_id = $1
	`
	insertMessageSQL = `
		INSERT INTO messages (thread_id, role, content)
		VALUES ($1, $2, $3)
		RETURNING id, thread_id, role, content, created_at
	`
	listMessagesSQL = `
		SELECT id, thread_id, role, content, created_at
		FROM messages
		WHERE thread_id = $1
		ORDER BY id ASC
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

func (s *Store) CreateSession(ctx context.Context, params session.CreateSessionParams) (created session.Session, err error) {
	tx, err := s.beginTx(ctx)
	if err != nil {
		return session.Session{}, fmt.Errorf("begin create session tx: %w", err)
	}
	defer rollbackTx(ctx, tx, &err)

	var row pgx.Row
	if params.ThreadID != "" {
		row = tx.QueryRow(ctx, insertSessionWithThreadIDSQL, params.ThreadID, params.RepoRoot)
	} else {
		row = tx.QueryRow(ctx, insertSessionSQL, params.RepoRoot)
	}

	if err = row.Scan(&created.ThreadID, &created.RepoRoot, &created.CreatedAt); err != nil {
		return session.Session{}, fmt.Errorf("insert session: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return session.Session{}, fmt.Errorf("commit create session tx: %w", err)
	}

	return created, nil
}

func (s *Store) ResumeSession(ctx context.Context, threadID string) (session.Session, error) {
	var resumed session.Session
	err := s.queryRow(ctx, selectSessionSQL, threadID).Scan(&resumed.ThreadID, &resumed.RepoRoot, &resumed.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return session.Session{}, session.ErrSessionNotFound
		}
		return session.Session{}, fmt.Errorf("select session: %w", err)
	}

	return resumed, nil
}

func (s *Store) AppendMessage(ctx context.Context, params session.AppendMessageParams) (message session.Message, err error) {
	tx, err := s.beginTx(ctx)
	if err != nil {
		return session.Message{}, fmt.Errorf("begin append message tx: %w", err)
	}
	defer rollbackTx(ctx, tx, &err)

	if _, err = scanSession(tx.QueryRow(ctx, selectSessionSQL, params.ThreadID)); err != nil {
		return session.Message{}, err
	}

	if err = tx.QueryRow(ctx, insertMessageSQL, params.ThreadID, params.Role, params.Content).Scan(
		&message.ID,
		&message.ThreadID,
		&message.Role,
		&message.Content,
		&message.CreatedAt,
	); err != nil {
		return session.Message{}, fmt.Errorf("insert message: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return session.Message{}, fmt.Errorf("commit append message tx: %w", err)
	}

	return message, nil
}

func (s *Store) ListMessages(ctx context.Context, threadID string) ([]session.Message, error) {
	if _, err := s.ResumeSession(ctx, threadID); err != nil {
		return nil, err
	}

	rows, err := s.query(ctx, listMessagesSQL, threadID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []session.Message
	for rows.Next() {
		var message session.Message
		if err := rows.Scan(&message.ID, &message.ThreadID, &message.Role, &message.Content, &message.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	return messages, nil
}

func scanSession(row pgx.Row) (session.Session, error) {
	var found session.Session
	if err := row.Scan(&found.ThreadID, &found.RepoRoot, &found.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return session.Session{}, session.ErrSessionNotFound
		}
		return session.Session{}, fmt.Errorf("select session: %w", err)
	}

	return found, nil
}

func rollbackTx(ctx context.Context, tx pgx.Tx, errPtr *error) {
	if *errPtr == nil {
		return
	}
	_ = tx.Rollback(ctx)
}
