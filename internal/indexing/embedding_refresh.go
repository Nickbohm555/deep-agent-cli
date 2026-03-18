package indexing

import (
	"context"
	"fmt"
	"slices"

	"github.com/Nickbohm555/deep-agent-cli/internal/embeddings"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
)

type deltaApplyEmbedder interface {
	EmbedTexts(context.Context, []string) (embeddings.Result, error)
}

func RefreshEmbeddingsForChangedChunks(
	ctx context.Context,
	existing []indexstore.ChunkRecord,
	records []indexstore.ChunkRecordInput,
	embedder deltaApplyEmbedder,
) ([]indexstore.ChunkRecordInput, error) {
	if len(records) == 0 {
		return nil, nil
	}

	existingByChunk := make(map[string]indexstore.ChunkRecord, len(existing))
	for _, record := range existing {
		existingByChunk[chunkRecordKey(record.RelPath, record.ChunkIndex)] = record
	}

	refreshed := cloneChunkInputs(records)
	pendingIndexes := make([]int, 0, len(refreshed))
	pendingTexts := make([]string, 0, len(refreshed))

	for i := range refreshed {
		existingRecord, ok := existingByChunk[chunkRecordKey(refreshed[i].RelPath, refreshed[i].ChunkIndex)]
		if ok && existingRecord.ContentHash == refreshed[i].ContentHash {
			refreshed[i].Model = existingRecord.Model
			refreshed[i].EmbeddingDims = existingRecord.EmbeddingDims
			refreshed[i].Embedding = append([]float32(nil), existingRecord.Embedding...)
			continue
		}

		pendingIndexes = append(pendingIndexes, i)
		pendingTexts = append(pendingTexts, refreshed[i].Content)
	}

	if len(pendingIndexes) == 0 {
		return refreshed, nil
	}
	if embedder == nil {
		return nil, fmt.Errorf("refresh embeddings for changed chunks: embedder is required")
	}

	result, err := embedder.EmbedTexts(ctx, pendingTexts)
	if err != nil {
		return nil, fmt.Errorf("refresh embeddings for changed chunks: embed texts: %w", err)
	}
	if result.Dimensions <= 0 {
		return nil, fmt.Errorf("refresh embeddings for changed chunks: embedding dimensions must be positive")
	}
	if len(result.Vectors) != len(pendingIndexes) {
		return nil, fmt.Errorf(
			"refresh embeddings for changed chunks: embedding count mismatch: got %d vectors for %d records",
			len(result.Vectors),
			len(pendingIndexes),
		)
	}

	for i, recordIndex := range pendingIndexes {
		vector := result.Vectors[i]
		if len(vector) != result.Dimensions {
			return nil, fmt.Errorf(
				"refresh embeddings for changed chunks: embedding dimensions mismatch for record %d: got %d, want %d",
				recordIndex,
				len(vector),
				result.Dimensions,
			)
		}

		refreshed[recordIndex].Model = result.Model
		refreshed[recordIndex].EmbeddingDims = result.Dimensions
		refreshed[recordIndex].Embedding = append([]float32(nil), vector...)
	}

	return refreshed, nil
}

func mergeChunkRecords(
	sessionID, repoRoot string,
	existing []indexstore.ChunkRecord,
	refreshed []indexstore.ChunkRecordInput,
	deletePaths []string,
) ([]indexstore.ChunkRecordInput, error) {
	removed := make(map[string]struct{}, len(deletePaths)+len(refreshed))
	for _, path := range deletePaths {
		normalized, err := normalizeRelativePath(path)
		if err != nil {
			return nil, fmt.Errorf("merge chunk records: normalize delete path %q: %w", path, err)
		}
		removed[normalized] = struct{}{}
	}

	for _, record := range refreshed {
		normalized, err := normalizeRelativePath(record.RelPath)
		if err != nil {
			return nil, fmt.Errorf("merge chunk records: normalize refreshed path %q: %w", record.RelPath, err)
		}
		removed[normalized] = struct{}{}
	}

	merged := make([]indexstore.ChunkRecordInput, 0, len(existing)+len(refreshed))
	for i, record := range existing {
		if err := validateExistingChunkScope(sessionID, repoRoot, record); err != nil {
			return nil, fmt.Errorf("merge chunk records: validate existing chunk %d: %w", i, err)
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

	merged = append(merged, refreshed...)
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

func chunkRecordKey(relPath string, chunkIndex int) string {
	return fmt.Sprintf("%s:%d", relPath, chunkIndex)
}
