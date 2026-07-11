package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"mellclipsbot/internal/bot"
	"mellclipsbot/internal/config"
	"mellclipsbot/internal/ffmpeg"
	"mellclipsbot/internal/logger"
	"mellclipsbot/internal/repository/postgres"
	"mellclipsbot/internal/search"
	"mellclipsbot/internal/services"
	"mellclipsbot/internal/telegram"
)

func main() {
	log := logger.New()

	cfg, err := config.Load()
	if err != nil {
		log.Error("load config failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("connect database failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		log.Error("init telegram bot failed", "error", err)
		os.Exit(1)
	}
	log.Info("authorized on telegram", "username", api.Self.UserName)

	// --- repositories -------------------------------------------------------
	clipRepo := postgres.NewClipRepository(pool)
	clipSourceRepo := postgres.NewClipSourceRepository(pool)
	importRepo := postgres.NewImportRepository(pool)
	stagingRepo := postgres.NewStagingRepository(pool)

	// --- infrastructure adapters ---------------------------------------------
	tgClient := telegram.NewClient(api, cfg.TempDir)
	processor := ffmpeg.NewProcessor(cfg.FFmpegPath, cfg.ProcessingVersion, cfg.TempDir)

	// --- services -------------------------------------------------------------
	clipService := services.NewClipService(
		clipRepo, clipSourceRepo, tgClient, processor,
		cfg.WatermarkText, cfg.TempDir, cfg.StorageChatID, cfg.ProcessingVersion,
	)
	importService := services.NewImportService(stagingRepo, importRepo, clipService, log)
	searchService := search.NewService(clipRepo)

	// Resume any import left RUNNING from a previous crash before serving
	// new traffic.
	importService.ResumeIncomplete(ctx)

	b := bot.New(api, cfg.AdminUserID, stagingRepo, importService, searchService, log)

	log.Info("starting bot")
	if err := b.Run(ctx); err != nil && ctx.Err() == nil {
		log.Error("bot run failed", "error", err)
		os.Exit(1)
	}

	log.Info("shutting down")
}
