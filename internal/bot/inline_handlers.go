package bot

import (
	"context"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"mellclipsbot/internal/domain"
)

// handleInlineQuery serves the public surface. It only ever reads
// already-processed, cached clips (status READY) and returns their stored
// telegram_file_id — no download, no ffmpeg, no re-upload happens here
func (b *Bot) handleInlineQuery(ctx context.Context, q *tgbotapi.InlineQuery) {
	const limit = 50

	offset := 0
	if q.Offset != "" {
		if n, err := strconv.Atoi(q.Offset); err == nil {
			offset = n
		}
	}

	clips, err := b.search.Search(ctx, q.From.ID, q.Query, limit, offset)
	if err != nil {
		b.log.Error("inline search failed", "error", err, "query", q.Query)
		clips = nil
	}

	results := make([]interface{}, 0, len(clips))
	for _, c := range clips {
		results = append(results, buildInlineResult(c))
	}

	nextOffset := ""
	if len(clips) == limit {
		nextOffset = strconv.Itoa(offset + limit)
	}

	inlineConf := tgbotapi.InlineConfig{
		InlineQueryID: q.ID,
		Results:       results,
		CacheTime:     0,
		IsPersonal:    true,
		NextOffset:    nextOffset,
	}
	if _, err := b.api.Request(inlineConf); err != nil {
		b.log.Error("answer inline query failed", "error", err)
	}
}

func (b *Bot) handleChosenInlineResult(ctx context.Context, r *tgbotapi.ChosenInlineResult) {
	clipID, err := strconv.ParseInt(r.ResultID, 10, 64) // or base36, matching resultID above
	if err != nil {
		b.log.Error("parse chosen result id failed", "error", err, "result_id", r.ResultID)
		return
	}
	if err := b.search.RecordSend(ctx, r.From.ID, clipID); err != nil {
		b.log.Error("record clip send failed", "error", err, "clip_id", clipID, "user_id", r.From.ID)
	}
}

func buildInlineResult(c *domain.Clip) tgbotapi.InlineQueryResultCachedVideo {
	title := c.CleanTitle
	if title == "" {
		title = "Mellstroy clip"
	}
	return tgbotapi.InlineQueryResultCachedVideo{
		Type:    "video",
		ID:      resultID(c),
		VideoID: c.TelegramFileID,
		Title:   title,
	}
}

func resultID(c *domain.Clip) string {
	return strconv.FormatInt(c.ID, 10)
}
