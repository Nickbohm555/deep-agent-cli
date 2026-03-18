package indexing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

type Chunk struct {
	Index   int
	Content string
}

type ChunkedDocument struct {
	RelPath string
	Chunks  []Chunk
}

func BuildChunkRecords(sessionID, repoRoot string, documents []ChunkedDocument) ([]indexstore.ChunkRecordInput, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("build chunk records: session_id is required")
	}

	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("build chunk records: %w", err)
	}

	records := make([]indexstore.ChunkRecordInput, 0)
	for _, document := range documents {
		relPath, err := normalizeRelativePath(document.RelPath)
		if err != nil {
			return nil, fmt.Errorf("build chunk records for %q: %w", document.RelPath, err)
		}

		for i, chunk := range document.Chunks {
			if chunk.Index != i {
				return nil, fmt.Errorf("build chunk records for %q: chunk index %d does not match expected contiguous index %d", relPath, chunk.Index, i)
			}
			if strings.TrimSpace(chunk.Content) == "" {
				return nil, fmt.Errorf("build chunk records for %q: chunk %d content is empty", relPath, chunk.Index)
			}

			records = append(records, indexstore.ChunkRecordInput{
				SessionID:     sessionID,
				RepoRoot:      canonicalRoot,
				RelPath:       relPath,
				ChunkIndex:    chunk.Index,
				Content:       chunk.Content,
				ContentHash:   chunkContentHash(relPath, chunk.Index, chunk.Content),
				Model:         "",
				EmbeddingDims: 0,
				Embedding:     nil,
			})
		}
	}

	return records, nil
}

func normalizeRelativePath(relPath string) (string, error) {
	trimmed := strings.TrimSpace(relPath)
	if trimmed == "" {
		return "", fmt.Errorf("rel_path is required")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("rel_path must be relative: %q", relPath)
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return "", fmt.Errorf("rel_path is required")
	}

	parts := strings.Split(filepath.ToSlash(cleaned), "/")
	for _, part := range parts {
		if part == ".." {
			return "", fmt.Errorf("rel_path escapes repo root: %q", relPath)
		}
	}

	return filepath.ToSlash(cleaned), nil
}

func chunkContentHash(relPath string, chunkIndex int, content string) string {
	digest := sha256.Sum256([]byte(relPath + "\n" + strconv.Itoa(chunkIndex) + "\n" + content))
	return hex.EncodeToString(digest[:])
}
