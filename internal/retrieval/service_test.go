package retrieval

import (
	"context"
	"strings"
	"testing"
)

func TestSemanticRetrievalContracts(t *testing.T) {
	t.Parallel()

	t.Run("validates required request fields", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name string
			req  SemanticQueryRequest
			want string
		}{
			{
				name: "missing session",
				req: SemanticQueryRequest{
					RepoRoot: "/repo",
					Query:    "where is the registry",
					TopK:     3,
				},
				want: "session_id is required",
			},
			{
				name: "missing repo",
				req: SemanticQueryRequest{
					SessionID: "session-1",
					Query:     "where is the registry",
					TopK:      3,
				},
				want: "repo_root is required",
			},
			{
				name: "missing query",
				req: SemanticQueryRequest{
					SessionID: "session-1",
					RepoRoot:  "/repo",
					TopK:      3,
				},
				want: "query is required",
			},
			{
				name: "invalid top k",
				req: SemanticQueryRequest{
					SessionID: "session-1",
					RepoRoot:  "/repo",
					Query:     "where is the registry",
				},
				want: "top_k must be positive",
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				err := ValidateSemanticQueryRequest(tc.req)
				if err == nil || err.Error() != tc.want {
					t.Fatalf("ValidateSemanticQueryRequest() error = %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("normalizes scope and bounds snippets", func(t *testing.T) {
		t.Parallel()

		service := NewService(func(_ context.Context, req SemanticQueryRequest) (SemanticQueryResponse, error) {
			if req.SessionID != "session-1" {
				t.Fatalf("SessionID = %q, want trimmed session-1", req.SessionID)
			}
			if req.RepoRoot != "/repo/project" {
				t.Fatalf("RepoRoot = %q, want trimmed /repo/project", req.RepoRoot)
			}
			if req.Query != "where is the registry" {
				t.Fatalf("Query = %q, want trimmed query", req.Query)
			}

			return SemanticQueryResponse{
				Index: SemanticIndexReadiness{
					Ready: true,
				},
				Results: []SemanticQueryResult{
					{
						Rank:     1,
						FilePath: "internal/tools/registry/static.go",
						ChunkID:  "internal/tools/registry/static.go#12",
						Score:    0.8731,
						Snippet:  strings.Repeat("x", MaxSnippetLength+25),
					},
				},
			}, nil
		})

		got, err := service.Query(context.Background(), SemanticQueryRequest{
			SessionID: " session-1 ",
			RepoRoot:  " /repo/project ",
			Query:     " where is the registry ",
			TopK:      5,
		})
		if err != nil {
			t.Fatalf("Query returned error: %v", err)
		}

		if got.SessionID != "session-1" || got.RepoRoot != "/repo/project" || got.Query != "where is the registry" {
			t.Fatalf("response scope = %+v, want normalized request fields", got)
		}
		if got.TopK != 5 {
			t.Fatalf("TopK = %d, want 5", got.TopK)
		}
		if !got.Index.Ready {
			t.Fatal("Index.Ready = false, want true")
		}
		if got.Index.Status != IndexStatusUnknown {
			t.Fatalf("Index.Status = %q, want %q", got.Index.Status, IndexStatusUnknown)
		}
		if len(got.Results) != 1 {
			t.Fatalf("len(Results) = %d, want 1", len(got.Results))
		}

		result := got.Results[0]
		if result.Rank != 1 {
			t.Fatalf("Rank = %d, want 1", result.Rank)
		}
		if result.FilePath == "" || result.ChunkID == "" {
			t.Fatalf("result identifiers = %+v, want file_path and chunk_id", result)
		}
		if result.Score != 0.8731 {
			t.Fatalf("Score = %v, want 0.8731", result.Score)
		}
		if len(result.Snippet) != MaxSnippetLength {
			t.Fatalf("snippet length = %d, want %d", len(result.Snippet), MaxSnippetLength)
		}
	})

	t.Run("errors when retriever is not configured", func(t *testing.T) {
		t.Parallel()

		service := NewService(nil)
		_, err := service.Query(context.Background(), SemanticQueryRequest{
			SessionID: "session-1",
			RepoRoot:  "/repo/project",
			Query:     "where is the registry",
			TopK:      5,
		})
		if err == nil || err.Error() != "semantic retriever is not configured" {
			t.Fatalf("Query() error = %v, want semantic retriever is not configured", err)
		}
	})
}
