package bot

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"mellclipsbot/internal/domain"
)

// handleInlineQuery serves the public surface. It only ever reads
// already-processed, cached clips (status READY) and returns their stored
// telegram_file_id — no download, no ffmpeg, no re-upload happens here.
//
// IMPORTANT: Telegram's inline result types do not include a "cached video
// note" type, so round video-note bubbles cannot be produced via inline
// mode. This returns InlineQueryResultCachedVideo, which plays the clip as
// a normal (square) video. See the architecture note delivered alongside
// this code for the direct-DM sendVideoNote alternative if the round
// bubble is required.
func (b *Bot) handleInlineQuery(ctx context.Context, q *tgbotapi.InlineQuery) {
	const limit = 50

	offset := 0
	if q.Offset != "" {
		if n, err := strconv.Atoi(q.Offset); err == nil {
			offset = n
		}
	}

	clips, err := b.search.Search(ctx, q.Query, limit, offset)
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
		CacheTime:     300, // safe to cache: results only reference already-processed clips
		IsPersonal:    false,
		NextOffset:    nextOffset,
	}
	if _, err := b.api.Request(inlineConf); err != nil {
		b.log.Error("answer inline query failed", "error", err)
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
		Caption: title,
	}
}

// resultID must be stable and under 64 bytes per Telegram's limits; the
// clip ID alone is fine, but hashing keeps this robust if IDs ever need to
// be namespaced (e.g. multi-provider results later).
func resultID(c *domain.Clip) string {
	h := sha1.Sum([]byte(fmt.Sprintf("clip:%d", c.ID)))
	return hex.EncodeToString(h[:])[:32]
}
