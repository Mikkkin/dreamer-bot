package bot

import (
	"context"
	"errors"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// sendWishList answers /list with the «Хотим» tab.
func (a *app) sendWishList(ctx context.Context, chatID int64) {
	text, kb, err := a.wishListView(ctx, domain.StatusWant, 0)
	if err != nil {
		a.say(ctx, chatID, a.userError(err, "list wishes"))
		return
	}
	if _, err := sendHTML(ctx, a.api, chatID, text, kb); err != nil {
		a.log.Warn("bot: send wish list failed", "err", err)
	}
}

func (a *app) showWishList(ctx context.Context, r *cbReply, msg *models.Message, status domain.Status, page int) {
	text, kb, err := a.wishListView(ctx, status, page)
	if err != nil {
		r.answer(ctx, a.userError(err, "list wishes"))
		return
	}
	a.replaceMessage(ctx, msg, text, kb)
}

func (a *app) wishListView(ctx context.Context, status domain.Status, page int) (string, *models.InlineKeyboardMarkup, error) {
	all, err := a.svc.Wishes.List(ctx, domain.WishFilter{})
	if err != nil {
		return "", nil, err
	}
	counts := make(map[domain.Status]int, len(domain.Statuses))
	var items []domain.Wish
	for _, w := range all {
		counts[w.Status]++
		if w.Status == status {
			items = append(items, w)
		}
	}
	page, pages, from, to := pageBounds(len(items), page)
	return renderWishList(status, len(items)), wishListKeyboard(status, counts, items[from:to], page, pages), nil
}

func (a *app) wishCard(ctx context.Context, w domain.Wish) card {
	return wishCard(w, a.meta(ctx, w.AuthorID, w.CategoryID), a.loc, a.web.wishLink(w.ID))
}

func (a *app) openWish(ctx context.Context, r *cbReply, chatID int64, id domain.WishID) {
	w, err := a.svc.Wishes.Get(ctx, id)
	if err != nil {
		r.answer(ctx, a.userError(err, "get wish"))
		return
	}
	r.answer(ctx, "")
	a.sendCard(ctx, chatID, a.wishCard(ctx, w))
}

func (a *app) setWishStatus(ctx context.Context, r *cbReply, msg *models.Message, user domain.UserID, id domain.WishID, status domain.Status) {
	before, err := a.svc.Wishes.Get(ctx, id)
	if err != nil {
		r.answer(ctx, a.userError(err, "get wish"))
		return
	}
	w, err := a.svc.Wishes.SetStatus(ctx, user, id, status)
	if err != nil {
		r.answer(ctx, a.userError(err, "set wish status"))
		return
	}
	a.editCard(ctx, msg, a.wishCard(ctx, w))
	if w.Status != domain.StatusDone || before.Status == domain.StatusDone {
		r.answer(ctx, w.Status.Emoji()+" "+w.Status.Label())
		return
	}
	r.answer(ctx, "✨ Ура!")
	h := newHTML(maxMessageLen).Text("✨ Ура! Мечта сбылась: ").Bold(w.Title).Text(" 🎉")
	if _, err := a.api.SendMessage(ctx, &tg.SendMessageParams{
		ChatID:          msg.Chat.ID,
		Text:            h.String(),
		ParseMode:       models.ParseModeHTML,
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID, AllowSendingWithoutReply: true},
	}); err != nil {
		a.log.Warn("bot: send celebration failed", "err", err)
	}
}

func (a *app) deleteWish(ctx context.Context, r *cbReply, msg *models.Message, user domain.UserID, id domain.WishID) {
	if err := a.svc.Wishes.Delete(ctx, user, id); err != nil && !errors.Is(err, domain.ErrNotFound) {
		r.answer(ctx, a.userError(err, "delete wish"))
		return
	}
	a.removeCard(ctx, msg)
	r.answer(ctx, "Удалено 🗑")
}

func (a *app) keepWish(ctx context.Context, r *cbReply, msg *models.Message, id domain.WishID) {
	w, err := a.svc.Wishes.Get(ctx, id)
	if err != nil {
		r.answer(ctx, a.userError(err, "get wish"))
		a.setKeyboard(ctx, msg, nil)
		return
	}
	a.setKeyboard(ctx, msg, wishCardKeyboard(w, a.web.wishLink(w.ID)))
	r.answer(ctx, "Оставили 👌")
}
