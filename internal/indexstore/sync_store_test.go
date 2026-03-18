package indexstore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/jackc/pgx/v5"
)

func TestSaveSnapshotStateRejectsCrossScopeFileState(t *testing.T) {
	store := &Store{}

	_, err := store.SaveSnapshotState(
		context.Background(),
		indexsync.SnapshotRoot{
			SessionID: testSessionIDPrimary,
			RepoRoot:  "/repo/primary",
			RootHash:  "root-1",
			Status:    indexsync.SnapshotStatusActive,
			IsActive:  true,
		},
		[]indexsync.MerkleNode{
			{
				Path:     "a.md",
				NodeType: indexsync.NodeTypeFile,
				NodeHash: "node-a",
				Status:   indexsync.FileStatusActive,
			},
		},
		[]indexsync.FileState{
			{
				SessionID: testSessionIDOther,
				RepoRoot:  "/repo/primary",
				RelPath:   "a.md",
				Status:    indexsync.FileStatusActive,
			},
		},
	)
	if err == nil {
		t.Fatal("SaveSnapshotState returned nil error for cross-scope file state")
	}
}

func TestLoadLatestSnapshotReturnsNilWhenMissing(t *testing.T) {
	store := &Store{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeSnapshotRow{err: pgx.ErrNoRows}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			t.Fatal("query should not be called when no snapshot exists")
			return nil, nil
		},
	}

	state, err := store.LoadLatestSnapshot(context.Background(), testSessionIDPrimary, "/repo/primary")
	if err != nil {
		t.Fatalf("LoadLatestSnapshot returned error: %v", err)
	}
	if state != nil {
		t.Fatalf("LoadLatestSnapshot returned %+v, want nil", state)
	}
}

func TestSaveSnapshotStateIntegrationIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newIndexStoreHarness(t)
	defer harness.Close()
	harness.ApplySQLFile(t, "internal/storage/postgres/migrations/0005_index_sync.sql", nil)

	store := New(harness.NewPool(t))
	repoRoot := filepath.Join(harness.repoRoot, "sync")
	harness.InsertSession(t, testSessionIDPrimary, repoRoot)

	root := indexsync.SnapshotRoot{
		SessionID: testSessionIDPrimary,
		RepoRoot:  repoRoot,
		RootHash:  "root-hash-1",
		Status:    indexsync.SnapshotStatusActive,
		IsActive:  true,
	}
	nodes := []indexsync.MerkleNode{
		testMerkleNode("docs", "", indexsync.NodeTypeDir, "node-dir", "", indexsync.FileStatusActive),
		testMerkleNode("docs/a.md", "docs", indexsync.NodeTypeFile, "node-a", "content-a", indexsync.FileStatusActive),
	}
	fileStates := []indexsync.FileState{
		testFileState(testSessionIDPrimary, repoRoot, "docs/a.md", "content-a", "node-a", indexsync.FileStatusActive),
	}

	saved1, err := store.SaveSnapshotState(ctx, root, nodes, fileStates)
	if err != nil {
		t.Fatalf("first SaveSnapshotState returned error: %v", err)
	}
	saved2, err := store.SaveSnapshotState(ctx, root, nodes, fileStates)
	if err != nil {
		t.Fatalf("second SaveSnapshotState returned error: %v", err)
	}

	if saved1.ID != saved2.ID {
		t.Fatalf("saved snapshot IDs = (%d,%d), want identical", saved1.ID, saved2.ID)
	}

	if got := harness.CountSnapshotRows(t, testSessionIDPrimary, repoRoot); got != 1 {
		t.Fatalf("snapshot row count = %d, want 1", got)
	}
	if got := harness.CountNodeRowsForSnapshot(t, saved1.ID); got != 2 {
		t.Fatalf("node row count = %d, want 2", got)
	}
	if got := harness.CountFileStateRows(t, testSessionIDPrimary, repoRoot); got != 1 {
		t.Fatalf("file state row count = %d, want 1", got)
	}

	latest, err := store.LoadLatestSnapshot(ctx, testSessionIDPrimary, repoRoot)
	if err != nil {
		t.Fatalf("LoadLatestSnapshot returned error: %v", err)
	}
	if latest == nil {
		t.Fatal("LoadLatestSnapshot returned nil state")
	}
	if latest.Root.ID != saved1.ID {
		t.Fatalf("latest root ID = %d, want %d", latest.Root.ID, saved1.ID)
	}
	if len(latest.Nodes) != 2 {
		t.Fatalf("latest node count = %d, want 2", len(latest.Nodes))
	}
	if len(latest.FileStates) != 1 {
		t.Fatalf("latest file state count = %d, want 1", len(latest.FileStates))
	}
}

