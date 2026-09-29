package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// sendShopping answers /shop with the first page of the shopping list.
func (a *app) sendShopping(ctx context.Context, chatID int64) {
	text, kb, err := a.shoppingView(ctx, 0)
	if err != nil {
		a.say(ctx, chatID, a.userError(err, "list shopping"))
		return
	}
	if _, err := sendHTML(ctx, a.api, chatID, text, kb); err != nil {
		a.log.Warn("bot: send shopping list failed", "err", err)
	}
}

func (a *app) shoppingView(ctx context.Context, page int) (string, *models.InlineKeyboardMarkup, error) {
	items, err := a.svc.Shopping.List(ctx)
	if err != nil {
		return "", nil, err
	}
	v := newShopView(items, page)
	return renderShopping(v), shoppingKeyboard(v, a.web.shoppingLink()), nil
}

// showShopping re-renders the list in place (both partners may have changed
// it since the message was sent) and answers the button with toast.
func (a *app) showShopping(ctx context.Context, r *cbReply, msg *models.Message, page int, toast string) {
	text, kb, err := a.shoppingView(ctx, page)
	if err != nil {
		r.answer(ctx, a.userError(err, "list shopping"))
		return
	}
	a.replaceMessage(ctx, msg, text, kb)
	r.answer(ctx, toast)
}

// markBought sets the item to the state its button showed the opposite of,
// so a tap on an out-of-date list never undoes the partner's change.
func (a *app) markBought(ctx context.Context, r *cbReply, msg *models.Message, user domain.UserID, id domain.ShoppingItemID, bought bool, page int) {
	_, err := a.svc.Shopping.Update(ctx, user, id, domain.ShoppingPatch{Checked: domain.Some(bought)})
	toast := "↩️ Снова в списке"
	switch {
	case errors.Is(err, domain.ErrNotFound):
		toast = "Этой позиции уже нет в списке"
	case err != nil:
		r.answer(ctx, a.userError(err, "update shopping item"))
		return
	case bought:
		toast = "☑ Куплено"
	}
	a.showShopping(ctx, r, msg, page, toast)
}

func (a *app) clearBought(ctx context.Context, r *cbReply, msg *models.Message, user domain.UserID) {
	n, err := a.svc.Shopping.ClearChecked(ctx, user)
	if err != nil {
		r.answer(ctx, a.userError(err, "clear shopping list"))
		return
	}
	toast := "Купленного нет"
	if n > 0 {
		toast = "🧹 Убрали купленное: " + strconv.Itoa(n)
	}
	a.showShopping(ctx, r, msg, 0, toast)
}
