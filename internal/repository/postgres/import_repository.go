package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"mellclipsbot/internal/domain"
)

type ImportRepository struct {
	pool *pgxpool.Pool
}

func NewImportRepository(pool *pgxpool.Pool) *ImportRepository {
	return &ImportRepository{pool: pool}
}

func (r *ImportRepository) CreateImport(ctx context.Context, imp *domain.Import) (int64, error) {
	const q = `
		INSERT INTO imports (
			status, initiated_by, source_chat_id, start_message_id, end_message_id
		) VALUES ($1,$2,$3,$4,$5)
		RETURNING id, started_at`
	var id int64
	err := r.pool.QueryRow(ctx, q,
		imp.Status, imp.InitiatedBy, imp.SourceChatID, imp.StartMessageID, imp.EndMessageID,
	).Scan(&id, &imp.StartedAt)
	if err != nil {
		return 0, fmt.Errorf("insert import: %w", err)
	}
	return id, nil
}

func (r *ImportRepository) FinishImport(ctx context.Context, id int64, status domain.ImportStatus, imported, skipped, failed int) error {
	const q = `
		UPDATE imports SET
			status = $1, imported_count = $2, skipped_count = $3, failed_count = $4,
			finished_at = now()
		WHERE id = $5`
	_, err := r.pool.Exec(ctx, q, status, imported, skipped, failed, id)
	if err != nil {
		return fmt.Errorf("finish import %d: %w", id, err)
	}
	return nil
}

func (r *ImportRepository) CreateImportItem(ctx context.Context, item *domain.ImportItem) (int64, error) {
	const q = `
		INSERT INTO import_items (import_id, source_message_id, clip_id, status, error)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (import_id, source_message_id) DO UPDATE SET
			clip_id = EXCLUDED.clip_id, status = EXCLUDED.status,
			error = EXCLUDED.error, updated_at = now()
		RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, q,
		item.ImportID, item.SourceMessageID, item.ClipID, item.Status, item.Error,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert import item: %w", err)
	}
	return id, nil
}

func (r *ImportRepository) UpdateImportItem(ctx context.Context, item *domain.ImportItem) error {
	const q = `
		UPDATE import_items SET clip_id = $1, status = $2, error = $3, updated_at = now()
		WHERE id = $4`
	_, err := r.pool.Exec(ctx, q, item.ClipID, item.Status, item.Error, item.ID)
	if err != nil {
		return fmt.Errorf("update import item %d: %w", item.ID, err)
	}
	return nil
}

func (r *ImportRepository) GetImportItems(ctx context.Context, importID int64) ([]*domain.ImportItem, error) {
	const q = `
		SELECT id, import_id, source_message_id, clip_id, status, error, created_at, updated_at
		FROM import_items WHERE import_id = $1 ORDER BY source_message_id ASC`
	rows, err := r.pool.Query(ctx, q, importID)
	if err != nil {
		return nil, fmt.Errorf("list import items: %w", err)
	}
	defer rows.Close()

	var out []*domain.ImportItem
	for rows.Next() {
		it := &domain.ImportItem{}
		if err := rows.Scan(&it.ID, &it.ImportID, &it.SourceMessageID, &it.ClipID,
			&it.Status, &it.Error, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan import item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *ImportRepository) GetIncompleteImports(ctx context.Context) ([]*domain.Import, error) {
	const q = `
		SELECT id, started_at, finished_at, status, imported_count, skipped_count,
			failed_count, initiated_by, source_chat_id, start_message_id, end_message_id
		FROM imports WHERE status = 'RUNNING' ORDER BY started_at ASC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list incomplete imports: %w", err)
	}
	defer rows.Close()

	var out []*domain.Import
	for rows.Next() {
		imp := &domain.Import{}
		if err := rows.Scan(&imp.ID, &imp.StartedAt, &imp.FinishedAt, &imp.Status,
			&imp.ImportedCount, &imp.SkippedCount, &imp.FailedCount,
			&imp.InitiatedBy, &imp.SourceChatID, &imp.StartMessageID, &imp.EndMessageID); err != nil {
			return nil, fmt.Errorf("scan import: %w", err)
		}
		out = append(out, imp)
	}
	return out, rows.Err()
}