func TestSaveSnapshotStateIntegrationSupersedesOldState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newIndexStoreHarness(t)
	defer harness.Close()
	harness.ApplySQLFile(t, "internal/storage/postgres/migrations/0005_index_sync.sql", nil)

	store := New(harness.NewPool(t))
	repoRoot := filepath.Join(harness.repoRoot, "sync")
	harness.InsertSession(t, testSessionIDPrimary, repoRoot)

	first, err := store.SaveSnapshotState(
		ctx,
		indexsync.SnapshotRoot{
			SessionID: testSessionIDPrimary,
			RepoRoot:  repoRoot,
			RootHash:  "root-hash-1",
			Status:    indexsync.SnapshotStatusActive,
			IsActive:  true,
		},
		[]indexsync.MerkleNode{
			testMerkleNode("docs", "", indexsync.NodeTypeDir, "node-dir-1", "", indexsync.FileStatusActive),
			testMerkleNode("docs/a.md", "docs", indexsync.NodeTypeFile, "node-a-1", "content-a-1", indexsync.FileStatusActive),
		},
		[]indexsync.FileState{
			testFileState(testSessionIDPrimary, repoRoot, "docs/a.md", "content-a-1", "node-a-1", indexsync.FileStatusActive),
		},
	)
	if err != nil {
		t.Fatalf("first SaveSnapshotState returned error: %v", err)
	}

	second, err := store.SaveSnapshotState(
		ctx,
		indexsync.SnapshotRoot{
			SessionID:        testSessionIDPrimary,
			RepoRoot:         repoRoot,
			RootHash:         "root-hash-2",
			ParentSnapshotID: &first.ID,
			Status:           indexsync.SnapshotStatusActive,
			IsActive:         true,
		},
		[]indexsync.MerkleNode{
			testMerkleNode("docs", "", indexsync.NodeTypeDir, "node-dir-2", "", indexsync.FileStatusActive),
			testMerkleNode("docs/b.md", "docs", indexsync.NodeTypeFile, "node-b-2", "content-b-2", indexsync.FileStatusActive),
		},
		[]indexsync.FileState{
			testFileState(testSessionIDPrimary, repoRoot, "docs/b.md", "content-b-2", "node-b-2", indexsync.FileStatusActive),
		},
	)
	if err != nil {
		t.Fatalf("second SaveSnapshotState returned error: %v", err)
	}

	if got := harness.CountSnapshotRows(t, testSessionIDPrimary, repoRoot); got != 2 {
		t.Fatalf("snapshot row count = %d, want 2", got)
	}
	if got := harness.CountNodeRowsForSnapshot(t, first.ID); got != 0 {
		t.Fatalf("superseded snapshot node count = %d, want 0", got)
	}
	if got := harness.CountNodeRowsForSnapshot(t, second.ID); got != 2 {
		t.Fatalf("latest snapshot node count = %d, want 2", got)
	}
	if got := harness.CountFileStateRows(t, testSessionIDPrimary, repoRoot); got != 1 {
		t.Fatalf("latest file state row count = %d, want 1", got)
	}

	latest, err := store.LoadLatestSnapshot(ctx, testSessionIDPrimary, repoRoot)
	if err != nil {
		t.Fatalf("LoadLatestSnapshot returned error: %v", err)
	}
	if latest == nil {
		t.Fatal("LoadLatestSnapshot returned nil state")
	}
	if latest.Root.ID != second.ID {
		t.Fatalf("latest root ID = %d, want %d", latest.Root.ID, second.ID)
	}
	if latest.Root.ParentSnapshotID == nil || *latest.Root.ParentSnapshotID != first.ID {
		t.Fatalf("latest parent snapshot = %v, want %d", latest.Root.ParentSnapshotID, first.ID)
	}
	if len(latest.Nodes) != 2 {
		t.Fatalf("latest node count = %d, want 2", len(latest.Nodes))
	}
	if latest.Nodes[1].Path != "docs/b.md" {
		t.Fatalf("latest file node path = %q, want docs/b.md", latest.Nodes[1].Path)
	}
	if len(latest.FileStates) != 1 {
		t.Fatalf("latest file state count = %d, want 1", len(latest.FileStates))
	}
	if latest.FileStates[0].RelPath != "docs/b.md" {
		t.Fatalf("latest file state path = %q, want docs/b.md", latest.FileStates[0].RelPath)
	}
}

