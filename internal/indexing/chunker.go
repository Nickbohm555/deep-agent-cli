package indexing

import "strings"

const (
	DefaultChunkMaxChars     = 1200
	DefaultChunkMaxLines     = 40
	DefaultChunkOverlapLines = 5
)

type ChunkPolicy struct {
	MaxChars     int
	MaxLines     int
	OverlapLines int
}

var DefaultChunkPolicy = ChunkPolicy{
	MaxChars:     DefaultChunkMaxChars,
	MaxLines:     DefaultChunkMaxLines,
	OverlapLines: DefaultChunkOverlapLines,
}

func ChunkDocument(content string) []Chunk {
	return ChunkDocumentWithPolicy(content, DefaultChunkPolicy)
}

func ChunkDocumentWithPolicy(content string, policy ChunkPolicy) []Chunk {
	policy = policy.withDefaults()

	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if strings.TrimSpace(normalized) == "" {
		return nil
	}

	lines := strings.SplitAfter(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	chunks := make([]Chunk, 0, max(1, len(lines)/policy.MaxLines+1))
	start := 0
	for start < len(lines) {
		end := start
		charCount := 0
		for end < len(lines) {
			nextChars := charCount + len(lines[end])
			nextLineCount := end - start + 1
			if end > start && (nextChars > policy.MaxChars || nextLineCount > policy.MaxLines) {
				break
			}
			charCount = nextChars
			end++
			if charCount >= policy.MaxChars || nextLineCount >= policy.MaxLines {
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

		nextStart := end - policy.OverlapLines
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}

	return chunks
}

func (p ChunkPolicy) withDefaults() ChunkPolicy {
	if p.MaxChars <= 0 {
		p.MaxChars = DefaultChunkMaxChars
	}
	if p.MaxLines <= 0 {
		p.MaxLines = DefaultChunkMaxLines
	}
	if p.OverlapLines < 0 {
		p.OverlapLines = 0
	}
	if p.OverlapLines >= p.MaxLines {
		p.OverlapLines = p.MaxLines - 1
	}
	if p.OverlapLines < 0 {
		p.OverlapLines = 0
	}
	return p
}
