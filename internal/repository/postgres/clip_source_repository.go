package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mellclipsbot/internal/domain"
)

type ClipSourceRepository struct {
	pool *pgxpool.Pool
}

func NewClipSourceRepository(pool *pgxpool.Pool) *ClipSourceRepository {
	return &ClipSourceRepository{pool: pool}
}

func (r *ClipSourceRepository) Create(ctx context.Context, s *domain.ClipSource) (int64, error) {
	const q = `
		INSERT INTO clip_sources (
			clip_id, provider, source_chat_id, source_message_id,
			source_file_id, source_file_unique_id, source_url
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, q,
		s.ClipID, s.Provider, s.SourceChatID, s.SourceMessageID,
		s.SourceFileID, s.SourceFileUniqueID, s.SourceURL,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert clip source: %w", err)
	}
	return id, nil
}

func (r *ClipSourceRepository) GetByUniqueFileID(ctx context.Context, uniqueID string) (*domain.ClipSource, error) {
	const q = `
		SELECT id, clip_id, provider, source_chat_id, source_message_id,
			source_file_id, source_file_unique_id, source_url, created_at
		FROM clip_sources WHERE source_file_unique_id = $1`
	s := &domain.ClipSource{}
	err := r.pool.QueryRow(ctx, q, uniqueID).Scan(
		&s.ID, &s.ClipID, &s.Provider, &s.SourceChatID, &s.SourceMessageID,
		&s.SourceFileID, &s.SourceFileUniqueID, &s.SourceURL, &s.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get clip source: %w", err)
	}
	return s, nil
}
