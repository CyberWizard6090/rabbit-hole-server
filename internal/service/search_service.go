package service

import (
	"context"

	"rabbit-hole-server/internal/domain"
)

type SearchService interface {
	Search(ctx context.Context, params domain.SearchParams) ([]domain.SearchResult, int64, error)
}

type searchService struct {
	repo domain.SearchRepository
}

func NewSearchService(repo domain.SearchRepository) SearchService {
	return &searchService{repo: repo}
}

func (s *searchService) Search(ctx context.Context, params domain.SearchParams) ([]domain.SearchResult, int64, error) {
	hits, total, err := s.repo.Search(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	results := make([]domain.SearchResult, len(hits))
	for i, hit := range hits {
		results[i] = buildSearchResult(hit, params.Query)
	}
	return results, total, nil
}
