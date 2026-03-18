package retrieval

import (
	"context"
	"fmt"
)

type SemanticRetriever interface {
	Query(context.Context, SemanticQueryRequest) (SemanticQueryResponse, error)
}

type QueryFunc func(context.Context, SemanticQueryRequest) (SemanticQueryResponse, error)

type Service struct {
	queryFn QueryFunc
}

func NewService(queryFn QueryFunc) *Service {
	return &Service{queryFn: queryFn}
}

func (s *Service) Query(ctx context.Context, req SemanticQueryRequest) (SemanticQueryResponse, error) {
	req = NormalizeSemanticQueryRequest(req)
	if err := ValidateSemanticQueryRequest(req); err != nil {
		return SemanticQueryResponse{}, err
	}
	if s == nil || s.queryFn == nil {
		return SemanticQueryResponse{}, fmt.Errorf("semantic retriever is not configured")
	}

	resp, err := s.queryFn(ctx, req)
	if err != nil {
		return SemanticQueryResponse{}, err
	}

	resp.SessionID = req.SessionID
	resp.RepoRoot = req.RepoRoot
	resp.Query = req.Query
	resp.TopK = req.TopK
	if resp.Index.Status == "" {
		resp.Index.Status = IndexStatusUnknown
	}
	if resp.Results == nil {
		resp.Results = []SemanticQueryResult{}
	}
	for i := range resp.Results {
		resp.Results[i].Snippet = BoundSnippet(resp.Results[i].Snippet)
	}

	return resp, nil
}

var _ SemanticRetriever = (*Service)(nil)
