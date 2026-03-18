package indexing

import (
	"reflect"
	"strings"
	"testing"
)

func TestChunkDocumentSmallFileSingleChunk(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"

	got := ChunkDocument(content)
	want := []Chunk{
		{Index: 0, Content: strings.TrimSpace(content)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChunkDocument() = %#v, want %#v", got, want)
	}
}

func TestChunkDocumentMultiChunkDeterministicBoundaries(t *testing.T) {
	content := strings.Join([]string{
		"line01",
		"line02",
		"line03",
		"line04",
		"line05",
	}, "\n") + "\n"

	policy := ChunkPolicy{MaxChars: 14, MaxLines: 2, OverlapLines: 1}
	got := ChunkDocumentWithPolicy(content, policy)
	want := []Chunk{
		{Index: 0, Content: "line01\nline02"},
		{Index: 1, Content: "line02\nline03"},
		{Index: 2, Content: "line03\nline04"},
		{Index: 3, Content: "line04\nline05"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChunkDocumentWithPolicy() = %#v, want %#v", got, want)
	}

	gotRepeat := ChunkDocumentWithPolicy(content, policy)
	if !reflect.DeepEqual(gotRepeat, want) {
		t.Fatalf("ChunkDocumentWithPolicy() repeat = %#v, want %#v", gotRepeat, want)
	}
}

func TestChunkDocumentBoundaryConditions(t *testing.T) {
	content := "12345\n67890\n"

	t.Run("exact size limit stays in one chunk", func(t *testing.T) {
		got := ChunkDocumentWithPolicy(content, ChunkPolicy{MaxChars: len(content), MaxLines: 2, OverlapLines: 0})
		want := []Chunk{{Index: 0, Content: strings.TrimSpace(content)}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ChunkDocumentWithPolicy() = %#v, want %#v", got, want)
		}
	})

	t.Run("line limit forces split", func(t *testing.T) {
		got := ChunkDocumentWithPolicy(content, ChunkPolicy{MaxChars: 100, MaxLines: 1, OverlapLines: 0})
		want := []Chunk{
			{Index: 0, Content: "12345"},
			{Index: 1, Content: "67890"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ChunkDocumentWithPolicy() = %#v, want %#v", got, want)
		}
	})
}

func TestChunkDocumentOversizedInputHandling(t *testing.T) {
	longLine := strings.Repeat("a", DefaultChunkMaxChars*3)

	got := ChunkDocument(longLine)
	want := []Chunk{
		{Index: 0, Content: longLine},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChunkDocument() = %#v, want %#v", got, want)
	}
}

func TestChunkDocumentNeverEmitsEmptyChunks(t *testing.T) {
	got := ChunkDocument(" \n\t\n")
	if len(got) != 0 {
		t.Fatalf("ChunkDocument() = %#v, want no chunks", got)
	}
}
