package telegram

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"mellclipsbot/internal/domain"
)

// Client wraps the tgbotapi.BotAPI to satisfy domain.TelegramClient, so the
// importer/service layer never imports tgbotapi directly and stays
// testable behind the interface.
type Client struct {
	api     *tgbotapi.BotAPI
	tempDir string
}

func NewClient(api *tgbotapi.BotAPI, tempDir string) *Client {
	return &Client{api: api, tempDir: tempDir}
}

func (c *Client) DownloadFile(ctx context.Context, fileID string) (*domain.DownloadedFile, error) {
	tgFile, err := c.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("get file info: %w", err)
	}

	url := tgFile.Link(c.api.Token)

	if err := os.MkdirAll(c.tempDir, 0o755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	localPath := filepath.Join(c.tempDir, fmt.Sprintf("%s%s", fileID, filepath.Ext(tgFile.FilePath)))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build download request: %w", err)
	}

	httpClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download file: unexpected status %d", resp.StatusCode)
	}

	out, err := os.Create(localPath)
	if err != nil {
		return nil, fmt.Errorf("create local file: %w", err)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("write local file: %w", err)
	}

	return &domain.DownloadedFile{
		LocalPath: localPath,
		Size:      written,
		MimeType:  "video/mp4",
	}, nil
}

// UploadVideo pushes the processed file into the storage chat via sendVideo
// (NOT sendVideoNote — see the architecture note on inline delivery: only a
// regular video's file_id can be used in InlineQueryResultCachedVideo).
func (c *Client) UploadVideo(ctx context.Context, chatID int64, localPath, caption string) (*domain.UploadedVideo, error) {
	video := tgbotapi.NewVideoNote(chatID, 512, tgbotapi.FilePath(localPath))

	msg, err := c.api.Send(video)
	if err != nil {
		return nil, fmt.Errorf("send video to storage chat: %w", err)
	}
	if msg.VideoNote == nil {
		return nil, fmt.Errorf("sendVideo response missing video payload")
	}

	return &domain.UploadedVideo{
		FileID:       msg.VideoNote.FileID,
		UniqueFileID: msg.VideoNote.FileUniqueID,
		MessageID:    msg.MessageID,
		Duration:     msg.VideoNote.Duration,
		Size:         int64(msg.VideoNote.FileSize),
	}, nil
}

// ChatIDFromString is a small helper for parsing storage/admin IDs that
// might be supplied as either numeric chat IDs or (less commonly) as
// @username strings; kept here since it's telegram-specific parsing.
func ChatIDFromString(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
