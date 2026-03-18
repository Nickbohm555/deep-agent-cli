package indexstore

import "time"

const DefaultEmbeddingDimensions = 1536

type ChunkRecord struct {
	ID            int64
	SessionID     string
	RepoRoot      string
	RelPath       string
	ChunkIndex    int
	Content       string
	ContentHash   string
	Model         string
	EmbeddingDims int
	Embedding     []float32
	CreatedAt     time.Time
}

type ChunkRecordInput struct {
	SessionID     string
	RepoRoot      string
	RelPath       string
	ChunkIndex    int
	Content       string
	ContentHash   string
	Model         string
	EmbeddingDims int
	Embedding     []float32
}
