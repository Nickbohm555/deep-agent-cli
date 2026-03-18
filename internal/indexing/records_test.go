package indexing

import (
	"reflect"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

func TestBuildChunkRecordsProjectsMetadataDeterministically(t *testing.T) {
	repoRoot := copyDiscoveryFixture(t)
	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		t.Fatalf("CanonicalizeRepoRoot(%q) returned error: %v", repoRoot, err)
	}

	documents := []ChunkedDocument{
		{
			RelPath: "src/main.go",
			Chunks: []Chunk{
				{Index: 0, Content: "package main\n"},
				{Index: 1, Content: "func main() {}\n"},
			},
		},
		{
			RelPath: "./docs/guide.txt",
			Chunks: []Chunk{
				{Index: 0, Content: "guide content\n"},
			},
		},
	}

	got, err := BuildChunkRecords("session-123", repoRoot, documents)
	if err != nil {
		t.Fatalf("BuildChunkRecords returned error: %v", err)
	}

	want := []indexstore.ChunkRecordInput{
		{
			SessionID:     "session-123",
			RepoRoot:      canonicalRoot,
			RelPath:       "src/main.go",
			ChunkIndex:    0,
			Content:       "package main\n",
			ContentHash:   chunkContentHash("src/main.go", 0, "package main\n"),
			Model:         "",
			EmbeddingDims: 0,
			Embedding:     nil,
		},
		{
			SessionID:     "session-123",
			RepoRoot:      canonicalRoot,
			RelPath:       "src/main.go",
			ChunkIndex:    1,
			Content:       "func main() {}\n",
			ContentHash:   chunkContentHash("src/main.go", 1, "func main() {}\n"),
			Model:         "",
			EmbeddingDims: 0,
			Embedding:     nil,
		},
		{
			SessionID:     "session-123",
			RepoRoot:      canonicalRoot,
			RelPath:       "docs/guide.txt",
			ChunkIndex:    0,
			Content:       "guide content\n",
			ContentHash:   chunkContentHash("docs/guide.txt", 0, "guide content\n"),
			Model:         "",
			EmbeddingDims: 0,
			Embedding:     nil,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildChunkRecords() = %#v, want %#v", got, want)
	}

	gotRepeat, err := BuildChunkRecords("session-123", repoRoot, documents)
	if err != nil {
		t.Fatalf("BuildChunkRecords repeat returned error: %v", err)
	}
	if !reflect.DeepEqual(gotRepeat, want) {
		t.Fatalf("BuildChunkRecords repeat = %#v, want %#v", gotRepeat, want)
	}
}

func TestBuildChunkRecordsRejectsInvalidInput(t *testing.T) {
	repoRoot := copyDiscoveryFixture(t)

	testCases := []struct {
		name      string
		sessionID string
		repo      string
		documents []ChunkedDocument
	}{
		{
			name:      "missing session",
			sessionID: "",
			repo:      repoRoot,
			documents: []ChunkedDocument{{RelPath: "README.md", Chunks: []Chunk{{Index: 0, Content: "content"}}}},
		},
		{
			name:      "escaping rel path",
			sessionID: "session-123",
			repo:      repoRoot,
			documents: []ChunkedDocument{{RelPath: "../README.md", Chunks: []Chunk{{Index: 0, Content: "content"}}}},
		},
		{
			name:      "non-contiguous chunk index",
			sessionID: "session-123",
			repo:      repoRoot,
			documents: []ChunkedDocument{{RelPath: "README.md", Chunks: []Chunk{{Index: 1, Content: "content"}}}},
		},
		{
			name:      "empty chunk content",
			sessionID: "session-123",
			repo:      repoRoot,
			documents: []ChunkedDocument{{RelPath: "README.md", Chunks: []Chunk{{Index: 0, Content: "   "}}}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := BuildChunkRecords(testCase.sessionID, testCase.repo, testCase.documents); err == nil {
				t.Fatalf("BuildChunkRecords(%q) error = nil, want error", testCase.name)
			}
		})
	}
}

func TestChunkContentHashStableAndDistinct(t *testing.T) {
	hashOne := chunkContentHash("docs/guide.txt", 0, "same content")
	hashTwo := chunkContentHash("docs/guide.txt", 0, "same content")
	if hashOne != hashTwo {
		t.Fatalf("chunkContentHash stable mismatch: %q != %q", hashOne, hashTwo)
	}

	distinctInputs := []string{
		chunkContentHash("docs/guide.txt", 1, "same content"),
		chunkContentHash("docs/other.txt", 0, "same content"),
		chunkContentHash("docs/guide.txt", 0, "different content"),
	}
	for _, hash := range distinctInputs {
		if hash == hashOne {
			t.Fatalf("chunkContentHash collision for distinct input: %q", hash)
		}
	}
}
