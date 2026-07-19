package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config is loaded entirely from environment variables, per the "no
// unnecessary dependencies" constraint — no viper/env-parsing lib needed
// for a set this small.
type Config struct {
	Env string

	// DevTelegramBotToken is used in development mode to avoid using the
	// production bot token. It is ignored in production mode.
	DevTelegramBotToken string

	TelegramBotToken string
	// AdminUserID is the only Telegram user ID allowed to use admin
	// commands (/import) or have their forwarded messages staged at all.
	AdminUserID int64
	// StorageChatID is the private channel/chat the bot uploads processed
	// clips into, purely to obtain durable file_id/file_unique_id values.
	StorageChatID int64

	DatabaseURL string

	// ProcessingVersion is bumped whenever the ffmpeg pipeline changes in a
	// way that should trigger reprocessing of existing clips.
	ProcessingVersion int
	WatermarkText     string

	// FFmpegPath allows pointing at a specific binary; defaults to "ffmpeg"
	// (resolved via PATH).
	FFmpegPath string

	// TempDir is scratch space for downloads/ffmpeg output.
	TempDir string
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:                 getEnvDefault("ENV", "production"),
		DevTelegramBotToken: os.Getenv("DEV_TELEGRAM_BOT_TOKEN"),
		TelegramBotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		WatermarkText:       getEnvDefault("WATERMARK_TEXT", "@mellclipsbot"),
		FFmpegPath:          getEnvDefault("FFMPEG_PATH", "ffmpeg"),
		TempDir:             getEnvDefault("TEMP_DIR", "/tmp/mellclipsbot"),
	}

	// In development mode, use the development bot token if available
	if cfg.Env == "development" && cfg.DevTelegramBotToken != "" {
		cfg.TelegramBotToken = cfg.DevTelegramBotToken
	}

	if cfg.TelegramBotToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	adminID, err := parseInt64Env("ADMIN_USER_ID")
	if err != nil {
		return nil, err
	}
	cfg.AdminUserID = adminID

	storageID, err := parseInt64Env("STORAGE_CHAT_ID")
	if err != nil {
		return nil, err
	}
	cfg.StorageChatID = storageID

	version := getEnvDefault("PROCESSING_VERSION", "1")
	v, err := strconv.Atoi(version)
	if err != nil {
		return nil, fmt.Errorf("PROCESSING_VERSION must be an integer: %w", err)
	}
	cfg.ProcessingVersion = v

	return cfg, nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseInt64Env(key string) (int64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return v, nil
}
