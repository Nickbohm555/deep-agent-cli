package retrieval

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/embeddings"
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

func TestScoreFromCosineDistance(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		distance float64
		want     float64
	}{
		{name: "perfect match", distance: 0, want: 1},
		{name: "partial match", distance: 0.25, want: 0.75},
		{name: "orthogonal", distance: 1, want: 0},
		{name: "opposite direction", distance: 2, want: -1},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ScoreFromCosineDistance(tc.distance); got != tc.want {
				t.Fatalf("ScoreFromCosineDistance(%v) = %v, want %v", tc.distance, got, tc.want)
			}
		})
	}
}

func TestApplyStableRanks(t *testing.T) {
	t.Parallel()

	input := []SemanticQueryResult{
		{Rank: 99, FilePath: "zeta.go", ChunkID: "zeta.go#1", Score: 0.5},
		{Rank: 88, FilePath: "alpha.go", ChunkID: "alpha.go#2", Score: 0.9},
		{Rank: 77, FilePath: "alpha.go", ChunkID: "alpha.go#1", Score: 0.9},
		{Rank: 66, FilePath: "beta.go", ChunkID: "beta.go#1", Score: 0.9},
	}

	got := ApplyStableRanks(input)

	want := []SemanticQueryResult{
		{Rank: 1, FilePath: "alpha.go", ChunkID: "alpha.go#1", Score: 0.9},
		{Rank: 2, FilePath: "alpha.go", ChunkID: "alpha.go#2", Score: 0.9},
		{Rank: 3, FilePath: "beta.go", ChunkID: "beta.go#1", Score: 0.9},
		{Rank: 4, FilePath: "zeta.go", ChunkID: "zeta.go#1", Score: 0.5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApplyStableRanks() = %#v, want %#v", got, want)
	}

	if input[0].Rank != 99 || input[1].Rank != 88 || input[2].Rank != 77 || input[3].Rank != 66 {
		t.Fatalf("ApplyStableRanks() mutated input ranks: %#v", input)
	}
}

func TestServiceQueryIndexNotReady(t *testing.T) {
	t.Parallel()

	snapshotID := int64(17)
	updatedAt := time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC)
	readiness := &stubReadinessChecker{
		resp: SemanticIndexReadiness{
			Ready:      false,
			SnapshotID: &snapshotID,
			UpdatedAt:  &updatedAt,
		},
	}
	embedder := &stubQueryEmbedder{}
	store := &stubQueryStore{}
	service := NewOrchestratedService(readiness, embedder, store)

	resp, err := service.Query(context.Background(), SemanticQueryRequest{
		SessionID: " session-1 ",
		RepoRoot:  " /repo/project ",
		Query:     " where is the registry ",
		TopK:      3,
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	if readiness.calls != 1 {
		t.Fatalf("readiness calls = %d, want 1", readiness.calls)
	}
	if readiness.sessionID != "session-1" || readiness.repoRoot != "/repo/project" {
		t.Fatalf("readiness scope = %q %q, want trimmed request scope", readiness.sessionID, readiness.repoRoot)
	}
	if embedder.calls != 0 {
		t.Fatalf("embedder calls = %d, want 0 when index is not ready", embedder.calls)
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0 when index is not ready", store.calls)
	}
	if resp.Index.Ready {
		t.Fatal("Index.Ready = true, want false")
	}
	if resp.Index.Status != "index_not_ready" {
		t.Fatalf("Index.Status = %q, want index_not_ready", resp.Index.Status)
	}
	if resp.Index.SnapshotID == nil || *resp.Index.SnapshotID != snapshotID {
		t.Fatalf("SnapshotID = %v, want %d", resp.Index.SnapshotID, snapshotID)
	}
	if resp.Index.UpdatedAt == nil || !resp.Index.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("UpdatedAt = %v, want %v", resp.Index.UpdatedAt, updatedAt)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("len(Results) = %d, want 0", len(resp.Results))
	}
}

func TestServiceQueryRanksDeterministically(t *testing.T) {
	t.Parallel()

	service := NewOrchestratedService(
		&stubReadinessChecker{
			resp: SemanticIndexReadiness{
				Ready:  true,
				Status: "ready",
			},
		},
		&stubQueryEmbedder{
			vector: []float32{0.1, 0.2, 0.3},
		},
		&stubQueryStore{
			results: []SemanticQueryResult{
				{Rank: 88, FilePath: "beta.go", ChunkID: "beta.go#2", Score: 0.8, Snippet: "beta"},
				{Rank: 77, FilePath: "alpha.go", ChunkID: "alpha.go#2", Score: 0.9, Snippet: strings.Repeat("x", MaxSnippetLength+5)},
				{Rank: 66, FilePath: "alpha.go", ChunkID: "alpha.go#1", Score: 0.9, Snippet: "alpha"},
			},
		},
	)

	resp, err := service.Query(context.Background(), SemanticQueryRequest{
		SessionID: "session-1",
		RepoRoot:  "/repo/project",
		Query:     "where is the registry",
		TopK:      3,
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	if resp.Index.Status != "ready" {
		t.Fatalf("Index.Status = %q, want ready", resp.Index.Status)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("len(Results) = %d, want 3", len(resp.Results))
	}

	want := []SemanticQueryResult{
		{Rank: 1, FilePath: "alpha.go", ChunkID: "alpha.go#1", Score: 0.9, Snippet: "alpha"},
		{Rank: 2, FilePath: "alpha.go", ChunkID: "alpha.go#2", Score: 0.9, Snippet: strings.Repeat("x", MaxSnippetLength)},
		{Rank: 3, FilePath: "beta.go", ChunkID: "beta.go#2", Score: 0.8, Snippet: "beta"},
	}
	if !reflect.DeepEqual(resp.Results, want) {
		t.Fatalf("Results = %#v, want %#v", resp.Results, want)
	}
}

type stubReadinessChecker struct {
	resp      SemanticIndexReadiness
	err       error
	calls     int
	sessionID string
	repoRoot  string
}

func (s *stubReadinessChecker) GetReadiness(_ context.Context, sessionID, repoRoot string) (SemanticIndexReadiness, error) {
	s.calls++
	s.sessionID = sessionID
	s.repoRoot = repoRoot
	return s.resp, s.err
}

type stubQueryEmbedder struct {
	vector []float32
	err    error
	calls  int
	query  []string
}

func (s *stubQueryEmbedder) EmbedTexts(_ context.Context, texts []string) (embeddings.Result, error) {
	s.calls++
	s.query = append([]string(nil), texts...)
	if s.err != nil {
		return embeddings.Result{}, s.err
	}
	return embeddings.Result{
		Model:      "test-model",
		Dimensions: len(s.vector),
		Vectors:    [][]float32{append([]float32(nil), s.vector...)},
	}, nil
}

type stubQueryStore struct {
	results []SemanticQueryResult
	err     error
	calls   int
	req     SemanticQueryRequest
	vector  []float32
}

func (s *stubQueryStore) QueryTopK(_ context.Context, req SemanticQueryRequest, queryVector []float32) ([]SemanticQueryResult, error) {
	s.calls++
	s.req = req
	s.vector = append([]float32(nil), queryVector...)
	if s.err != nil {
		return nil, s.err
	}
	return append([]SemanticQueryResult(nil), s.results...), nil
}

func TestServiceQueryPropagatesReadinessErrors(t *testing.T) {
	t.Parallel()

	service := NewOrchestratedService(
		&stubReadinessChecker{err: errors.New("boom")},
		&stubQueryEmbedder{vector: []float32{0.1}},
		&stubQueryStore{},
	)

	_, err := service.Query(context.Background(), SemanticQueryRequest{
		SessionID: "session-1",
		RepoRoot:  "/repo/project",
		Query:     "where is the registry",
		TopK:      1,
	})
	if err == nil || err.Error() != "check index readiness: boom" {
		t.Fatalf("Query error = %v, want readiness error", err)
	}
}
