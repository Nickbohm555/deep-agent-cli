package indexstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testSessionIDPrimary = "11111111-1111-1111-1111-111111111111"
	testSessionIDOther   = "22222222-2222-2222-2222-222222222222"
)

func TestReplaceRepoIndex(t *testing.T) {
	ctx := context.Background()
	initial := []ChunkRecord{
		newChunkRecord(testSessionIDPrimary, "/repo/primary", "stale.md", 0, "stale", "hash-stale"),
		newChunkRecord(testSessionIDOther, "/repo/primary", "other-session.md", 0, "other session", "hash-other-session"),
		newChunkRecord("33333333-3333-3333-3333-333333333333", "/repo/other", "other-repo.md", 0, "other repo", "hash-other-repo"),
	}

	cases := []struct {
		name      string
		sessionID string
		repoRoot  string
		chunks    []ChunkRecordInput
		wantErr   bool
		verify    func(*testing.T, []ChunkRecord, *fakeIndexTx)
	}{
		{
			name:      "replaces_exact_scope_rows",
			sessionID: testSessionIDPrimary,
			repoRoot:  "/repo/primary",
			chunks: []ChunkRecordInput{
				newChunkInput(testSessionIDPrimary, "/repo/primary", "b.md", 1, "second chunk", "hash-b-1"),
				newChunkInput(testSessionIDPrimary, "/repo/primary", "a.md", 0, "first chunk", "hash-a-0"),
			},
			verify: func(t *testing.T, records []ChunkRecord, tx *fakeIndexTx) {
				t.Helper()
				if !tx.committed {
					t.Fatal("transaction was not committed")
				}
				if tx.rolledBack {
					t.Fatal("transaction should not roll back on success")
				}
				if got := countRowsForScope(records, testSessionIDPrimary, "/repo/primary"); got != 2 {
					t.Fatalf("primary scope row count = %d, want 2", got)
				}
				if rowExists(records, testSessionIDPrimary, "/repo/primary", "stale.md", 0) {
					t.Fatal("stale row still present after replace")
				}
				if got := countRowsForScope(records, testSessionIDOther, "/repo/primary"); got != 1 {
					t.Fatalf("other session row count = %d, want 1", got)
				}
				if got := countRowsForScope(records, "33333333-3333-3333-3333-333333333333", "/repo/other"); got != 1 {
					t.Fatalf("other repo row count = %d, want 1", got)
				}
			},
		},
		{
			name:      "rejects_cross_scope_chunk_payload",
			sessionID: testSessionIDPrimary,
			repoRoot:  "/repo/primary",
			chunks: []ChunkRecordInput{
				newChunkInput(testSessionIDOther, "/repo/primary", "wrong.md", 0, "wrong", "hash-wrong"),
			},
			wantErr: true,
			verify: func(t *testing.T, records []ChunkRecord, tx *fakeIndexTx) {
				t.Helper()
				if !tx.rolledBack {
					t.Fatal("transaction was not rolled back")
				}
				if tx.committed {
					t.Fatal("transaction should not commit on validation error")
				}
				if len(records) != len(initial) {
					t.Fatalf("record count = %d, want %d", len(records), len(initial))
				}
				if !rowExists(records, testSessionIDPrimary, "/repo/primary", "stale.md", 0) {
					t.Fatal("original target row should remain after rejected replace")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := newFakeIndexTx(initial)
			store := &Store{
				beginTx: func(context.Context) (pgx.Tx, error) { return tx, nil },
			}

			err := store.ReplaceRepoIndex(ctx, tc.sessionID, tc.repoRoot, tc.chunks)
			if tc.wantErr {
				if err == nil {
					t.Fatal("ReplaceRepoIndex returned nil error")
				}
			} else if err != nil {
				t.Fatalf("ReplaceRepoIndex returned error: %v", err)
			}

			tc.verify(t, tx.records, tx)
		})
	}
}

func TestListRepoIndex(t *testing.T) {
	ctx := context.Background()
	records := []ChunkRecord{
		newChunkRecord(testSessionIDPrimary, "/repo/ordered", "z-last.md", 2, "z2", "hash-z-2"),
		newChunkRecord(testSessionIDPrimary, "/repo/ordered", "a-first.md", 1, "a1", "hash-a-1"),
		newChunkRecord(testSessionIDPrimary, "/repo/ordered", "a-first.md", 0, "a0", "hash-a-0"),
		newChunkRecord(testSessionIDOther, "/repo/ordered", "other-session.md", 0, "other", "hash-other"),
	}

	store := &Store{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if sql != listRepoIndexSQL {
				return nil, fmt.Errorf("unexpected query: %s", sql)
			}
			sessionID := args[0].(string)
			repoRoot := args[1].(string)

			var filtered []ChunkRecord
			for _, record := range records {
				if record.SessionID == sessionID && record.RepoRoot == repoRoot {
					filtered = append(filtered, record)
				}
			}
			sort.Slice(filtered, func(i, j int) bool {
				if filtered[i].RelPath != filtered[j].RelPath {
					return filtered[i].RelPath < filtered[j].RelPath
				}
				return filtered[i].ChunkIndex < filtered[j].ChunkIndex
			})

			values := make([][]any, 0, len(filtered))
			for _, record := range filtered {
				values = append(values, []any{
					record.ID,
					record.SessionID,
					record.RepoRoot,
					record.RelPath,
					record.ChunkIndex,
					record.Content,
					record.ContentHash,
					record.Model,
					record.EmbeddingDims,
					vectorLiteral(record.Embedding),
					record.CreatedAt,
				})
			}

			return &fakeIndexRows{values: values}, nil
		},
	}

	listed, err := store.ListRepoIndex(ctx, testSessionIDPrimary, "/repo/ordered")
	if err != nil {
		t.Fatalf("ListRepoIndex returned error: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("ListRepoIndex length = %d, want 3", len(listed))
	}

	wantOrder := []struct {
		path  string
		index int
	}{
		{path: "a-first.md", index: 0},
		{path: "a-first.md", index: 1},
		{path: "z-last.md", index: 2},
	}

	for i, want := range wantOrder {
		if listed[i].RelPath != want.path || listed[i].ChunkIndex != want.index {
			t.Fatalf("listed[%d] = (%s,%d), want (%s,%d)", i, listed[i].RelPath, listed[i].ChunkIndex, want.path, want.index)
		}
	}
}

func TestReplaceRepoIndexIntegration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newIndexStoreHarness(t)
	defer harness.Close()

	store := New(harness.NewPool(t))
	primaryRepo := filepath.Join(harness.repoRoot, "primary")
	otherRepo := filepath.Join(harness.repoRoot, "other")

	harness.InsertSession(t, testSessionIDPrimary, primaryRepo)
	harness.InsertSession(t, testSessionIDOther, primaryRepo)
	harness.InsertSession(t, "33333333-3333-3333-3333-333333333333", otherRepo)

	if err := store.ReplaceRepoIndex(ctx, testSessionIDPrimary, primaryRepo, []ChunkRecordInput{
		newChunkInput(testSessionIDPrimary, primaryRepo, "stale.md", 0, "stale", "hash-stale"),
	}); err != nil {
		t.Fatalf("seed ReplaceRepoIndex returned error: %v", err)
	}
	if err := store.ReplaceRepoIndex(ctx, testSessionIDOther, primaryRepo, []ChunkRecordInput{
		newChunkInput(testSessionIDOther, primaryRepo, "other-session.md", 0, "other session", "hash-other-session"),
	}); err != nil {
		t.Fatalf("seed other session ReplaceRepoIndex returned error: %v", err)
	}
	if err := store.ReplaceRepoIndex(ctx, "33333333-3333-3333-3333-333333333333", otherRepo, []ChunkRecordInput{
		newChunkInput("33333333-3333-3333-3333-333333333333", otherRepo, "other-repo.md", 0, "other repo", "hash-other-repo"),
	}); err != nil {
		t.Fatalf("seed other repo ReplaceRepoIndex returned error: %v", err)
	}

	cases := []struct {
		name      string
		sessionID string
		repoRoot  string
		chunks    []ChunkRecordInput
		verify    func(*testing.T)
	}{
		{
			name:      "replaces_exact_scope_rows",
			sessionID: testSessionIDPrimary,
			repoRoot:  primaryRepo,
			chunks: []ChunkRecordInput{
				newChunkInput(testSessionIDPrimary, primaryRepo, "b.md", 1, "second chunk", "hash-b-1"),
				newChunkInput(testSessionIDPrimary, primaryRepo, "a.md", 0, "first chunk", "hash-a-0"),
			},
			verify: func(t *testing.T) {
				records := harness.ListRows(t)
				if got := countRowsForScope(records, testSessionIDPrimary, primaryRepo); got != 2 {
					t.Fatalf("primary scope row count = %d, want 2", got)
				}
				if rowExists(records, testSessionIDPrimary, primaryRepo, "stale.md", 0) {
					t.Fatal("stale primary row still exists after replace")
				}
			},
		},
		{
			name:      "rejects_cross_scope_chunk_payload",
			sessionID: testSessionIDPrimary,
			repoRoot:  primaryRepo,
			chunks: []ChunkRecordInput{
				newChunkInput(testSessionIDOther, primaryRepo, "wrong.md", 0, "wrong", "hash-wrong"),
			},
			verify: func(t *testing.T) {
				records := harness.ListRows(t)
				if got := countRowsForScope(records, testSessionIDPrimary, primaryRepo); got != 2 {
					t.Fatalf("primary scope row count after rejected replace = %d, want 2", got)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.ReplaceRepoIndex(ctx, tc.sessionID, tc.repoRoot, tc.chunks)
			if tc.name == "rejects_cross_scope_chunk_payload" {
				if err == nil {
					t.Fatal("ReplaceRepoIndex returned nil error for cross-scope payload")
				}
			} else if err != nil {
				t.Fatalf("ReplaceRepoIndex returned error: %v", err)
			}

			tc.verify(t)

			records := harness.ListRows(t)
			if got := countRowsForScope(records, testSessionIDOther, primaryRepo); got != 1 {
				t.Fatalf("other session row count = %d, want 1", got)
			}
			if got := countRowsForScope(records, "33333333-3333-3333-3333-333333333333", otherRepo); got != 1 {
				t.Fatalf("other repo row count = %d, want 1", got)
			}
		})
	}
}

func TestListRepoIndexIntegration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newIndexStoreHarness(t)
	defer harness.Close()

	store := New(harness.NewPool(t))
	repoRoot := filepath.Join(harness.repoRoot, "ordered")
	harness.InsertSession(t, testSessionIDPrimary, repoRoot)

	if err := store.ReplaceRepoIndex(ctx, testSessionIDPrimary, repoRoot, []ChunkRecordInput{
		newChunkInput(testSessionIDPrimary, repoRoot, "z-last.md", 2, "z2", "hash-z-2"),
		newChunkInput(testSessionIDPrimary, repoRoot, "a-first.md", 1, "a1", "hash-a-1"),
		newChunkInput(testSessionIDPrimary, repoRoot, "a-first.md", 0, "a0", "hash-a-0"),
	}); err != nil {
		t.Fatalf("ReplaceRepoIndex returned error: %v", err)
	}

	records, err := store.ListRepoIndex(ctx, testSessionIDPrimary, repoRoot)
	if err != nil {
		t.Fatalf("ListRepoIndex returned error: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("ListRepoIndex length = %d, want 3", len(records))
	}

	wantOrder := []struct {
		path  string
		index int
	}{
		{path: "a-first.md", index: 0},
		{path: "a-first.md", index: 1},
		{path: "z-last.md", index: 2},
	}

	for i, want := range wantOrder {
		if records[i].RelPath != want.path || records[i].ChunkIndex != want.index {
			t.Fatalf("records[%d] = (%s,%d), want (%s,%d)", i, records[i].RelPath, records[i].ChunkIndex, want.path, want.index)
		}
		if records[i].SessionID != testSessionIDPrimary || records[i].RepoRoot != repoRoot {
			t.Fatalf("records[%d] scope = (%s,%s), want (%s,%s)", i, records[i].SessionID, records[i].RepoRoot, testSessionIDPrimary, repoRoot)
		}
		if len(records[i].Embedding) != DefaultEmbeddingDimensions {
			t.Fatalf("records[%d] embedding dims = %d, want %d", i, len(records[i].Embedding), DefaultEmbeddingDimensions)
		}
	}
}

func newChunkInput(sessionID, repoRoot, relPath string, chunkIndex int, content, contentHash string) ChunkRecordInput {
	return ChunkRecordInput{
		SessionID:     sessionID,
		RepoRoot:      repoRoot,
		RelPath:       relPath,
		ChunkIndex:    chunkIndex,
		Content:       content,
		ContentHash:   contentHash,
		Model:         "text-embedding-3-small",
		EmbeddingDims: DefaultEmbeddingDimensions,
		Embedding:     embeddingWithSeed(float32(chunkIndex + 1)),
	}
}

func newChunkRecord(sessionID, repoRoot, relPath string, chunkIndex int, content, contentHash string) ChunkRecord {
	return ChunkRecord{
		ID:            int64(chunkIndex + 1),
		SessionID:     sessionID,
		RepoRoot:      repoRoot,
		RelPath:       relPath,
		ChunkIndex:    chunkIndex,
		Content:       content,
		ContentHash:   contentHash,
		Model:         "text-embedding-3-small",
		EmbeddingDims: DefaultEmbeddingDimensions,
		Embedding:     embeddingWithSeed(float32(chunkIndex + 1)),
		CreatedAt:     time.Unix(int64(chunkIndex+1), 0).UTC(),
	}
}

func embeddingWithSeed(seed float32) []float32 {
	vector := make([]float32, DefaultEmbeddingDimensions)
	for i := range vector {
		vector[i] = seed + float32(i)/1000
	}
	return vector
}

type fakeIndexTx struct {
	records    []ChunkRecord
	rolledBack bool
	committed  bool
	nextID     int64
	snapshot   []ChunkRecord
}

func newFakeIndexTx(initial []ChunkRecord) *fakeIndexTx {
	records := append([]ChunkRecord(nil), initial...)
	snapshot := append([]ChunkRecord(nil), initial...)
	var maxID int64
	for _, record := range records {
		if record.ID > maxID {
			maxID = record.ID
		}
	}
	return &fakeIndexTx{
		records:  records,
		nextID:   maxID + 1,
		snapshot: snapshot,
	}
}

func (f *fakeIndexTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeIndexTx) Commit(context.Context) error {
	f.committed = true
	return nil
}

func (f *fakeIndexTx) Rollback(context.Context) error {
	f.rolledBack = true
	f.records = append([]ChunkRecord(nil), f.snapshot...)
	return nil
}

func (f *fakeIndexTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}

func (f *fakeIndexTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (f *fakeIndexTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (f *fakeIndexTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeIndexTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch sql {
	case deleteRepoIndexSQL:
		sessionID := args[0].(string)
		repoRoot := args[1].(string)
		filtered := f.records[:0]
		for _, record := range f.records {
			if record.SessionID == sessionID && record.RepoRoot == repoRoot {
				continue
			}
			filtered = append(filtered, record)
		}
		f.records = filtered
		return pgconn.CommandTag{}, nil
	case insertChunkSQL:
		embedding, err := parseVectorLiteral(args[8].(string))
		if err != nil {
			return pgconn.CommandTag{}, err
		}
		record := ChunkRecord{
			ID:            f.nextID,
			SessionID:     args[0].(string),
			RepoRoot:      args[1].(string),
			RelPath:       args[2].(string),
			ChunkIndex:    args[3].(int),
			Content:       args[4].(string),
			ContentHash:   args[5].(string),
			Model:         args[6].(string),
			EmbeddingDims: args[7].(int),
			Embedding:     embedding,
			CreatedAt:     time.Unix(f.nextID, 0).UTC(),
		}
		f.records = append(f.records, record)
		f.nextID++
		return pgconn.CommandTag{}, nil
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected exec: %s", sql)
	}
}

func (f *fakeIndexTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeIndexTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

func (f *fakeIndexTx) Conn() *pgx.Conn {
	return nil
}

type fakeIndexRows struct {
	values [][]any
	index  int
	err    error
}

func (f *fakeIndexRows) Close() {}

func (f *fakeIndexRows) Err() error {
	return f.err
}

func (f *fakeIndexRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (f *fakeIndexRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (f *fakeIndexRows) Next() bool {
	if f.index >= len(f.values) {
		return false
	}
	f.index++
	return true
}

func (f *fakeIndexRows) Scan(dest ...any) error {
	row := f.values[f.index-1]
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = row[i].(string)
		case *int:
			*target = row[i].(int)
		case *int64:
			*target = row[i].(int64)
		case *time.Time:
			*target = row[i].(time.Time)
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	return nil
}

func (f *fakeIndexRows) Values() ([]any, error) {
	if f.index == 0 || f.index > len(f.values) {
		return nil, errors.New("no current row")
	}
	return f.values[f.index-1], nil
}

func (f *fakeIndexRows) RawValues() [][]byte {
	return nil
}

func (f *fakeIndexRows) Conn() *pgx.Conn {
	return nil
}

type indexStoreHarness struct {
	adminPool   *pgxpool.Pool
	schemaName  string
	databaseURL string
	repoRoot    string
}

func newIndexStoreHarness(t *testing.T) *indexStoreHarness {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for indexstore integration tests")
	}

	schemaName := fmt.Sprintf("test_indexstore_%d", os.Getpid())
	adminPool := newIndexStoreTestPool(t, databaseURL, "")

	if _, err := adminPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)); err != nil {
		t.Fatalf("drop schema returned error: %v", err)
	}
	if _, err := adminPool.Exec(context.Background(), fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		t.Fatalf("create schema returned error: %v", err)
	}

	harness := &indexStoreHarness{
		adminPool:   adminPool,
		schemaName:  schemaName,
		databaseURL: databaseURL,
		repoRoot:    filepath.Join("/tmp", schemaName),
	}

	harness.ApplySQLFile(t, "db/migrations/0001_sessions_messages.sql", nil)
	harness.ApplySQLFile(t, "db/migrations/0004_index_chunks.sql", nil)

	return harness
}

func (h *indexStoreHarness) Close() {
	_, _ = h.adminPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", h.schemaName))
	h.adminPool.Close()
}

func (h *indexStoreHarness) NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newIndexStoreTestPool(t, h.databaseURL, h.schemaName)
}

func (h *indexStoreHarness) ApplySQLFile(t *testing.T, relativePath string, replacements map[string]string) {
	t.Helper()

	content, err := os.ReadFile(relativePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", relativePath, err)
	}

	sql := string(content)
	for oldValue, newValue := range replacements {
		sql = strings.ReplaceAll(sql, oldValue, newValue)
	}

	if _, err := h.adminPool.Exec(context.Background(), fmt.Sprintf("SET search_path TO %s, public", h.schemaName)); err != nil {
		t.Fatalf("set search_path returned error: %v", err)
	}
	if _, err := h.adminPool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("Exec(%q) returned error: %v", relativePath, err)
	}
}

func (h *indexStoreHarness) InsertSession(t *testing.T, sessionID, repoRoot string) {
	t.Helper()

	if _, err := h.adminPool.Exec(
		context.Background(),
		fmt.Sprintf("INSERT INTO %s.sessions (thread_id, repo_root) VALUES ($1, $2)", h.schemaName),
		sessionID,
		repoRoot,
	); err != nil {
		t.Fatalf("insert session returned error: %v", err)
	}
}

func (h *indexStoreHarness) ListRows(t *testing.T) []ChunkRecord {
	t.Helper()

	rows, err := h.adminPool.Query(
		context.Background(),
		fmt.Sprintf(`
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
			FROM %s.index_chunks
			ORDER BY session_id::text, repo_root, rel_path, chunk_index
		`, h.schemaName),
	)
	if err != nil {
		t.Fatalf("query rows returned error: %v", err)
	}
	defer rows.Close()

	var records []ChunkRecord
	for rows.Next() {
		var (
			record    ChunkRecord
			embedding string
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
			&embedding,
			&record.CreatedAt,
		); err != nil {
			t.Fatalf("scan row returned error: %v", err)
		}
		record.Embedding, err = parseVectorLiteral(embedding)
		if err != nil {
			t.Fatalf("parse embedding returned error: %v", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows returned error: %v", err)
	}

	return records
}

func newIndexStoreTestPool(t *testing.T, databaseURL, schemaName string) *pgxpool.Pool {
	t.Helper()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if schemaName != "" {
		config.ConnConfig.RuntimeParams["search_path"] = fmt.Sprintf("%s,public", schemaName)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("Ping returned error: %v", err)
	}

	return pool
}

func countRowsForScope(records []ChunkRecord, sessionID, repoRoot string) int {
	count := 0
	for _, record := range records {
		if record.SessionID == sessionID && record.RepoRoot == repoRoot {
			count++
		}
	}
	return count
}

func rowExists(records []ChunkRecord, sessionID, repoRoot, relPath string, chunkIndex int) bool {
	for _, record := range records {
		if record.SessionID == sessionID &&
			record.RepoRoot == repoRoot &&
			record.RelPath == relPath &&
			record.ChunkIndex == chunkIndex {
			return true
		}
	}
	return false
}
