package retrieval

import (
	"context"
	"fmt"
)

type SemanticRetriever interface {
	Query(context.Context, SemanticQueryRequest) (SemanticQueryResponse, error)
}

type QueryFunc func(context.Context, SemanticQueryRequest) (SemanticQueryResponse, error)

type indexReadinessChecker interface {
	GetReadiness(context.Context, string, string) (SemanticIndexReadiness, error)
}

type queryStore interface {
	QueryTopK(context.Context, SemanticQueryRequest, []float32) ([]SemanticQueryResult, error)
}

type Service struct {
	queryFn   QueryFunc
	readiness indexReadinessChecker
	embedder  queryTextEmbedder
	store     queryStore
}

func NewService(queryFn QueryFunc) *Service {
	return &Service{queryFn: queryFn}
}

func NewOrchestratedService(readiness indexReadinessChecker, embedder queryTextEmbedder, store queryStore) *Service {
	return &Service{
		readiness: readiness,
		embedder:  embedder,
		store:     store,
	}
}

func (s *Service) Query(ctx context.Context, req SemanticQueryRequest) (SemanticQueryResponse, error) {
	req = NormalizeSemanticQueryRequest(req)
	if err := ValidateSemanticQueryRequest(req); err != nil {
		return SemanticQueryResponse{}, err
	}

	resp := SemanticQueryResponse{
		SessionID: req.SessionID,
		RepoRoot:  req.RepoRoot,
		Query:     req.Query,
		TopK:      req.TopK,
		Results:   []SemanticQueryResult{},
	}

	switch {
	case s == nil:
		return SemanticQueryResponse{}, fmt.Errorf("semantic retriever is not configured")
	case s.queryFn != nil:
		queryResp, err := s.queryFn(ctx, req)
		if err != nil {
			return SemanticQueryResponse{}, err
		}
		resp.Index = queryResp.Index
		resp.Results = queryResp.Results
	case s.readiness == nil || s.embedder == nil || s.store == nil:
		return SemanticQueryResponse{}, fmt.Errorf("semantic retriever is not configured")
	default:
		readiness, err := s.readiness.GetReadiness(ctx, req.SessionID, req.RepoRoot)
		if err != nil {
			return SemanticQueryResponse{}, fmt.Errorf("check index readiness: %w", err)
		}
		if readiness.Status == "" {
			if readiness.Ready {
				readiness.Status = IndexStatusUnknown
			} else {
				readiness.Status = "index_not_ready"
			}
		}
		resp.Index = readiness
		if !readiness.Ready {
			return finalizeSemanticQueryResponse(resp), nil
		}

		queryVector, err := EmbedQuery(ctx, s.embedder, req.Query)
		if err != nil {
			return SemanticQueryResponse{}, err
		}

		results, err := s.store.QueryTopK(ctx, req, queryVector)
		if err != nil {
			return SemanticQueryResponse{}, err
		}
		resp.Results = ApplyStableRanks(results)
	}

	return finalizeSemanticQueryResponse(resp), nil
}

var _ SemanticRetriever = (*Service)(nil)

func finalizeSemanticQueryResponse(resp SemanticQueryResponse) SemanticQueryResponse {
	if resp.Index.Status == "" {
		resp.Index.Status = IndexStatusUnknown
	}
	if resp.Results == nil {
		resp.Results = []SemanticQueryResult{}
	}
	for i := range resp.Results {
		resp.Results[i].Snippet = BoundSnippet(resp.Results[i].Snippet)
	}
	return resp
}
