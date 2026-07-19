package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mellclipsbot/internal/domain"
)

type ClipRepository struct {
	pool *pgxpool.Pool
}

func NewClipRepository(pool *pgxpool.Pool) *ClipRepository {
	return &ClipRepository{pool: pool}
}

func (r *ClipRepository) Create(ctx context.Context, c *domain.Clip) (int64, error) {
	const q = `
		INSERT INTO clips (
			title, original_caption, clean_title,
			telegram_file_id, telegram_unique_file_id,
			duration, width, height, mime_type, size,
			processing_version, status, storage_chat_id, storage_message_id,
			failure_reason
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, q,
		c.Title, c.OriginalCaption, c.CleanTitle,
		c.TelegramFileID, c.TelegramUniqueFileID,
		c.Duration, c.Width, c.Height, c.MimeType, c.Size,
		c.ProcessingVersion, c.Status, c.StorageChatID, c.StorageMessageID,
		c.FailureReason,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert clip: %w", err)
	}
	return id, nil
}

func (r *ClipRepository) Update(ctx context.Context, c *domain.Clip) error {
	const q = `
		UPDATE clips SET
			title = $1, original_caption = $2, clean_title = $3,
			telegram_file_id = $4, telegram_unique_file_id = $5,
			duration = $6, width = $7, height = $8, mime_type = $9, size = $10,
			processing_version = $11, status = $12,
			storage_chat_id = $13, storage_message_id = $14,
			failure_reason = $15, updated_at = now()
		WHERE id = $16`
	_, err := r.pool.Exec(ctx, q,
		c.Title, c.OriginalCaption, c.CleanTitle,
		c.TelegramFileID, c.TelegramUniqueFileID,
		c.Duration, c.Width, c.Height, c.MimeType, c.Size,
		c.ProcessingVersion, c.Status, c.StorageChatID, c.StorageMessageID,
		c.FailureReason, c.ID,
	)
	if err != nil {
		return fmt.Errorf("update clip %d: %w", c.ID, err)
	}
	return nil
}

func (r *ClipRepository) UpdateStatus(ctx context.Context, id int64, status domain.ClipStatus, failureReason string) error {
	const q = `UPDATE clips SET status = $1, failure_reason = $2, updated_at = now() WHERE id = $3`
	_, err := r.pool.Exec(ctx, q, status, failureReason, id)
	if err != nil {
		return fmt.Errorf("update clip status %d: %w", id, err)
	}
	return nil
}

func (r *ClipRepository) GetByID(ctx context.Context, id int64) (*domain.Clip, error) {
	const q = clipSelectColumns + ` WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	return scanClip(row)
}

