package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mellclipsbot/internal/domain"
)

type StagingRepository struct {
	pool *pgxpool.Pool
}

func NewStagingRepository(pool *pgxpool.Pool) *StagingRepository {
	return &StagingRepository{pool: pool}
}

// Upsert is called for every message the bot receives in the admin chat.
// ON CONFLICT keeps this idempotent if Telegram redelivers an update or the
// bot restarts mid-stream.
func (r *StagingRepository) Upsert(ctx context.Context, m *domain.StagedMessage) error {
	const q = `
		INSERT INTO staged_messages (
			chat_id, message_id, from_user_id, has_video, file_id, file_unique_id,
			caption, duration, width, height, mime_type, size
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (chat_id, message_id) DO UPDATE SET
			has_video = EXCLUDED.has_video, file_id = EXCLUDED.file_id,
			file_unique_id = EXCLUDED.file_unique_id, caption = EXCLUDED.caption,
			duration = EXCLUDED.duration, width = EXCLUDED.width, height = EXCLUDED.height,
			mime_type = EXCLUDED.mime_type, size = EXCLUDED.size`
	_, err := r.pool.Exec(ctx, q,
		m.ChatID, m.MessageID, m.FromUserID, m.HasVideo, m.FileID, m.FileUniqueID,
		m.Caption, m.Duration, m.Width, m.Height, m.MimeType, m.Size,
	)
	if err != nil {
		return fmt.Errorf("upsert staged message: %w", err)
	}
	return nil
}

func (r *StagingRepository) GetByMessageID(ctx context.Context, chatID int64, messageID int) (*domain.StagedMessage, error) {
	const q = `
		SELECT id, chat_id, message_id, from_user_id, has_video, file_id, file_unique_id,
			caption, duration, width, height, mime_type, size, received_at
		FROM staged_messages WHERE chat_id = $1 AND message_id = $2`
	row := r.pool.QueryRow(ctx, q, chatID, messageID)
	m, err := scanStagedMessage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

// GetRange is the local substitute for Telethon's get_messages(min_id,
// max_id): it reads from the buffer this bot already populated via Upsert
// as messages arrived, ordered so processing happens in forward order.
func (r *StagingRepository) GetRange(ctx context.Context, chatID int64, minMessageID, maxMessageID int) ([]*domain.StagedMessage, error) {
	const q = `
		SELECT id, chat_id, message_id, from_user_id, has_video, file_id, file_unique_id,
			caption, duration, width, height, mime_type, size, received_at
		FROM staged_messages
		WHERE chat_id = $1 AND message_id BETWEEN $2 AND $3
		ORDER BY message_id ASC`
	rows, err := r.pool.Query(ctx, q, chatID, minMessageID, maxMessageID)
	if err != nil {
		return nil, fmt.Errorf("get staged message range: %w", err)
	}
	defer rows.Close()

	var out []*domain.StagedMessage
	for rows.Next() {
		m, err := scanStagedMessageRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanStagedMessage(row rowScanner) (*domain.StagedMessage, error) {
	return scanStagedMessageRows(row)
}

func scanStagedMessageRows(row rowScanner) (*domain.StagedMessage, error) {
	m := &domain.StagedMessage{}
	err := row.Scan(
		&m.ID, &m.ChatID, &m.MessageID, &m.FromUserID, &m.HasVideo, &m.FileID, &m.FileUniqueID,
		&m.Caption, &m.Duration, &m.Width, &m.Height, &m.MimeType, &m.Size, &m.ReceivedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan staged message: %w", err)
	}
	return m, nil
}
