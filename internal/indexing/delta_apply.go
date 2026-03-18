package indexing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

type deltaApplyStore interface {
	ListRepoIndex(context.Context, string, string) ([]indexstore.ChunkRecord, error)
	ReplaceRepoIndex(context.Context, string, string, []indexstore.ChunkRecordInput) error
}

type DeltaApplyResult struct {
	UpsertedPaths  []string
	DeletedPaths   []string
	FilesTouched   int
	ChunksReplaced int
}

type DeltaApplier struct {
	store    deltaApplyStore
	readFile func(string) ([]byte, error)
}

func NewDeltaApplier(store deltaApplyStore) *DeltaApplier {
	return &DeltaApplier{
		store:    store,
		readFile: os.ReadFile,
	}
}

func ApplyDeltaToIndex(ctx context.Context, store deltaApplyStore, sessionID, repoRoot string, delta indexsync.SyncDelta) (DeltaApplyResult, error) {
	return NewDeltaApplier(store).ApplyDeltaToIndex(ctx, sessionID, repoRoot, delta)
}

func (a *DeltaApplier) ApplyDeltaToIndex(ctx context.Context, sessionID, repoRoot string, delta indexsync.SyncDelta) (DeltaApplyResult, error) {
	if a == nil {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: applier is nil")
	}
	if a.store == nil {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: store is required")
	}
	if a.readFile == nil {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: read file function is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: session_id is required")
	}

	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: %w", err)
	}
	if err := validateDeltaScope(sessionID, canonicalRoot, delta); err != nil {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: %w", err)
	}

	existing, err := a.store.ListRepoIndex(ctx, sessionID, canonicalRoot)
	if err != nil {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: list repo index: %w", err)
	}

	upsertPaths, deletePaths, err := collectDeltaPaths(delta)
	if err != nil {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: %w", err)
	}

	documents := make([]ChunkedDocument, 0, len(upsertPaths))
	for _, relPath := range upsertPaths {
		content, err := a.readRepoFile(canonicalRoot, relPath)
		if err != nil {
			return DeltaApplyResult{}, err
		}
		documents = append(documents, ChunkedDocument{
			RelPath: relPath,
			Chunks:  ChunkDocument(string(content)),
		})
	}

	merged, err := BuildChunkDelta(sessionID, canonicalRoot, existing, documents, deletePaths)
	if err != nil {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: %w", err)
	}

	if err := a.store.ReplaceRepoIndex(ctx, sessionID, canonicalRoot, merged); err != nil {
		return DeltaApplyResult{}, fmt.Errorf("apply delta to index: replace repo index: %w", err)
	}

	return DeltaApplyResult{
		UpsertedPaths:  upsertPaths,
		DeletedPaths:   deletePaths,
		FilesTouched:   len(upsertPaths) + len(deletePaths),
		ChunksReplaced: len(merged),
	}, nil
}

func validateDeltaScope(sessionID, repoRoot string, delta indexsync.SyncDelta) error {
	if delta.SessionID != "" && strings.TrimSpace(delta.SessionID) != sessionID {
		return fmt.Errorf("delta session_id %q does not match scope %q", delta.SessionID, sessionID)
	}
	if delta.RepoRoot != "" {
		canonicalDeltaRoot, err := session.CanonicalizeRepoRoot(delta.RepoRoot)
		if err != nil {
			return fmt.Errorf("canonicalize delta repo_root: %w", err)
		}
		if canonicalDeltaRoot != repoRoot {
			return fmt.Errorf("delta repo_root %q does not match scope %q", canonicalDeltaRoot, repoRoot)
		}
	}
	return nil
}

func collectDeltaPaths(delta indexsync.SyncDelta) ([]string, []string, error) {
	upserts := make(map[string]struct{}, len(delta.Changes))
	deletes := make(map[string]struct{}, len(delta.Changes))

	for _, change := range delta.Changes {
		if change.NodeType != "" && change.NodeType != indexsync.NodeTypeFile {
			continue
		}

		path, err := normalizeRelativePath(change.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("normalize delta path %q: %w", change.Path, err)
		}

		switch change.Action {
		case indexsync.DeltaActionAdd, indexsync.DeltaActionModify:
			upserts[path] = struct{}{}
			delete(deletes, path)
		case indexsync.DeltaActionDelete:
			deletes[path] = struct{}{}
			delete(upserts, path)
		default:
			return nil, nil, fmt.Errorf("unsupported delta action %q for %q", change.Action, path)
		}
	}

	upsertPaths := mapKeys(upserts)
	deletePaths := mapKeys(deletes)
	slices.Sort(upsertPaths)
	slices.Sort(deletePaths)
	if len(upsertPaths) == 0 {
		upsertPaths = nil
	}
	if len(deletePaths) == 0 {
		deletePaths = nil
	}
	return upsertPaths, deletePaths, nil
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func (a *DeltaApplier) readRepoFile(repoRoot, relPath string) ([]byte, error) {
	cleanRelPath, err := normalizeRelativePath(relPath)
	if err != nil {
		return nil, fmt.Errorf("apply delta to index: normalize relative path %q: %w", relPath, err)
	}

	absPath := filepath.Join(repoRoot, filepath.FromSlash(cleanRelPath))
	data, err := a.readFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("apply delta to index: read %q: %w", cleanRelPath, err)
	}

	return data, nil
}