// GetBySourceUniqueFileID is the idempotency check used before importing a
// forwarded video: joins through clip_sources on the RAW file's
// unique_id, which is stable regardless of how many times it's re-forwarded
// or reprocessed.
func (r *ClipRepository) GetBySourceUniqueFileID(ctx context.Context, uniqueID string) (*domain.Clip, error) {
	const q = `
		SELECT c.id, c.title, c.original_caption, c.clean_title,
			c.telegram_file_id, c.telegram_unique_file_id,
			c.duration, c.width, c.height, c.mime_type, c.size,
			c.processing_version, c.status, c.storage_chat_id, c.storage_message_id,
			c.failure_reason, c.created_at, c.updated_at
		FROM clips c
		JOIN clip_sources cs ON cs.clip_id = c.id
		WHERE cs.source_file_unique_id = $1
		LIMIT 1`
	row := r.pool.QueryRow(ctx, q, uniqueID)
	clip, err := scanClip(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return clip, err
}

func (r *ClipRepository) Search(ctx context.Context, userID int64, query string, limit int, offset int) ([]*domain.Clip, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}

	if query == "" {
		return r.searchRecentPersonalized(ctx, userID, limit, offset)
	}

	const q = `
		SELECT id, title, original_caption, clean_title,
			telegram_file_id, telegram_unique_file_id,
			duration, width, height, mime_type, size,
			processing_version, status, storage_chat_id, storage_message_id,
			failure_reason, created_at, updated_at
		FROM clips
		WHERE status = 'READY'
		  AND search_vector @@ plainto_tsquery('russian', $1)
		ORDER BY ts_rank(search_vector, plainto_tsquery('russian', $1)) DESC
		LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search clips: %w", err)
	}
	defer rows.Close()

	var out []*domain.Clip
	for rows.Next() {
		c, err := scanClipRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *ClipRepository) RecordSend(ctx context.Context, userID int64, clipID int64) error {
	const q = `INSERT INTO clip_sends (user_Id, clip_id, sent_at, send_count) 
	VALUES ($1, $2, now(), 1) 
	ON CONFLICT (user_id, clip_id) 
	DO UPDATE SET sent_at = now(), send_count = clip_sends.send_count + 1`

	_, err := r.pool.Exec(ctx, q, userID, clipID)
	if err != nil {
		return fmt.Errorf("record clip send: %w", err)
	}

	return nil
}

// searchRecentPersonalized returns the user's own recently-sent clips
// first (most recent send first), then fills any remaining slots with
// the globally most recent READY clips, skipping duplicates.
//
// Pagination note: instead of tracking cursor state, it fetches enough
// rows to satisfy offset+limit from each source and slices the combined
// result. Fine for typical inline-query page sizes (offset grows by
// ~50 per page); avoid for very large offsets.
func (r *ClipRepository) searchRecentPersonalized(ctx context.Context, userID int64, limit int, offset int) ([]*domain.Clip, error) {
	need := offset + limit

	const personalQ = `
		SELECT c.id, c.title, c.original_caption, c.clean_title,
			c.telegram_file_id, c.telegram_unique_file_id,
			c.duration, c.width, c.height, c.mime_type, c.size,
			c.processing_version, c.status, c.storage_chat_id, c.storage_message_id,
			c.failure_reason, c.created_at, c.updated_at
		FROM clips c
		JOIN clip_sends cs ON cs.clip_id = c.id
		WHERE cs.user_id = $1 AND c.status = 'READY'
		ORDER BY cs.sent_at DESC
		LIMIT $2`
	rows, err := r.pool.Query(ctx, personalQ, userID, need)
	if err != nil {
		return nil, fmt.Errorf("search personalized clips: %w", err)
	}
	personal, err := collectClips(rows)
	if err != nil {
		return nil, err
	}

	combined := personal
	if len(combined) < need {
		exclude := make([]int64, len(personal))
		for i, c := range personal {
			exclude[i] = c.ID
		}

		const genericQ = `
			SELECT id, title, original_caption, clean_title,
				telegram_file_id, telegram_unique_file_id,
				duration, width, height, mime_type, size,
				processing_version, status, storage_chat_id, storage_message_id,
				failure_reason, created_at, updated_at
			FROM clips
			WHERE status = 'READY' AND NOT (id = ANY($1))
			ORDER BY created_at DESC
			LIMIT $2`
		rows, err := r.pool.Query(ctx, genericQ, exclude, need-len(combined))
		if err != nil {
			return nil, fmt.Errorf("search generic recent clips: %w", err)
		}
		generic, err := collectClips(rows)
		if err != nil {
			return nil, err
		}
		combined = append(combined, generic...)
	}

	if offset >= len(combined) {
		return nil, nil
	}
	end := offset + limit
	if end > len(combined) {
		end = len(combined)
	}
	return combined[offset:end], nil
}

func collectClips(rows pgx.Rows) ([]*domain.Clip, error) {
	defer rows.Close()
	var out []*domain.Clip
	for rows.Next() {
		c, err := scanClipRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *ClipRepository) ListNeedingReprocess(ctx context.Context, currentVersion int, limit int) ([]*domain.Clip, error) {
	const q = clipSelectColumns + `
		WHERE status = 'READY' AND processing_version < $1
		ORDER BY created_at ASC
		LIMIT $2`
	rows, err := r.pool.Query(ctx, q, currentVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("list clips needing reprocess: %w", err)
	}
	defer rows.Close()

	var out []*domain.Clip
	for rows.Next() {
		c, err := scanClipRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

const clipSelectColumns = `
	SELECT id, title, original_caption, clean_title,
		telegram_file_id, telegram_unique_file_id,
		duration, width, height, mime_type, size,
		processing_version, status, storage_chat_id, storage_message_id,
		failure_reason, created_at, updated_at
	FROM clips`

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanClip(row rowScanner) (*domain.Clip, error) {
	return scanClipRows(row)
}

func scanClipRows(row rowScanner) (*domain.Clip, error) {
	c := &domain.Clip{}
	err := row.Scan(
		&c.ID, &c.Title, &c.OriginalCaption, &c.CleanTitle,
		&c.TelegramFileID, &c.TelegramUniqueFileID,
		&c.Duration, &c.Width, &c.Height, &c.MimeType, &c.Size,
		&c.ProcessingVersion, &c.Status, &c.StorageChatID, &c.StorageMessageID,
		&c.FailureReason, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan clip: %w", err)
	}
	return c, nil
}
