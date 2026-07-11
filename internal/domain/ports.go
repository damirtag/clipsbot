package domain

import "context"

// ClipRepository persists and queries clips. The Search method backs the
// public inline query; everything else backs the import/admin flow.
type ClipRepository interface {
	Create(ctx context.Context, c *Clip) (int64, error)
	Update(ctx context.Context, c *Clip) error
	UpdateStatus(ctx context.Context, id int64, status ClipStatus, failureReason string) error
	GetByID(ctx context.Context, id int64) (*Clip, error)
	// GetBySourceUniqueFileID is the idempotency check: if a raw Telegram
	// file with this file_unique_id has already been imported (via any
	// clip_sources row), return the existing clip so the caller can skip it.
	GetBySourceUniqueFileID(ctx context.Context, uniqueID string) (*Clip, error)
	Search(ctx context.Context, query string, limit int, offset int) ([]*Clip, error)
	// ListNeedingReprocess returns READY clips whose processing_version is
	// below currentVersion, for the future "reprocess after pipeline
	// upgrade" workflow.
	ListNeedingReprocess(ctx context.Context, currentVersion int, limit int) ([]*Clip, error)
}

type ClipSourceRepository interface {
	Create(ctx context.Context, s *ClipSource) (int64, error)
	GetByUniqueFileID(ctx context.Context, uniqueID string) (*ClipSource, error)
}

type ImportRepository interface {
	CreateImport(ctx context.Context, imp *Import) (int64, error)
	FinishImport(ctx context.Context, id int64, status ImportStatus, imported, skipped, failed int) error
	CreateImportItem(ctx context.Context, item *ImportItem) (int64, error)
	UpdateImportItem(ctx context.Context, item *ImportItem) error
	GetImportItems(ctx context.Context, importID int64) ([]*ImportItem, error)
	// GetIncompleteImports supports resuming after a crash: any import left
	// in RUNNING status when the process died gets picked back up on boot.
	GetIncompleteImports(ctx context.Context) ([]*Import, error)
}

type StagingRepository interface {
	// Upsert keyed on (chat_id, message_id) — forwarding the same message
	// twice, or the bot restarting mid-forward-batch, must not duplicate
	// staged rows.
	Upsert(ctx context.Context, m *StagedMessage) error
	GetByMessageID(ctx context.Context, chatID int64, messageID int) (*StagedMessage, error)
	GetRange(ctx context.Context, chatID int64, minMessageID, maxMessageID int) ([]*StagedMessage, error)
}

// ProcessInput/ProcessResult/VideoProcessor is the FFmpeg abstraction. The
// service layer never shells out to ffmpeg directly — it only depends on
// this interface — so the pipeline (chroma key, blurred background, AI
// reframing, whatever comes later) can be swapped or versioned without
// touching import/upload logic.
type ProcessInput struct {
	InputPath     string
	OutputPath    string
	WatermarkText string
}

type ProcessResult struct {
	OutputPath string
	Duration   int
	Width      int
	Height     int
}

type VideoProcessor interface {
	Process(ctx context.Context, in ProcessInput) (*ProcessResult, error)
	// Version identifies this pipeline implementation/config, stored on the
	// clip as processing_version.
	Version() int
}

// DownloadedFile is what TelegramClient.DownloadFile returns.
type DownloadedFile struct {
	LocalPath string
	Size      int64
	MimeType  string
}

// UploadedVideo is what TelegramClient.UploadVideo returns after pushing a
// processed file into the storage channel.
type UploadedVideo struct {
	FileID       string
	UniqueFileID string
	MessageID    int
	Duration     int
	Width        int
	Height       int
	Size         int64
	MimeType     string
}

// TelegramClient is the thin port around the Bot API that the
// importer/service layer depends on, so those layers stay unit-testable
// without a real bot token or network access.
type TelegramClient interface {
	DownloadFile(ctx context.Context, fileID string) (*DownloadedFile, error)
	UploadVideo(ctx context.Context, chatID int64, localPath, caption string) (*UploadedVideo, error)
}
