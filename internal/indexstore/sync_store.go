package indexstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/jackc/pgx/v5"
)

const (
	upsertSnapshotRootSQL = `
		INSERT INTO index_snapshots (
			session_id,
			repo_root,
			root_hash,
			parent_snapshot_id,
			status,
			is_active,
			completed_at
		)
		VALUES ($1, $2, $3, $4, $5, FALSE, $6)
		ON CONFLICT (session_id, repo_root, root_hash) DO UPDATE
		SET
			parent_snapshot_id = COALESCE(EXCLUDED.parent_snapshot_id, index_snapshots.parent_snapshot_id),
			status = EXCLUDED.status,
			completed_at = COALESCE(EXCLUDED.completed_at, index_snapshots.completed_at)
		RETURNING
			id,
			session_id::text,
			repo_root,
			root_hash,
			parent_snapshot_id,
			status,
			is_active,
			created_at,
			completed_at
	`
	updateSnapshotActivationSQL = `
		UPDATE index_snapshots
		SET
			status = $4,
			is_active = $5,
			completed_at = $6,
			parent_snapshot_id = COALESCE($7, parent_snapshot_id)
		WHERE id = $1 AND session_id = $2 AND repo_root = $3
		RETURNING
			id,
			session_id::text,
			repo_root,
			root_hash,
			parent_snapshot_id,
			status,
			is_active,
			created_at,
			completed_at
	`
	deactivateScopeSnapshotsSQL = `
		UPDATE index_snapshots
		SET
			is_active = FALSE,
			status = $4,
			completed_at = COALESCE(completed_at, $5)
		WHERE session_id = $1 AND repo_root = $2 AND id <> $3 AND is_active
	`
	upsertSnapshotNodeSQL = `
		INSERT INTO index_nodes (
			snapshot_id,
			path,
			parent_path,
			node_type,
			node_hash,
			parent_hash,
			content_hash,
			size_bytes,
			mtime_ns,
			status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (snapshot_id, path) DO UPDATE
		SET
			parent_path = EXCLUDED.parent_path,
			node_type = EXCLUDED.node_type,
			node_hash = EXCLUDED.node_hash,
			parent_hash = EXCLUDED.parent_hash,
			content_hash = EXCLUDED.content_hash,
			size_bytes = EXCLUDED.size_bytes,
			mtime_ns = EXCLUDED.mtime_ns,
			status = EXCLUDED.status
	`
	deleteSnapshotNodesOutsideSetSQL = `
		DELETE FROM index_nodes
		WHERE snapshot_id = $1 AND NOT (path = ANY($2))
	`
	deleteAllSnapshotNodesSQL = `
		DELETE FROM index_nodes
		WHERE snapshot_id = $1
	`
	deleteSupersededScopeNodesSQL = `
		DELETE FROM index_nodes n
		USING index_snapshots s
		WHERE
			n.snapshot_id = s.id AND
			s.session_id = $1 AND
			s.repo_root = $2 AND
			s.id <> $3
	`
	upsertFileStateSQL = `
		INSERT INTO index_file_state (
			session_id,
			repo_root,
			rel_path,
			last_snapshot_id,
			content_hash,
			node_hash,
			parent_hash,
			chunk_set_hash,
			size_bytes,
			mtime_ns,
			status,
			deleted_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (session_id, repo_root, rel_path) DO UPDATE
		SET
			last_snapshot_id = EXCLUDED.last_snapshot_id,
			content_hash = EXCLUDED.content_hash,
			node_hash = EXCLUDED.node_hash,
			parent_hash = EXCLUDED.parent_hash,
			chunk_set_hash = EXCLUDED.chunk_set_hash,
			size_bytes = EXCLUDED.size_bytes,
			mtime_ns = EXCLUDED.mtime_ns,
			status = EXCLUDED.status,
			deleted_at = EXCLUDED.deleted_at,
			updated_at = NOW()
	`
	deleteFileStateOutsideSetSQL = `
		DELETE FROM index_file_state
		WHERE session_id = $1 AND repo_root = $2 AND NOT (rel_path = ANY($3))
	`
	deleteAllFileStateSQL = `
		DELETE FROM index_file_state
		WHERE session_id = $1 AND repo_root = $2
	`
	selectLatestSnapshotSQL = `
		SELECT
			id,
			session_id::text,
			repo_root,
			root_hash,
			parent_snapshot_id,
			status,
			is_active,
			created_at,
			completed_at
		FROM index_snapshots
		WHERE session_id = $1 AND repo_root = $2
		ORDER BY is_active DESC, completed_at DESC NULLS LAST, created_at DESC, id DESC
		LIMIT 1
	`
	selectSnapshotNodesSQL = `
		SELECT
			id,
			snapshot_id,
			path,
			parent_path,
			node_type,
			node_hash,
			parent_hash,
			content_hash,
			size_bytes,
			mtime_ns,
			status,
			created_at
		FROM index_nodes
		WHERE snapshot_id = $1
		ORDER BY path ASC
	`
	selectFileStateSQL = `
		SELECT
			id,
			session_id::text,
			repo_root,
			rel_path,
			last_snapshot_id,
			content_hash,
			node_hash,
			parent_hash,
			chunk_set_hash,
			size_bytes,
			mtime_ns,
			status,
			deleted_at,
			updated_at
		FROM index_file_state
		WHERE session_id = $1 AND repo_root = $2
		ORDER BY rel_path ASC
	`
)

