package domain

import "time"

// ClipStatus tracks a clip through the processing pipeline. Persisting this
// (rather than keeping it in memory) is what lets an import resume safely
// after a crash: on restart, anything not in StatusReady/StatusFailed gets
// picked back up from wherever it stopped.
type ClipStatus string

const (
	StatusNew         ClipStatus = "NEW"
	StatusDownloading ClipStatus = "DOWNLOADING"
	StatusProcessing  ClipStatus = "PROCESSING"
	StatusUploading   ClipStatus = "UPLOADING"
	StatusReady       ClipStatus = "READY"
	StatusFailed      ClipStatus = "FAILED"
)

// SourceProvider identifies where a clip originally came from. Only
// "telegram" is implemented today; the others exist so the schema and
// interfaces don't need to change when those importers are added.
type SourceProvider string

const (
	ProviderTelegram  SourceProvider = "telegram"
	ProviderYouTube   SourceProvider = "youtube"
	ProviderTikTok    SourceProvider = "tiktok"
	ProviderInstagram SourceProvider = "instagram"
)

// Clip is the canonical processed video, independent of where it came from
// or how it's delivered.
type Clip struct {
	ID                    int64
	Title                 string
	OriginalCaption       string
	CleanTitle            string
	TelegramFileID        string // file_id of the PROCESSED video, in storage chat
	TelegramUniqueFileID  string // file_unique_id of the PROCESSED video (stable across bots/time)
	Duration              int
	Width                 int
	Height                int
	MimeType              string
	Size                  int64
	ProcessingVersion     int
	Status                ClipStatus
	StorageChatID         int64
	StorageMessageID      int
	FailureReason         string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// ClipSource records provenance. A clip could in principle be re-derived
// from multiple sources (e.g. reprocessed from a higher quality upload
// later) so this is 1:N rather than folded into Clip.
type ClipSource struct {
	ID                  int64
	ClipID              int64
	Provider            SourceProvider
	SourceChatID        int64
	SourceMessageID     int
	SourceFileID        string // file_id of the RAW video as forwarded (pre-processing)
	SourceFileUniqueID  string // file_unique_id of the RAW video — this is the idempotency key
	SourceURL           *string
	CreatedAt           time.Time
}

type ImportStatus string

const (
	ImportRunning   ImportStatus = "RUNNING"
	ImportCompleted ImportStatus = "COMPLETED"
	ImportFailed    ImportStatus = "FAILED"
)

// Import is one /import invocation (one batch of forwarded messages).
type Import struct {
	ID             int64
	StartedAt      time.Time
	FinishedAt     *time.Time
	Status         ImportStatus
	ImportedCount  int
	SkippedCount   int
	FailedCount    int
	InitiatedBy    int64
	SourceChatID   int64
	StartMessageID int
	EndMessageID   int
}

type ImportItemStatus string

const (
	ItemPending          ImportItemStatus = "PENDING"
	ItemProcessing       ImportItemStatus = "PROCESSING"
	ItemReady            ImportItemStatus = "READY"
	ItemSkippedDuplicate ImportItemStatus = "SKIPPED_DUPLICATE"
	ItemSkippedNoVideo   ImportItemStatus = "SKIPPED_NO_VIDEO"
	ItemFailed           ImportItemStatus = "FAILED"
)

// ImportItem is the per-message audit trail within an Import, so a partial
// failure tells you exactly which of the N forwarded messages succeeded,
// were duplicates, or failed and why.
type ImportItem struct {
	ID              int64
	ImportID        int64
	SourceMessageID int
	ClipID          *int64
	Status          ImportItemStatus
	Error           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// StagedMessage is the local buffer described above: every message the bot
// receives in the admin's private chat gets upserted here as it arrives.
// This is what makes the ID-range resolution in /import possible at all
// without a Telethon-style getMessages call.
type StagedMessage struct {
	ID           int64
	ChatID       int64
	MessageID    int
	FromUserID   int64
	HasVideo     bool
	FileID       string
	FileUniqueID string
	Caption      string
	Duration     int
	Width        int
	Height       int
	MimeType     string
	Size         int64
	ReceivedAt   time.Time
}
