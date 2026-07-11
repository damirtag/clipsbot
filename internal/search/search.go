package search

import (
	"context"
	"strings"

	"mellclipsbot/internal/domain"
)

// Service exists as its own package (rather than calling
// domain.ClipRepository.Search directly from the bot layer) so that
// query normalization, ranking tweaks, or a future swap to a dedicated
// search engine (Meilisearch/Typesense) can happen here without touching
// the bot package or the repository's CRUD responsibilities.
type Service struct {
	clips domain.ClipRepository
}

func NewService(clips domain.ClipRepository) *Service {
	return &Service{clips: clips}
}

const maxInlineResults = 20

func (s *Service) Search(ctx context.Context, rawQuery string, limit, offset int) ([]*domain.Clip, error) {
	query := strings.TrimSpace(rawQuery)
	return s.clips.Search(ctx, query, limit, offset)
}
