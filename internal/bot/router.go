package bot

import (
	"context"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"mellclipsbot/internal/domain"
	"mellclipsbot/internal/importer"
	"mellclipsbot/internal/search"
	"mellclipsbot/internal/services"
)

// Bot wires the Telegram update loop to the admin and public handlers. It's
// the only package that imports tgbotapi directly outside of internal/telegram.
type Bot struct {
	api         *tgbotapi.BotAPI
	adminUserID int64
	staging     domain.StagingRepository
	imports     *services.ImportService
	search      *search.Service
	log         *slog.Logger
}

func New(
	api *tgbotapi.BotAPI,
	adminUserID int64,
	staging domain.StagingRepository,
	imports *services.ImportService,
	searchSvc *search.Service,
	log *slog.Logger,
) *Bot {
	return &Bot{
		api:         api,
		adminUserID: adminUserID,
		staging:     staging,
		imports:     imports,
		search:      searchSvc,
		log:         log,
	}
}

// Run starts long-polling and blocks until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			return ctx.Err()
		case update := <-updates:
			b.handleUpdate(ctx, update)
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	switch {
	case update.InlineQuery != nil:
		b.handleInlineQuery(ctx, update.InlineQuery)
	case update.Message != nil:
		b.handleMessage(ctx, update.Message)
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil {
		return
	}

	// Public commands
	if msg.IsCommand() {
		switch msg.Command() {
		case "start":
			b.handleStartCommand(msg)
			return
		}
	}

	// admin-only
	if msg.From.ID != b.adminUserID {
		return
	}

	b.handleAdminMessage(ctx, msg)
}

func (b *Bot) handleAdminMessage(ctx context.Context, msg *tgbotapi.Message) {
	staged := importer.FromMessage(msg)
	if err := b.staging.Upsert(ctx, staged); err != nil {
		b.log.Error("stage message failed", "error", err)
	}

	if msg.IsCommand() {
		switch msg.Command() {
		case "import":
			b.handleImportCommand(ctx, msg)
		}
	}
}

func (b *Bot) handleStartCommand(msg *tgbotapi.Message) {
	query := ""

	button := tgbotapi.InlineKeyboardButton{
		Text:                         "🔍 Искать клипы",
		SwitchInlineQueryCurrentChat: &query,
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(button),
	)

	reply := tgbotapi.NewMessage(
		msg.Chat.ID,
		"Предоставляю клипы с Меллстроем, я работаю только в inline-режиме.\n\nДля поиска нажми кнопку ниже 👇",
	)
	reply.ReplyMarkup = keyboard

	if _, err := b.api.Send(reply); err != nil {
		b.log.Error("send start message failed", "error", err)
	}
}

func (b *Bot) handleImportCommand(ctx context.Context, cmd *tgbotapi.Message) {
	if cmd.ReplyToMessage == nil {
		b.reply(cmd.Chat.ID, "Reply to the FIRST forwarded video with /import.")
		return
	}

	startID := cmd.ReplyToMessage.MessageID
	endID := cmd.MessageID

	b.reply(cmd.Chat.ID, "Starting import…")

	imp, err := b.imports.RunImport(ctx, cmd.Chat.ID, startID, endID, cmd.From.ID)
	if err != nil {
		b.reply(cmd.Chat.ID, "Import failed: "+err.Error())
		return
	}

	b.reply(cmd.Chat.ID, formatImportSummary(imp))
}

func formatImportSummary(imp *domain.Import) string {
	return "Import complete.\n" +
		"Imported: " + itoa(imp.ImportedCount) + "\n" +
		"Skipped (duplicates): " + itoa(imp.SkippedCount) + "\n" +
		"Failed: " + itoa(imp.FailedCount)
}

func (b *Bot) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send reply failed", "error", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