type fakeSnapshotRow struct {
	err error
}

func (f fakeSnapshotRow) Scan(...any) error {
	return f.err
}

func testMerkleNode(path, parentPath string, nodeType indexsync.NodeType, nodeHash, contentHash string, status indexsync.FileStatus) indexsync.MerkleNode {
	size := int64(len(path))
	mtime := time.Unix(0, int64(len(path))*100).UTC().UnixNano()
	return indexsync.MerkleNode{
		Path:        path,
		ParentPath:  parentPath,
		NodeType:    nodeType,
		NodeHash:    nodeHash,
		ParentHash:  "parent-" + nodeHash,
		ContentHash: contentHash,
		SizeBytes:   &size,
		MTimeNS:     &mtime,
		Status:      status,
	}
}

func testFileState(sessionID, repoRoot, relPath, contentHash, nodeHash string, status indexsync.FileStatus) indexsync.FileState {
	size := int64(len(relPath))
	mtime := time.Unix(0, int64(len(relPath))*100).UTC().UnixNano()
	return indexsync.FileState{
		SessionID:    sessionID,
		RepoRoot:     repoRoot,
		RelPath:      relPath,
		ContentHash:  contentHash,
		NodeHash:     nodeHash,
		ParentHash:   "parent-" + nodeHash,
		ChunkSetHash: "chunks-" + relPath,
		SizeBytes:    &size,
		MTimeNS:      &mtime,
		Status:       status,
	}
}

func (h *indexStoreHarness) CountSnapshotRows(t *testing.T, sessionID, repoRoot string) int {
	t.Helper()

	var count int
	if err := h.adminPool.QueryRow(
		context.Background(),
		fmt.Sprintf("SELECT COUNT(*) FROM %s.index_snapshots WHERE session_id = $1 AND repo_root = $2", h.schemaName),
		sessionID,
		repoRoot,
	).Scan(&count); err != nil {
		t.Fatalf("count snapshot rows returned error: %v", err)
	}
	return count
}

func (h *indexStoreHarness) CountNodeRowsForSnapshot(t *testing.T, snapshotID int64) int {
	t.Helper()

	var count int
	if err := h.adminPool.QueryRow(
		context.Background(),
		fmt.Sprintf("SELECT COUNT(*) FROM %s.index_nodes WHERE snapshot_id = $1", h.schemaName),
		snapshotID,
	).Scan(&count); err != nil {
		t.Fatalf("count node rows returned error: %v", err)
	}
	return count
}

func (h *indexStoreHarness) CountFileStateRows(t *testing.T, sessionID, repoRoot string) int {
	t.Helper()

	var count int
	if err := h.adminPool.QueryRow(
		context.Background(),
		fmt.Sprintf("SELECT COUNT(*) FROM %s.index_file_state WHERE session_id = $1 AND repo_root = $2", h.schemaName),
		sessionID,
		repoRoot,
	).Scan(&count); err != nil {
		t.Fatalf("count file state rows returned error: %v", err)
	}
	return count
}

func TestScanSnapshotRootPropagatesRowError(t *testing.T) {
	var root indexsync.SnapshotRoot
	err := scanSnapshotRoot(fakeSnapshotRow{err: errors.New("boom")}, &root)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("scanSnapshotRoot error = %v, want boom", err)
	}
}
