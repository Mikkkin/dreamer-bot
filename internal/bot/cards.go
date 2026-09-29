package bot

import (
	"context"
	"errors"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// replaceMessage shows a new view in place of msg. A photo message cannot
// become a text message, and very old messages cannot be edited, so both
// fall back to sending a new message.
func (a *app) replaceMessage(ctx context.Context, msg *models.Message, text string, kb *models.InlineKeyboardMarkup) {
	if len(msg.Photo) == 0 {
		err := editHTML(ctx, a.api, msg.Chat.ID, msg.ID, false, text, kb)
		if err == nil {
			return
		}
		if !errors.Is(err, tg.ErrorBadRequest) {
			a.log.Warn("bot: edit message failed", "err", err)
			return
		}
	}
	if _, err := sendHTML(ctx, a.api, msg.Chat.ID, text, kb); err != nil {
		a.log.Warn("bot: send message failed", "err", err)
	}
}

// sendCard sends a wish or recipe card: the cover photo with a caption when
// possible, the text version otherwise.
func (a *app) sendCard(ctx context.Context, chatID int64, c card) {
	if c.cover != nil && a.sendCover(ctx, chatID, *c.cover, c.caption, c.kb) {
		return
	}
	if _, err := sendHTML(ctx, a.api, chatID, c.text, c.kb); err != nil {
		a.log.Warn("bot: send card failed", "err", err)
	}
}

// coverVariants are tried in order: Telegram rejects photos with extreme
// proportions, which the small rendition does not have.
var coverVariants = []service.ImageVariant{service.VariantFull, service.VariantThumb}

func (a *app) sendCover(ctx context.Context, chatID int64, img domain.ImageID, caption string, kb *models.InlineKeyboardMarkup) bool {
	for _, v := range coverVariants {
		f, err := a.svc.Images.Open(ctx, img, v)
		if err != nil {
			a.log.Warn("bot: open cover failed", "image_id", img, "err", err)
			return false
		}
		_, err = a.api.SendPhoto(ctx, &tg.SendPhotoParams{
			ChatID:      chatID,
			Photo:       &models.InputFileUpload{Filename: "cover.jpg", Data: f.Content},
			Caption:     caption,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: markup(kb),
		})
		_ = f.Content.Close()
		if err == nil {
			return true
		}
		a.log.Warn("bot: send cover failed", "image_id", img, "variant", string(v), "err", err)
	}
	return false
}

// editCard re-renders a card in place, as a caption or as text depending
// on the kind of message it was sent as.
func (a *app) editCard(ctx context.Context, msg *models.Message, c card) {
	a.editCardAt(ctx, msg.Chat.ID, msg.ID, len(msg.Photo) > 0, c)
}

// editCardAt is editCard for a message known by its IDs.
func (a *app) editCardAt(ctx context.Context, chatID int64, messageID int, isPhoto bool, c card) {
	text := c.text
	if isPhoto {
		text = c.caption
	}
	if err := editHTML(ctx, a.api, chatID, messageID, isPhoto, text, c.kb); err != nil {
		a.log.Warn("bot: edit card failed", "err", err)
	}
}

func (a *app) setKeyboard(ctx context.Context, msg *models.Message, kb *models.InlineKeyboardMarkup) {
	_, err := a.api.EditMessageReplyMarkup(ctx, &tg.EditMessageReplyMarkupParams{
		ChatID:      msg.Chat.ID,
		MessageID:   msg.ID,
		ReplyMarkup: markup(kb),
	})
	if err != nil && !isNotModified(err) {
		a.log.Warn("bot: edit keyboard failed", "err", err)
	}
}

// removeCard deletes a card after its entity was deleted; if the message
// is too old to delete, it is replaced by a short note.
func (a *app) removeCard(ctx context.Context, msg *models.Message) {
	if _, err := a.api.DeleteMessage(ctx, &tg.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID}); err == nil {
		return
	}
	note := newHTML(maxMessageLen).Text("🗑 Удалено").String()
	if err := editHTML(ctx, a.api, msg.Chat.ID, msg.ID, len(msg.Photo) > 0, note, nil); err != nil {
		a.log.Warn("bot: mark card deleted failed", "err", err)
	}
}

func (a *app) askDelete(ctx context.Context, r *cbReply, msg *models.Message, id int64, yes, no cbOp) {
	a.setKeyboard(ctx, msg, confirmDeleteKeyboard(id, yes, no))
	r.answer(ctx, "Точно удалить?")
}

// meta resolves the category label and the author name shown on a card.
// Both are decorations, so lookup failures degrade to defaults.
func (a *app) meta(ctx context.Context, author domain.UserID, category *domain.CategoryID) entityMeta {
	m := entityMeta{author: domain.User{}.DisplayName()}
	if u, err := a.svc.Users.Get(ctx, author); err == nil {
		m.author = u.DisplayName()
	}
	if category != nil {
		if c, err := a.svc.Categories.Get(ctx, *category); err == nil {
			m.category = c.Label()
		}
	}
	return m
}
