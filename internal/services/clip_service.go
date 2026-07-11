package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"mellclipsbot/internal/domain"
	"mellclipsbot/internal/importer"
)

// ClipService owns the per-clip pipeline: idempotency check, download,
// ffmpeg processing, upload to storage, and persistence. It depends only on
// domain interfaces (repositories, VideoProcessor, TelegramClient) so it's
// unit-testable with fakes and has no direct Telegram/ffmpeg coupling.
type ClipService struct {
	clips             domain.ClipRepository
	clipSources       domain.ClipSourceRepository
	telegram          domain.TelegramClient
	processor         domain.VideoProcessor
	watermark         string
	tempDir           string
	storageChatID     int64
	processingVersion int
}

func NewClipService(
	clips domain.ClipRepository,
	clipSources domain.ClipSourceRepository,
	telegram domain.TelegramClient,
	processor domain.VideoProcessor,
	watermark, tempDir string,
	storageChatID int64,
	processingVersion int,
) *ClipService {
	return &ClipService{
		clips:             clips,
		clipSources:       clipSources,
		telegram:          telegram,
		processor:         processor,
		watermark:         watermark,
		tempDir:           tempDir,
		storageChatID:     storageChatID,
		processingVersion: processingVersion,
	}
}

// ProcessOutcome indicates what happened for a single staged message.
type ProcessOutcome struct {
	ClipID  int64
	Skipped bool // true if this was a duplicate, already-imported source
}

// ProcessStagedVideo runs one video through the full pipeline. It is the
// unit of work retried/resumed at the per-item level by the import service.
func (s *ClipService) ProcessStagedVideo(ctx context.Context, sm *domain.StagedMessage) (*ProcessOutcome, error) {
	// --- Idempotency check -------------------------------------------------
	// Keyed on the RAW source file's file_unique_id, which stays stable
	// even if the same video is forwarded again, forwarded from a
	// different chat, or this bot restarts. This is what guarantees
	// forwarding the same clip twice never creates a duplicate.
	existingSource, err := s.clipSources.GetByUniqueFileID(ctx, sm.FileUniqueID)
	if err != nil {
		return nil, fmt.Errorf("check existing source: %w", err)
	}
	if existingSource != nil {
		return &ProcessOutcome{ClipID: existingSource.ClipID, Skipped: true}, nil
	}

	cleanTitle := importer.ExtractCleanTitle(sm.Caption)

	// --- Create the clip row up front in NEW status -------------------------
	// so that if the process crashes mid-pipeline, the clip is visible in
	// the DB with a status showing exactly where it got stuck.
	clip := &domain.Clip{
		Title:             cleanTitle,
		OriginalCaption:   sm.Caption,
		CleanTitle:        cleanTitle,
		ProcessingVersion: s.processingVersion,
		Status:            domain.StatusNew,
		StorageChatID:     s.storageChatID,
	}
	clipID, err := s.clips.Create(ctx, clip)
	if err != nil {
		return nil, fmt.Errorf("create clip row: %w", err)
	}
	clip.ID = clipID

	// Record provenance immediately (before processing) so the idempotency
	// check above catches concurrent/duplicate imports of this same source
	// file even while this one is still processing.
	if _, err := s.clipSources.Create(ctx, &domain.ClipSource{
		ClipID:             clipID,
		Provider:           domain.ProviderTelegram,
		SourceChatID:       sm.ChatID,
		SourceMessageID:    sm.MessageID,
		SourceFileID:       sm.FileID,
		SourceFileUniqueID: sm.FileUniqueID,
	}); err != nil {
		_ = s.clips.UpdateStatus(ctx, clipID, domain.StatusFailed, err.Error())
		return nil, fmt.Errorf("create clip source row: %w", err)
	}

	outputPath, err := s.runPipeline(ctx, clip, sm.FileID)
	if outputPath != "" {
		defer os.Remove(outputPath)
	}
	if err != nil {
		_ = s.clips.UpdateStatus(ctx, clipID, domain.StatusFailed, err.Error())
		return nil, err
	}

	return &ProcessOutcome{ClipID: clipID, Skipped: false}, nil
}

// runPipeline executes download -> process -> upload -> persist, updating
// clip.Status at each stage. Returns the local processed file path (for
// cleanup by the caller) and any error.
func (s *ClipService) runPipeline(ctx context.Context, clip *domain.Clip, rawFileID string) (string, error) {
	if err := s.clips.UpdateStatus(ctx, clip.ID, domain.StatusDownloading, ""); err != nil {
		return "", fmt.Errorf("mark downloading: %w", err)
	}
	downloaded, err := s.telegram.DownloadFile(ctx, rawFileID)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer os.Remove(downloaded.LocalPath)

	if err := s.clips.UpdateStatus(ctx, clip.ID, domain.StatusProcessing, ""); err != nil {
		return "", fmt.Errorf("mark processing: %w", err)
	}
	outputPath := filepath.Join(s.tempDir, fmt.Sprintf("processed_%d.mp4", clip.ID))
	result, err := s.processor.Process(ctx, domain.ProcessInput{
		InputPath:     downloaded.LocalPath,
		OutputPath:    outputPath,
		WatermarkText: s.watermark,
	})
	if err != nil {
		return "", fmt.Errorf("ffmpeg process: %w", err)
	}

	if err := s.clips.UpdateStatus(ctx, clip.ID, domain.StatusUploading, ""); err != nil {
		return outputPath, fmt.Errorf("mark uploading: %w", err)
	}
	uploaded, err := s.telegram.UploadVideo(ctx, s.storageChatID, outputPath, clip.CleanTitle)
	if err != nil {
		return outputPath, fmt.Errorf("upload: %w", err)
	}

	clip.TelegramFileID = uploaded.FileID
	clip.TelegramUniqueFileID = uploaded.UniqueFileID
	clip.StorageMessageID = uploaded.MessageID
	clip.Duration = result.Duration
	clip.Width = result.Width
	clip.Height = result.Height
	clip.MimeType = uploaded.MimeType
	clip.Size = uploaded.Size
	clip.Status = domain.StatusReady

	if err := s.clips.Update(ctx, clip); err != nil {
		return outputPath, fmt.Errorf("persist ready clip: %w", err)
	}

	return outputPath, nil
}
