package importer

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"mellclipsbot/internal/domain"
)

// FromMessage builds a StagedMessage from any message the bot receives in
// the admin's chat. Only Video is treated as importable; VideoNote is
// deliberately excluded here because a video note re-forwarded from
// another channel is already circular/processed and not what this bot
// imports from (source clips are expected as regular videos).
func FromMessage(msg *tgbotapi.Message) *domain.StagedMessage {
	sm := &domain.StagedMessage{
		ChatID:     msg.Chat.ID,
		MessageID:  msg.MessageID,
		FromUserID: msg.From.ID,
		Caption:    msg.Caption,
	}

	if msg.Video != nil {
		sm.HasVideo = true
		sm.FileID = msg.Video.FileID
		sm.FileUniqueID = msg.Video.FileUniqueID
		sm.Duration = msg.Video.Duration
		sm.Width = msg.Video.Width
		sm.Height = msg.Video.Height
		sm.MimeType = msg.Video.MimeType
		sm.Size = int64(msg.Video.FileSize)
	}

	return sm
}
