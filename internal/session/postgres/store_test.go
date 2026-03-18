package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestStoreCreateResumeAndListMessages(t *testing.T) {
	timestamp := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	store := &Store{
		beginTx: func(context.Context) (pgx.Tx, error) {
			return &fakeTx{
				queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
					switch sql {
					case insertSessionWithThreadIDSQL:
						return fakeRow{values: []any{"thread-123", "/repo", timestamp}}
					case selectSessionSQL:
						return fakeRow{values: []any{"thread-123", "/repo", timestamp}}
					case insertMessageSQL:
						return fakeRow{values: []any{int64(1), "thread-123", "user", "first", timestamp}}
					default:
						t.Fatalf("unexpected tx query: %s", sql)
						return nil
					}
				},
			}, nil
		},
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != selectSessionSQL {
				t.Fatalf("unexpected query row: %s", sql)
			}
			if len(args) != 1 || args[0] != "thread-123" {
				t.Fatalf("unexpected resume args: %v", args)
			}
			return fakeRow{values: []any{"thread-123", "/repo", timestamp}}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if sql != listMessagesSQL {
				t.Fatalf("unexpected query: %s", sql)
			}
			if len(args) != 1 || args[0] != "thread-123" {
				t.Fatalf("unexpected list args: %v", args)
			}
			return &fakeRows{
				values: [][]any{
					{int64(1), "thread-123", "user", "first", timestamp},
					{int64(2), "thread-123", "assistant", "second", timestamp.Add(time.Second)},
				},
			}, nil
		},
	}

	created, err := store.CreateSession(context.Background(), session.CreateSessionParams{
		ThreadID: "thread-123",
		RepoRoot: "/repo",
	})
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if created.ThreadID != "thread-123" || created.RepoRoot != "/repo" {
		t.Fatalf("CreateSession returned %+v", created)
	}

	resumed, err := store.ResumeSession(context.Background(), "thread-123")
	if err != nil {
		t.Fatalf("ResumeSession returned error: %v", err)
	}
	if resumed != created {
		t.Fatalf("ResumeSession returned %+v, want %+v", resumed, created)
	}

	message, err := store.AppendMessage(context.Background(), session.AppendMessageParams{
		ThreadID: "thread-123",
		Role:     "user",
		Content:  "first",
	})
	if err != nil {
		t.Fatalf("AppendMessage returned error: %v", err)
	}
	if message.ID != 1 || message.Content != "first" {
		t.Fatalf("AppendMessage returned %+v", message)
	}

	messages, err := store.ListMessages(context.Background(), "thread-123")
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("ListMessages length = %d, want 2", len(messages))
	}
	if messages[0].ID != 1 || messages[1].ID != 2 {
		t.Fatalf("ListMessages IDs = [%d %d], want [1 2]", messages[0].ID, messages[1].ID)
	}
}

func TestStoreAppendMessageReturnsNotFound(t *testing.T) {
	tx := &fakeTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != selectSessionSQL {
				t.Fatalf("unexpected tx query: %s", sql)
			}
			return fakeRow{err: pgx.ErrNoRows}
		},
	}
	store := &Store{
		beginTx: func(context.Context) (pgx.Tx, error) { return tx, nil },
	}

	_, err := store.AppendMessage(context.Background(), session.AppendMessageParams{
		ThreadID: "missing",
		Role:     "user",
		Content:  "hello",
	})
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("AppendMessage error = %v, want ErrSessionNotFound", err)
	}
	if !tx.rolledBack {
		t.Fatal("AppendMessage should roll back on missing session")
	}
}

func TestStoreAppendMessageRollsBackOnWriteFailure(t *testing.T) {
	timestamp := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	tx := &fakeTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			switch sql {
			case selectSessionSQL:
				return fakeRow{values: []any{"thread-123", "/repo", timestamp}}
			case insertMessageSQL:
				return fakeRow{err: errors.New("insert failed")}
			default:
				t.Fatalf("unexpected tx query: %s", sql)
				return nil
			}
		},
	}
	store := &Store{
		beginTx: func(context.Context) (pgx.Tx, error) { return tx, nil },
	}

	_, err := store.AppendMessage(context.Background(), session.AppendMessageParams{
		ThreadID: "thread-123",
		Role:     "assistant",
		Content:  "reply",
	})
	if err == nil {
		t.Fatal("AppendMessage returned nil error, want write failure")
	}
	if !tx.rolledBack {
		t.Fatal("AppendMessage should roll back on insert failure")
	}
	if tx.committed {
		t.Fatal("AppendMessage should not commit on insert failure")
	}
}

type fakeTx struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	rolledBack bool
	committed  bool
}

func (f *fakeTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTx) Commit(context.Context) error {
	f.committed = true
	return nil
}

func (f *fakeTx) Rollback(context.Context) error {
	f.rolledBack = true
	return nil
}

func (f *fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}

func (f *fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (f *fakeTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (f *fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("not implemented")
}

func (f *fakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRowFn(ctx, sql, args...)
}

func (f *fakeTx) Conn() *pgx.Conn {
	return nil
}

type fakeRow struct {
	values []any
	err    error
}

func (f fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = f.values[i].(string)
		case *int64:
			*target = f.values[i].(int64)
		case *time.Time:
			*target = f.values[i].(time.Time)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

type fakeRows struct {
	values [][]any
	index  int
	err    error
}

func (f *fakeRows) Close() {}

func (f *fakeRows) Err() error {
	return f.err
}

func (f *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (f *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (f *fakeRows) Next() bool {
	if f.index >= len(f.values) {
		return false
	}
	f.index++
	return true
}

func (f *fakeRows) Scan(dest ...any) error {
	row := f.values[f.index-1]
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = row[i].(string)
		case *int64:
			*target = row[i].(int64)
		case *time.Time:
			*target = row[i].(time.Time)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func (f *fakeRows) Values() ([]any, error) {
	if f.index == 0 || f.index > len(f.values) {
		return nil, errors.New("no current row")
	}
	return f.values[f.index-1], nil
}

func (f *fakeRows) RawValues() [][]byte {
	return nil
}

func (f *fakeRows) Conn() *pgx.Conn {
	return nil
}