type SnapshotState struct {
	Root       indexsync.SnapshotRoot
	Nodes      []indexsync.MerkleNode
	FileStates []indexsync.FileState
}

func (s *Store) SaveSnapshotState(
	ctx context.Context,
	root indexsync.SnapshotRoot,
	nodes []indexsync.MerkleNode,
	fileStates []indexsync.FileState,
) (saved indexsync.SnapshotRoot, err error) {
	root = normalizeSnapshotRoot(root)
	if err := validateSnapshotRoot(root); err != nil {
		return indexsync.SnapshotRoot{}, err
	}
	if err := validateSnapshotNodes(nodes); err != nil {
		return indexsync.SnapshotRoot{}, err
	}
	if err := validateFileStates(root.SessionID, root.RepoRoot, fileStates); err != nil {
		return indexsync.SnapshotRoot{}, err
	}

	tx, err := s.beginTx(ctx)
	if err != nil {
		return indexsync.SnapshotRoot{}, fmt.Errorf("begin save snapshot state tx: %w", err)
	}
	defer rollbackTx(ctx, tx, &err)

	saved, err = upsertSnapshotRoot(ctx, tx, root)
	if err != nil {
		return indexsync.SnapshotRoot{}, err
	}

	if err = bulkUpsertSnapshotNodes(ctx, tx, saved.ID, nodes); err != nil {
		return indexsync.SnapshotRoot{}, err
	}
	if err = clearStaleSnapshotNodes(ctx, tx, saved.ID, nodes); err != nil {
		return indexsync.SnapshotRoot{}, err
	}
	if err = bulkUpsertFileState(ctx, tx, saved, fileStates); err != nil {
		return indexsync.SnapshotRoot{}, err
	}
	if err = clearStaleFileState(ctx, tx, saved.SessionID, saved.RepoRoot, fileStates); err != nil {
		return indexsync.SnapshotRoot{}, err
	}

	if root.IsActive {
		if _, err = tx.Exec(ctx, deactivateScopeSnapshotsSQL, saved.SessionID, saved.RepoRoot, saved.ID, indexsync.SnapshotStatusSuperseded, snapshotCompletion(root)); err != nil {
			return indexsync.SnapshotRoot{}, fmt.Errorf("deactivate superseded snapshots: %w", err)
		}
	}

	saved, err = finalizeSnapshotRoot(ctx, tx, saved, root)
	if err != nil {
		return indexsync.SnapshotRoot{}, err
	}

	if saved.IsActive {
		if _, err = tx.Exec(ctx, deleteSupersededScopeNodesSQL, saved.SessionID, saved.RepoRoot, saved.ID); err != nil {
			return indexsync.SnapshotRoot{}, fmt.Errorf("delete superseded scope nodes: %w", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return indexsync.SnapshotRoot{}, fmt.Errorf("commit save snapshot state tx: %w", err)
	}

	return saved, nil
}

func (s *Store) LoadLatestSnapshot(ctx context.Context, sessionID, repoRoot string) (*SnapshotState, error) {
	if err := validateScope(sessionID, repoRoot); err != nil {
		return nil, err
	}

	if s.queryRow == nil || s.query == nil {
		return nil, fmt.Errorf("store is missing query dependencies")
	}

	var state SnapshotState
	if err := scanSnapshotRoot(s.queryRow(ctx, selectLatestSnapshotSQL, sessionID, repoRoot), &state.Root); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select latest snapshot: %w", err)
	}

	nodes, err := s.loadSnapshotNodes(ctx, state.Root.ID)
	if err != nil {
		return nil, err
	}
	fileStates, err := s.loadFileStates(ctx, sessionID, repoRoot)
	if err != nil {
		return nil, err
	}

	state.Nodes = nodes
	state.FileStates = fileStates
	return &state, nil
}

func (s *Store) loadSnapshotNodes(ctx context.Context, snapshotID int64) ([]indexsync.MerkleNode, error) {
	rows, err := s.query(ctx, selectSnapshotNodesSQL, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query snapshot nodes: %w", err)
	}
	defer rows.Close()

	var nodes []indexsync.MerkleNode
	for rows.Next() {
		var (
			node        indexsync.MerkleNode
			parentPath  *string
			parentHash  *string
			contentHash *string
		)
		if err := rows.Scan(
			&node.ID,
			&node.SnapshotID,
			&node.Path,
			&parentPath,
			&node.NodeType,
			&node.NodeHash,
			&parentHash,
			&contentHash,
			&node.SizeBytes,
			&node.MTimeNS,
			&node.Status,
			&node.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot node: %w", err)
		}
		node.ParentPath = stringFromPtr(parentPath)
		node.ParentHash = stringFromPtr(parentHash)
		node.ContentHash = stringFromPtr(contentHash)
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot nodes: %w", err)
	}

	return nodes, nil
}

func (s *Store) loadFileStates(ctx context.Context, sessionID, repoRoot string) ([]indexsync.FileState, error) {
	rows, err := s.query(ctx, selectFileStateSQL, sessionID, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("query file state: %w", err)
	}
	defer rows.Close()

	var fileStates []indexsync.FileState
	for rows.Next() {
		var (
			state        indexsync.FileState
			contentHash  *string
			nodeHash     *string
			parentHash   *string
			chunkSetHash *string
		)
		if err := rows.Scan(
			&state.ID,
			&state.SessionID,
			&state.RepoRoot,
			&state.RelPath,
			&state.LastSnapshotID,
			&contentHash,
			&nodeHash,
			&parentHash,
			&chunkSetHash,
			&state.SizeBytes,
			&state.MTimeNS,
			&state.Status,
			&state.DeletedAt,
			&state.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan file state: %w", err)
		}
		state.ContentHash = stringFromPtr(contentHash)
		state.NodeHash = stringFromPtr(nodeHash)
		state.ParentHash = stringFromPtr(parentHash)
		state.ChunkSetHash = stringFromPtr(chunkSetHash)
		fileStates = append(fileStates, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file state rows: %w", err)
	}

	return fileStates, nil
}

func upsertSnapshotRoot(ctx context.Context, tx pgx.Tx, root indexsync.SnapshotRoot) (indexsync.SnapshotRoot, error) {
	var saved indexsync.SnapshotRoot
	if err := scanSnapshotRoot(
		tx.QueryRow(
			ctx,
			upsertSnapshotRootSQL,
			root.SessionID,
			root.RepoRoot,
			root.RootHash,
			root.ParentSnapshotID,
			root.Status,
			root.CompletedAt,
		),
		&saved,
	); err != nil {
		return indexsync.SnapshotRoot{}, fmt.Errorf("upsert snapshot root: %w", err)
	}

	return saved, nil
}

func finalizeSnapshotRoot(ctx context.Context, tx pgx.Tx, saved, requested indexsync.SnapshotRoot) (indexsync.SnapshotRoot, error) {
	status := requested.Status
	if status == "" {
		status = indexsync.SnapshotStatusPending
	}

	completedAt := requested.CompletedAt
	if requested.IsActive && completedAt == nil {
		now := time.Now().UTC()
		completedAt = &now
	}

	var updated indexsync.SnapshotRoot
	if err := scanSnapshotRoot(
		tx.QueryRow(
			ctx,
			updateSnapshotActivationSQL,
			saved.ID,
			saved.SessionID,
			saved.RepoRoot,
			status,
			requested.IsActive,
			completedAt,
			requested.ParentSnapshotID,
		),
		&updated,
	); err != nil {
		return indexsync.SnapshotRoot{}, fmt.Errorf("update snapshot activation: %w", err)
	}

	return updated, nil
}

func bulkUpsertSnapshotNodes(ctx context.Context, tx pgx.Tx, snapshotID int64, nodes []indexsync.MerkleNode) error {
	for i, node := range nodes {
		if _, err := tx.Exec(
			ctx,
			upsertSnapshotNodeSQL,
			snapshotID,
			node.Path,
			emptyToNil(node.ParentPath),
			node.NodeType,
			node.NodeHash,
			emptyToNil(node.ParentHash),
			emptyToNil(node.ContentHash),
			node.SizeBytes,
			node.MTimeNS,
			node.Status,
		); err != nil {
			return fmt.Errorf("upsert snapshot node %d: %w", i, err)
		}
	}
	return nil
}

func clearStaleSnapshotNodes(ctx context.Context, tx pgx.Tx, snapshotID int64, nodes []indexsync.MerkleNode) error {
	if len(nodes) == 0 {
		if _, err := tx.Exec(ctx, deleteAllSnapshotNodesSQL, snapshotID); err != nil {
			return fmt.Errorf("delete all snapshot nodes: %w", err)
		}
		return nil
	}

	paths := make([]string, 0, len(nodes))
	for _, node := range nodes {
		paths = append(paths, node.Path)
	}
	if _, err := tx.Exec(ctx, deleteSnapshotNodesOutsideSetSQL, snapshotID, paths); err != nil {
		return fmt.Errorf("delete stale snapshot nodes: %w", err)
	}
	return nil
}

func bulkUpsertFileState(ctx context.Context, tx pgx.Tx, snapshot indexsync.SnapshotRoot, fileStates []indexsync.FileState) error {
	for i, state := range fileStates {
		lastSnapshotID := state.LastSnapshotID
		if lastSnapshotID == nil {
			lastSnapshotID = &snapshot.ID
		}

		deletedAt := state.DeletedAt
		if state.Status == indexsync.FileStatusDeleted && deletedAt == nil {
			now := time.Now().UTC()
			deletedAt = &now
		}
		if state.Status == indexsync.FileStatusActive {
			deletedAt = nil
		}

		if _, err := tx.Exec(
			ctx,
			upsertFileStateSQL,
			snapshot.SessionID,
			snapshot.RepoRoot,
			state.RelPath,
			lastSnapshotID,
			emptyToNil(state.ContentHash),
			emptyToNil(state.NodeHash),
			emptyToNil(state.ParentHash),
			emptyToNil(state.ChunkSetHash),
			state.SizeBytes,
			state.MTimeNS,
			state.Status,
			deletedAt,
		); err != nil {
			return fmt.Errorf("upsert file state %d: %w", i, err)
		}
	}
	return nil
}

func clearStaleFileState(ctx context.Context, tx pgx.Tx, sessionID, repoRoot string, fileStates []indexsync.FileState) error {
	if len(fileStates) == 0 {
		if _, err := tx.Exec(ctx, deleteAllFileStateSQL, sessionID, repoRoot); err != nil {
			return fmt.Errorf("delete all file state rows: %w", err)
		}
		return nil
	}

	paths := make([]string, 0, len(fileStates))
	for _, state := range fileStates {
		paths = append(paths, state.RelPath)
	}
	if _, err := tx.Exec(ctx, deleteFileStateOutsideSetSQL, sessionID, repoRoot, paths); err != nil {
		return fmt.Errorf("delete stale file state rows: %w", err)
	}
	return nil
}

func validateSnapshotRoot(root indexsync.SnapshotRoot) error {
	if err := validateScope(root.SessionID, root.RepoRoot); err != nil {
		return err
	}
	if strings.TrimSpace(root.RootHash) == "" {
		return fmt.Errorf("root_hash is required")
	}
	return nil
}

func normalizeSnapshotRoot(root indexsync.SnapshotRoot) indexsync.SnapshotRoot {
	if root.Status == "" {
		root.Status = indexsync.SnapshotStatusPending
	}
	return root
}

func validateSnapshotNodes(nodes []indexsync.MerkleNode) error {
	for i, node := range nodes {
		if strings.TrimSpace(node.Path) == "" {
			return fmt.Errorf("node %d path is required", i)
		}
		if strings.TrimSpace(node.NodeHash) == "" {
			return fmt.Errorf("node %d node_hash is required", i)
		}
		if node.NodeType == "" {
			return fmt.Errorf("node %d node_type is required", i)
		}
		if node.Status == "" {
			return fmt.Errorf("node %d status is required", i)
		}
	}
	return nil
}

func validateFileStates(sessionID, repoRoot string, fileStates []indexsync.FileState) error {
	for i, state := range fileStates {
		if err := validateScope(state.SessionID, state.RepoRoot); err != nil {
			return fmt.Errorf("file state %d: %w", i, err)
		}
		if state.SessionID != sessionID {
			return fmt.Errorf("file state %d session_id %q does not match scope %q", i, state.SessionID, sessionID)
		}
		if state.RepoRoot != repoRoot {
			return fmt.Errorf("file state %d repo_root %q does not match scope %q", i, state.RepoRoot, repoRoot)
		}
		if strings.TrimSpace(state.RelPath) == "" {
			return fmt.Errorf("file state %d rel_path is required", i)
		}
		if state.Status == "" {
			return fmt.Errorf("file state %d status is required", i)
		}
	}
	return nil
}

func scanSnapshotRoot(row pgx.Row, target *indexsync.SnapshotRoot) error {
	return row.Scan(
		&target.ID,
		&target.SessionID,
		&target.RepoRoot,
		&target.RootHash,
		&target.ParentSnapshotID,
		&target.Status,
		&target.IsActive,
		&target.CreatedAt,
		&target.CompletedAt,
	)
}

func emptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func snapshotCompletion(root indexsync.SnapshotRoot) any {
	if root.CompletedAt == nil {
		return time.Now().UTC()
	}
	return root.CompletedAt
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
