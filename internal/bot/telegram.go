package bot

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// messenger is the part of the Bot API used to talk in chats. The handlers,
// the middlewares and the notifier depend on it instead of *tg.Bot so they
// can be tested with a fake.
type messenger interface {
	SendMessage(ctx context.Context, p *tg.SendMessageParams) (*models.Message, error)
	SendPhoto(ctx context.Context, p *tg.SendPhotoParams) (*models.Message, error)
	EditMessageText(ctx context.Context, p *tg.EditMessageTextParams) (*models.Message, error)
	EditMessageCaption(ctx context.Context, p *tg.EditMessageCaptionParams) (*models.Message, error)
	EditMessageReplyMarkup(ctx context.Context, p *tg.EditMessageReplyMarkupParams) (*models.Message, error)
	DeleteMessage(ctx context.Context, p *tg.DeleteMessageParams) (bool, error)
	AnswerCallbackQuery(ctx context.Context, p *tg.AnswerCallbackQueryParams) (bool, error)
	SetMessageReaction(ctx context.Context, p *tg.SetMessageReactionParams) (bool, error)
	LeaveChat(ctx context.Context, p *tg.LeaveChatParams) (bool, error)
}

// fileSource resolves Telegram file IDs to downloadable files.
type fileSource interface {
	GetFile(ctx context.Context, p *tg.GetFileParams) (*models.File, error)
	// FileDownloadLink embeds the bot token, so its result is a secret.
	FileDownloadLink(f *models.File) string
}

// botSetup configures the bot account itself.
type botSetup interface {
	DeleteWebhook(ctx context.Context, p *tg.DeleteWebhookParams) (bool, error)
	SetMyCommands(ctx context.Context, p *tg.SetMyCommandsParams) (bool, error)
	SetChatMenuButton(ctx context.Context, p *tg.SetChatMenuButtonParams) (bool, error)
}

// telegramAPI is everything the package needs from the Bot API.
type telegramAPI interface {
	messenger
	fileSource
	botSetup
}

var _ telegramAPI = (*tg.Bot)(nil)

// Telegram limits, counted in visible characters after entity parsing.
const (
	maxMessageLen = 4096
	maxCaptionLen = 1024
)

// markup converts an optional keyboard into the interface the library
// expects. A typed nil pointer would be encoded as "null", which Telegram
// rejects, so nil must stay an untyped nil.
func markup(kb *models.InlineKeyboardMarkup) models.ReplyMarkup {
	if kb == nil {
		return nil
	}
	return kb
}

func noLinkPreview() *models.LinkPreviewOptions {
	return &models.LinkPreviewOptions{IsDisabled: tg.True()}
}

// sendHTML sends an HTML message without a link preview.
func sendHTML(ctx context.Context, api messenger, chatID int64, text string, kb *models.InlineKeyboardMarkup) (*models.Message, error) {
	return api.SendMessage(ctx, &tg.SendMessageParams{
		ChatID:             chatID,
		Text:               text,
		ParseMode:          models.ParseModeHTML,
		LinkPreviewOptions: noLinkPreview(),
		ReplyMarkup:        markup(kb),
	})
}

// editHTML replaces the text (or the caption of a photo message) and the
// keyboard of a message sent by the bot. Omitting the keyboard removes it.
func editHTML(ctx context.Context, api messenger, chatID int64, messageID int, isPhoto bool, text string, kb *models.InlineKeyboardMarkup) error {
	var err error
	if isPhoto {
		_, err = api.EditMessageCaption(ctx, &tg.EditMessageCaptionParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Caption:     text,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: markup(kb),
		})
	} else {
		_, err = api.EditMessageText(ctx, &tg.EditMessageTextParams{
			ChatID:             chatID,
			MessageID:          messageID,
			Text:               text,
			ParseMode:          models.ParseModeHTML,
			LinkPreviewOptions: noLinkPreview(),
			ReplyMarkup:        markup(kb),
		})
	}
	if isNotModified(err) {
		return nil
	}
	return err
}

// isNotModified reports Telegram's refusal to apply an edit that changes
// nothing, which is harmless (e.g. a double tap on the same button).
func isNotModified(err error) bool {
	return err != nil && errors.Is(err, tg.ErrorBadRequest) && strings.Contains(err.Error(), "message is not modified")
}

// deleteQuietly removes a message and only logs a failure: a message older
// than 48 hours cannot be deleted, which must never break a flow.
func deleteQuietly(ctx context.Context, api messenger, log *slog.Logger, chatID int64, messageID int) {
	if messageID == 0 {
		return
	}
	if _, err := api.DeleteMessage(ctx, &tg.DeleteMessageParams{ChatID: chatID, MessageID: messageID}); err != nil {
		log.Debug("bot: delete message failed", "err", err)
	}
}

// redactor scrubs the bot token out of text. FileDownloadLink URLs carry the
// token, and net/http puts the URL into every *url.Error it returns.
type redactor struct {
	secrets []string
}

const redacted = "<redacted>"

func newRedactor(token string) redactor {
	var secrets []string
	add := func(s string) {
		if s != "" && !slices.Contains(secrets, s) {
			secrets = append(secrets, s)
		}
	}
	add(token)
	add(url.PathEscape(token))
	add(url.QueryEscape(token))
	// The secret half alone is long enough to be unambiguous and must not
	// leak either, e.g. if a URL was split at the colon.
	if _, secret, ok := strings.Cut(token, ":"); ok && len(secret) >= 16 {
		add(secret)
	}
	return redactor{secrets: secrets}
}

func (r redactor) string(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, redacted)
	}
	return s
}

// err returns a new error with the same text minus the secrets. It
// deliberately does not wrap the original: unwrapping would expose the token
// again to whoever formats the cause.
func (r redactor) err(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(r.string(err.Error()))
}
