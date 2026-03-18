package indexing

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
)

func BuildChunkDelta(sessionID, repoRoot string, existing []indexstore.ChunkRecord, documents []ChunkedDocument, deletePaths []string) ([]indexstore.ChunkRecordInput, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("build chunk delta: session_id is required")
	}
	if strings.TrimSpace(repoRoot) == "" {
		return nil, fmt.Errorf("build chunk delta: repo_root is required")
	}

	removed := make(map[string]struct{}, len(deletePaths)+len(documents))
	for _, path := range deletePaths {
		normalized, err := normalizeRelativePath(path)
		if err != nil {
			return nil, fmt.Errorf("build chunk delta: normalize delete path %q: %w", path, err)
		}
		removed[normalized] = struct{}{}
	}

	for _, document := range documents {
		normalized, err := normalizeRelativePath(document.RelPath)
		if err != nil {
			return nil, fmt.Errorf("build chunk delta: normalize document path %q: %w", document.RelPath, err)
		}
		removed[normalized] = struct{}{}
	}

	merged := make([]indexstore.ChunkRecordInput, 0, len(existing))
	for i, record := range existing {
		if err := validateExistingChunkScope(sessionID, repoRoot, record); err != nil {
			return nil, fmt.Errorf("build chunk delta: validate existing chunk %d: %w", i, err)
		}
		if _, ok := removed[record.RelPath]; ok {
			continue
		}
		merged = append(merged, indexstore.ChunkRecordInput{
			SessionID:     record.SessionID,
			RepoRoot:      record.RepoRoot,
			RelPath:       record.RelPath,
			ChunkIndex:    record.ChunkIndex,
			Content:       record.Content,
			ContentHash:   record.ContentHash,
			Model:         record.Model,
			EmbeddingDims: record.EmbeddingDims,
			Embedding:     append([]float32(nil), record.Embedding...),
		})
	}

	newRecords, err := BuildChunkRecords(sessionID, repoRoot, documents)
	if err != nil {
		return nil, fmt.Errorf("build chunk delta: %w", err)
	}
	merged = append(merged, newRecords...)

	slices.SortFunc(merged, func(a, b indexstore.ChunkRecordInput) int {
		if a.RelPath != b.RelPath {
			if a.RelPath < b.RelPath {
				return -1
			}
			return 1
		}
		return a.ChunkIndex - b.ChunkIndex
	})

	return merged, nil
}

func DeleteChunksForFile(existing []indexstore.ChunkRecord, relPath string) ([]indexstore.ChunkRecord, error) {
	normalized, err := normalizeRelativePath(relPath)
	if err != nil {
		return nil, fmt.Errorf("delete chunks for file: %w", err)
	}

	filtered := make([]indexstore.ChunkRecord, 0, len(existing))
	for _, record := range existing {
		if record.RelPath == normalized {
			continue
		}
		filtered = append(filtered, record)
	}

	return filtered, nil
}

func validateExistingChunkScope(sessionID, repoRoot string, record indexstore.ChunkRecord) error {
	if record.SessionID != sessionID {
		return fmt.Errorf("chunk session_id %q does not match scope %q", record.SessionID, sessionID)
	}
	if record.RepoRoot != repoRoot {
		return fmt.Errorf("chunk repo_root %q does not match scope %q", record.RepoRoot, repoRoot)
	}
	return nil
}
