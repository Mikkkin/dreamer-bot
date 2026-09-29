package bot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// savingPrompt is a pending «💰 Отложить»: the bot asked for an amount with a
// ForceReply prompt, and the user's answer adds a saving to the wish.
type savingPrompt struct {
	wish      domain.WishID
	chatID    int64
	promptID  int  // message ID of the ForceReply prompt
	cardID    int  // the wish card to refresh afterwards
	cardPhoto bool // the card is a photo with a caption
	asked     time.Time
}

// savingPrompts keeps at most one pending prompt per user. Prompts expire
// after ttl, like drafts.
type savingPrompts struct {
	mu      sync.Mutex
	prompts map[domain.UserID]savingPrompt
	ttl     time.Duration
	now     func() time.Time
}

func newSavingPrompts(ttl time.Duration, now func() time.Time) *savingPrompts {
	return &savingPrompts{prompts: make(map[domain.UserID]savingPrompt), ttl: ttl, now: now}
}

func (s *savingPrompts) get(user domain.UserID) (savingPrompt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prompts[user]
	if ok && s.now().Sub(p.asked) >= s.ttl {
		delete(s.prompts, user)
		return savingPrompt{}, false
	}
	return p, ok
}

func (s *savingPrompts) put(user domain.UserID, p savingPrompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.asked = s.now()
	s.prompts[user] = p
}

// take removes and returns the user's live prompt.
func (s *savingPrompts) take(user domain.UserID) (savingPrompt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prompts[user]
	delete(s.prompts, user)
	if ok && s.now().Sub(p.asked) >= s.ttl {
		return savingPrompt{}, false
	}
	return p, ok
}

// savingsCurrency is the currency a new saving of w must use: the wish's
// (see Wish.SavingsCurrency) or, for a wish without price and savings, the
// default currency.
func (a *app) savingsCurrency(w domain.Wish) (domain.Currency, bool) {
	if c, ok := w.SavingsCurrency(); ok {
		return c, true
	}
	return a.currency, false
}

// askSaving starts «Отложить» on a wish card: a ForceReply prompt for the
// amount replaces any other pending input of the user.
func (a *app) askSaving(ctx context.Context, r *cbReply, msg *models.Message, user domain.UserID, id domain.WishID) {
	w, err := a.svc.Wishes.Get(ctx, id)
	if err != nil {
		r.answer(ctx, a.userError(err, "get wish"))
		return
	}
	if w.Status == domain.StatusDone {
		r.answer(ctx, "Это желание уже сбылось ✨")
		return
	}
	a.dropPrompts(ctx, user)
	currency, fixed := a.savingsCurrency(w)
	h := newHTML(maxMessageLen)
	h.Text("💰 Сколько отложить на «" + w.Title + "»?").NL()
	h.Text("Отправьте сумму, например ").Code("5000").Text(" — в " + currency.Symbol())
	if !fixed {
		h.Text(" (или с другим знаком валюты: ").Code("50 $").Text(")")
	}
	h.NL().Text("/cancel чтобы отменить")
	prompt, err := a.api.SendMessage(ctx, &tg.SendMessageParams{
		ChatID:    msg.Chat.ID,
		Text:      h.String(),
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: &models.ForceReply{
			ForceReply:            true,
			InputFieldPlaceholder: "5000 " + currency.Symbol(),
		},
	})
	if err != nil {
		r.answer(ctx, a.userError(err, "ask saving amount"))
		return
	}
	a.savings.put(user, savingPrompt{
		wish:      w.ID,
		chatID:    msg.Chat.ID,
		promptID:  prompt.ID,
		cardID:    msg.ID,
		cardPhoto: len(msg.Photo) > 0,
	})
	r.answer(ctx, "")
}

// fillSaving handles a text message while a saving prompt is pending. A
// reply to the prompt, or a message that is just an amount, is the answer;
// anything else abandons the prompt and is handled as usual (false), so a
// new wish such as «Поездка 1200€» is never booked as money put aside.
func (a *app) fillSaving(ctx context.Context, m *models.Message, p savingPrompt) bool {
	user := domain.UserID(m.From.ID)
	w, err := a.svc.Wishes.Get(ctx, p.wish)
	if err != nil {
		a.dropSaving(ctx, user)
		a.say(ctx, m.Chat.ID, a.userError(err, "get wish"))
		return true
	}
	if w.Status == domain.StatusDone { // fulfilled since the prompt was sent
		a.dropSaving(ctx, user)
		a.say(ctx, m.Chat.ID, "Это желание уже сбылось ✨ Откладывать на него больше не нужно.")
		return true
	}
	currency, _ := a.savingsCurrency(w)
	amount, err := parseSavingAmount(m.Text, currency)
	replied := m.ReplyToMessage != nil && m.ReplyToMessage.ID == p.promptID
	if err != nil && !replied {
		a.dropSaving(ctx, user)
		return false
	}
	if err == nil {
		// Validate locally for the same message whatever the storage does,
		// e.g. «копим в RUB — укажите сумму в этой валюте».
		_, err = domain.NewSaving(w, amount, user, "", a.now())
	}
	var saving domain.Saving
	if err == nil {
		saving, err = a.svc.Wishes.AddSaving(ctx, user, w.ID, amount, "")
	}
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			a.dropSaving(ctx, user)
			a.say(ctx, m.Chat.ID, a.userError(err, "add saving"))
			return true
		}
		a.say(ctx, m.Chat.ID, a.userError(err, "add saving")+"\nПопробуйте ещё раз или отправьте /cancel.")
		return true
	}

	a.savings.take(user)
	deleteQuietly(ctx, a.api, a.log, m.Chat.ID, p.promptID)
	deleteQuietly(ctx, a.api, a.log, m.Chat.ID, m.ID)
	if fresh, err := a.svc.Wishes.Get(ctx, w.ID); err == nil {
		w = fresh
	} else {
		a.log.Warn("bot: reload wish after saving failed", "err", err)
	}
	if _, err := sendHTML(ctx, a.api, m.Chat.ID, renderSavingAdded(w, saving), nil); err != nil {
		a.log.Warn("bot: send saving confirmation failed", "err", err)
	}
	if p.cardID != 0 {
		a.editCardAt(ctx, p.chatID, p.cardID, p.cardPhoto, a.wishCard(ctx, w))
	}
	return true
}

// parseSavingAmount accepts a message that is only an amount, with or
// without a currency: "5000", "5 000,50", "5 000 ₽", "€50".
func parseSavingAmount(s string, fallback domain.Currency) (domain.Money, error) {
	s = strings.TrimSpace(s)
	if m, sp, ok := findPrice(s); ok {
		if strings.TrimFunc(s[:sp.start]+s[sp.end:], isFiller) != "" {
			return domain.Money{}, &domain.ValidationError{Field: "amount", Message: "отправьте только сумму, например 5000"}
		}
		return m, nil
	}
	return domain.ParseAmount(s, fallback)
}

// dropSaving abandons the user's saving prompt. It reports whether there was
// one.
func (a *app) dropSaving(ctx context.Context, user domain.UserID) bool {
	p, ok := a.savings.take(user)
	if ok {
		deleteQuietly(ctx, a.api, a.log, p.chatID, p.promptID)
	}
	return ok
}

// dropPrompts leaves every "waiting for your answer" state of the user (a
// draft field or a saving amount). It reports whether there was one.
func (a *app) dropPrompts(ctx context.Context, user domain.UserID) bool {
	saving := a.dropSaving(ctx, user)
	field := a.dropAwaiting(ctx, user)
	return saving || field
}
