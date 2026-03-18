package indexing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/embeddings"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

const (
	defaultChunkMaxChars     = 1200
	defaultChunkMaxLines     = 40
	defaultChunkOverlapLines = 5
)

type rebuildEmbedder interface {
	EmbedTexts(context.Context, []string) (embeddings.Result, error)
}

type rebuildStore interface {
	ReplaceRepoIndex(context.Context, string, string, []indexstore.ChunkRecordInput) error
}

type FullRebuildResult struct {
	FilesIndexed   int
	ChunksEmbedded int
	Model          string
	Dimensions     int
}

type Rebuilder struct {
	discoverFiles func(string) ([]string, error)
	readFile      func(string) ([]byte, error)
	embedder      rebuildEmbedder
	store         rebuildStore
}

func NewRebuilder(store rebuildStore, embedder rebuildEmbedder) *Rebuilder {
	return &Rebuilder{
		discoverFiles: DiscoverIndexableFiles,
		readFile:      os.ReadFile,
		embedder:      embedder,
		store:         store,
	}
}

func RunFullRebuild(ctx context.Context, store rebuildStore, embedder rebuildEmbedder, sessionID, repoRoot string) (FullRebuildResult, error) {
	return NewRebuilder(store, embedder).RunFullRebuild(ctx, sessionID, repoRoot)
}

func (r *Rebuilder) RunFullRebuild(ctx context.Context, sessionID, repoRoot string) (FullRebuildResult, error) {
	if r == nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: rebuilder is nil")
	}
	if strings.TrimSpace(sessionID) == "" {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: session_id is required")
	}
	if r.store == nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: store is required")
	}
	if r.embedder == nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: embedder is required")
	}
	if r.discoverFiles == nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: discoverer is required")
	}
	if r.readFile == nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: read file function is required")
	}

	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: %w", err)
	}

	relPaths, err := r.discoverFiles(canonicalRoot)
	if err != nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: discover files: %w", err)
	}

	documents := make([]ChunkedDocument, 0, len(relPaths))
	for _, relPath := range relPaths {
		content, err := r.readRepoFile(canonicalRoot, relPath)
		if err != nil {
			return FullRebuildResult{}, err
		}

		chunks := chunkTextDeterministically(string(content))
		if len(chunks) == 0 {
			continue
		}

		documents = append(documents, ChunkedDocument{
			RelPath: relPath,
			Chunks:  chunks,
		})
	}

	records, err := BuildChunkRecords(sessionID, canonicalRoot, documents)
	if err != nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: build chunk records: %w", err)
	}

	if len(records) == 0 {
		if err := r.store.ReplaceRepoIndex(ctx, sessionID, canonicalRoot, nil); err != nil {
			return FullRebuildResult{}, fmt.Errorf("run full rebuild: replace repo index: %w", err)
		}
		return FullRebuildResult{FilesIndexed: len(relPaths)}, nil
	}

	texts := make([]string, 0, len(records))
	for _, record := range records {
		texts = append(texts, record.Content)
	}

	embeddingResult, err := r.embedder.EmbedTexts(ctx, texts)
	if err != nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: embed records: %w", err)
	}
	if embeddingResult.Dimensions <= 0 {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: embedding dimensions must be positive")
	}
	if len(embeddingResult.Vectors) != len(records) {
		return FullRebuildResult{}, fmt.Errorf(
			"run full rebuild: embedding count mismatch: got %d vectors for %d records",
			len(embeddingResult.Vectors),
			len(records),
		)
	}

	for i := range records {
		vector := embeddingResult.Vectors[i]
		if len(vector) != embeddingResult.Dimensions {
			return FullRebuildResult{}, fmt.Errorf(
				"run full rebuild: embedding dimensions mismatch for record %d: got %d, want %d",
				i,
				len(vector),
				embeddingResult.Dimensions,
			)
		}

		records[i].Model = embeddingResult.Model
		records[i].EmbeddingDims = embeddingResult.Dimensions
		records[i].Embedding = append([]float32(nil), vector...)
	}

	if err := r.store.ReplaceRepoIndex(ctx, sessionID, canonicalRoot, records); err != nil {
		return FullRebuildResult{}, fmt.Errorf("run full rebuild: replace repo index: %w", err)
	}

	return FullRebuildResult{
		FilesIndexed:   len(relPaths),
		ChunksEmbedded: len(records),
		Model:          embeddingResult.Model,
		Dimensions:     embeddingResult.Dimensions,
	}, nil
}

func (r *Rebuilder) readRepoFile(repoRoot, relPath string) ([]byte, error) {
	cleanRelPath, err := normalizeRelativePath(relPath)
	if err != nil {
		return nil, fmt.Errorf("run full rebuild: normalize relative path %q: %w", relPath, err)
	}

	absPath := filepath.Join(repoRoot, filepath.FromSlash(cleanRelPath))
	data, err := r.readFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("run full rebuild: read %q: %w", cleanRelPath, err)
	}

	return data, nil
}

func chunkTextDeterministically(content string) []Chunk {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if strings.TrimSpace(normalized) == "" {
		return nil
	}

	lines := strings.SplitAfter(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	chunks := make([]Chunk, 0, max(1, len(lines)/defaultChunkMaxLines+1))
	start := 0
	for start < len(lines) {
		end := start
		charCount := 0
		for end < len(lines) {
			nextChars := charCount + len(lines[end])
			nextLineCount := end - start + 1
			if end > start && (nextChars > defaultChunkMaxChars || nextLineCount > defaultChunkMaxLines) {
				break
			}
			charCount = nextChars
			end++
			if charCount >= defaultChunkMaxChars || nextLineCount >= defaultChunkMaxLines {
				break
			}
		}

		if end == start {
			end = start + 1
		}

		chunkContent := strings.TrimSpace(strings.Join(lines[start:end], ""))
		if chunkContent != "" {
			chunks = append(chunks, Chunk{
				Index:   len(chunks),
				Content: chunkContent,
			})
		}

		if end >= len(lines) {
			break
		}

		nextStart := end - defaultChunkOverlapLines
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}

	return chunks
}
