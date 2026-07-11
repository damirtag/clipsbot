package services

import (
	"context"
	"log/slog"
	"time"

	"mellclipsbot/internal/domain"
)

const maxRetries = 3

// ImportService resolves an /import command into a message-ID range against
// the local staging buffer, then drives ClipService over each staged video,
// recording per-item results for auditability and crash-resumption.
type ImportService struct {
	staging domain.StagingRepository
	imports domain.ImportRepository
	clips   *ClipService
	log     *slog.Logger
}

func NewImportService(staging domain.StagingRepository, imports domain.ImportRepository, clips *ClipService, log *slog.Logger) *ImportService {
	return &ImportService{staging: staging, imports: imports, clips: clips, log: log}
}

// RunImport implements the workflow described in the spec:
//
//	forward message 100..103
//	reply to message 100 with /import (command itself is message 104, say)
//
// -> iterate every staged message in [100, 104] inclusive, process every one
// that has a video attached, skip everything else.
func (s *ImportService) RunImport(ctx context.Context, chatID int64, startMessageID, endMessageID int, initiatedBy int64) (*domain.Import, error) {
	imp := &domain.Import{
		Status:         domain.ImportRunning,
		InitiatedBy:    initiatedBy,
		SourceChatID:   chatID,
		StartMessageID: startMessageID,
		EndMessageID:   endMessageID,
	}
	importID, err := s.imports.CreateImport(ctx, imp)
	if err != nil {
		return nil, err
	}
	imp.ID = importID

	messages, err := s.staging.GetRange(ctx, chatID, startMessageID, endMessageID)
	if err != nil {
		_ = s.imports.FinishImport(ctx, importID, domain.ImportFailed, 0, 0, 0)
		return imp, err
	}

	s.log.Info(
		"loaded staged messages",
		"count", len(messages),
		"chat_id", chatID,
		"start", startMessageID,
		"end", endMessageID,
	)

	var importedCount, skippedCount, failedCount int

	for _, sm := range messages {
		s.log.Info(
			"staged message",
			"id", sm.MessageID,
			"has_video", sm.HasVideo,
			"file_id", sm.FileID,
		)
		item := &domain.ImportItem{
			ImportID:        importID,
			SourceMessageID: sm.MessageID,
			Status:          domain.ItemPending,
		}
		itemID, err := s.imports.CreateImportItem(ctx, item)
		if err != nil {
			s.log.Error("create import item failed", "error", err, "message_id", sm.MessageID)
			continue
		}
		item.ID = itemID

		if !sm.HasVideo {
			item.Status = domain.ItemSkippedNoVideo
			_ = s.imports.UpdateImportItem(ctx, item)
			continue
		}

		item.Status = domain.ItemProcessing
		_ = s.imports.UpdateImportItem(ctx, item)

		var (
			outcome *ProcessOutcome
			procErr error
		)

		for attempt := 1; attempt <= maxRetries; attempt++ {
			outcome, procErr = s.clips.ProcessStagedVideo(ctx, sm)
			if procErr == nil {
				break
			}

			s.log.Warn(
				"process staged video attempt failed",
				"attempt", attempt,
				"max_retries", maxRetries,
				"message_id", sm.MessageID,
				"error", procErr,
			)

			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					procErr = ctx.Err()
					break
				case <-time.After(time.Duration(attempt) * 2 * time.Second):
				}
			}
		}

		if procErr != nil {
			item.Status = domain.ItemFailed
			item.Error = procErr.Error()
			_ = s.imports.UpdateImportItem(ctx, item)
			failedCount++

			s.log.Error(
				"process staged video failed after retries",
				"message_id", sm.MessageID,
				"error", procErr,
			)

			continue
		}

		clipID := outcome.ClipID
		item.ClipID = &clipID
		if outcome.Skipped {
			item.Status = domain.ItemSkippedDuplicate
			skippedCount++
		} else {
			item.Status = domain.ItemReady
			importedCount++
		}
		_ = s.imports.UpdateImportItem(ctx, item)
	}

	finalStatus := domain.ImportCompleted
	if err := s.imports.FinishImport(ctx, importID, finalStatus, importedCount, skippedCount, failedCount); err != nil {
		return imp, err
	}

	imp.Status = finalStatus
	imp.ImportedCount = importedCount
	imp.SkippedCount = skippedCount
	imp.FailedCount = failedCount
	return imp, nil
}

// ResumeIncomplete is called on boot to pick up any import left RUNNING
// when the process previously died. Since GetIncompleteImports only
// returns imports (not their unfinished items), and RunImport itself is
// idempotent per-item (ON CONFLICT upsert on import_items, plus the
// clip_sources idempotency check inside ClipService), the simplest safe
// resume strategy is to re-run RunImport with the same range: already-READY
// items are still skipped via the source idempotency check, so this never
// double-processes.
func (s *ImportService) ResumeIncomplete(ctx context.Context) {
	incomplete, err := s.imports.GetIncompleteImports(ctx)
	if err != nil {
		s.log.Error("list incomplete imports failed", "error", err)
		return
	}
	for _, imp := range incomplete {
		s.log.Info("resuming incomplete import", "import_id", imp.ID)
		if _, err := s.RunImport(ctx, imp.SourceChatID, imp.StartMessageID, imp.EndMessageID, imp.InitiatedBy); err != nil {
			s.log.Error("resume import failed", "error", err, "import_id", imp.ID)
		}
	}
}
