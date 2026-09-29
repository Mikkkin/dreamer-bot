package bot

import (
	"context"
	"slices"
	"strings"
	"testing"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func replyUpdate(user domain.UserID, text string, replyTo int) *models.Update {
	u := textUpdate(user, text)
	u.Message.ReplyToMessage = &models.Message{ID: replyTo, Chat: privateChat(user)}
	return u
}

func (e *testEnv) addPricedWish(t *testing.T, title string, price *domain.Money, status domain.Status) domain.Wish {
	t.Helper()
	w, err := e.svc.wishes.Create(context.Background(), alice, domain.WishDraft{Title: title, Price: price})
	if err != nil {
		t.Fatal(err)
	}
	if status != domain.StatusWant {
		if w, err = e.svc.wishes.SetStatus(context.Background(), alice, w.ID, status); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (e *testEnv) savingPrompt(t *testing.T) savingPrompt {
	t.Helper()
	p, ok := e.app.savings.get(alice)
	if !ok {
		t.Fatal("no pending saving prompt")
	}
	return p
}

func TestSavingFlow(t *testing.T) {
	e := newTestEnv(t)
	price := domain.Money{Minor: 4_500_000, Currency: "RUB"}
	w := e.addPricedWish(t, "Диван", &price, domain.StatusProgress)

	e.handle(callbackUpdate(alice, callback{op: opWishOpen, id: int64(w.ID)}.String(), cardMessage(alice, 50)))
	card := e.api.lastSent(t)
	if !strings.Contains(card.Text, "💰 Накоплено 0 из "+price.Format()+" (0%)\n▱▱▱▱▱▱▱▱▱▱") {
		t.Errorf("progress card %q", card.Text)
	}
	if strings.Contains(card.Text, "💰 "+price.Format()+"\n") {
		t.Error("the price is shown twice")
	}
	save := callback{op: opWishSave, id: int64(w.ID)}.String()
	if !slices.Contains(callbackData(card.ReplyMarkup), save) || !slices.Contains(buttonTexts(card.ReplyMarkup), "💰 Отложить") {
		t.Fatalf("no «Отложить» button: %v", buttonTexts(card.ReplyMarkup))
	}

	e.api.reset()
	e.handle(callbackUpdate(alice, save, cardMessage(alice, 60)))
	prompt := e.api.lastSent(t)
	fr, ok := prompt.ReplyMarkup.(*models.ForceReply)
	if !ok || !fr.ForceReply || fr.InputFieldPlaceholder != "5000 ₽" {
		t.Fatalf("prompt markup %#v", prompt.ReplyMarkup)
	}
	if !strings.Contains(prompt.Text, "Сколько отложить на «Диван»") || !strings.Contains(prompt.Text, "в ₽") ||
		!strings.Contains(prompt.Text, "/cancel") {
		t.Errorf("prompt %q", prompt.Text)
	}
	p := e.savingPrompt(t)

	// An amount in another currency is refused with the reason, and the
	// prompt keeps waiting.
	e.api.reset()
	e.handle(replyUpdate(alice, "50€", p.promptID))
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "Копим в RUB — укажите сумму в этой валюте") ||
		!strings.Contains(msg, "/cancel") {
		t.Errorf("currency mismatch reply %q", msg)
	}
	if e.svc.wishes.savingsCount() != 0 {
		t.Fatal("saved in the wrong currency")
	}
	e.handle(replyUpdate(alice, "ноль", p.promptID))
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "Не получилось распознать сумму") {
		t.Errorf("garbage reply %q", msg)
	}
	e.handle(replyUpdate(alice, "0", p.promptID))
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "больше нуля") {
		t.Errorf("zero reply %q", msg)
	}
	e.savingPrompt(t)

	e.api.reset()
	e.handle(replyUpdate(alice, "12 000", p.promptID))
	saved := e.svc.wishes.savings
	if len(saved) != 1 || saved[0].Amount != (domain.Money{Minor: 1_200_000, Currency: "RUB"}) || saved[0].UserID != alice {
		t.Fatalf("savings %+v", saved)
	}
	if _, ok := e.app.savings.get(alice); ok {
		t.Error("prompt still pending after the saving")
	}
	if n := e.api.count("DeleteMessage"); n != 2 {
		t.Errorf("prompt and answer not cleaned up: %d deletes", n)
	}
	total := domain.Money{Minor: 1_200_000, Currency: "RUB"}
	confirm := e.api.lastSent(t).Text
	for _, want := range []string{"Отложено " + total.Format(), "«Диван»", "Накоплено " + amountDigits(total) + " из " + price.Format() + " (26%)", "▰▰▱▱▱▱▱▱▱▱"} {
		if !strings.Contains(confirm, want) {
			t.Errorf("confirmation %q lacks %q", confirm, want)
		}
	}
	edit := e.api.lastEdit(t)
	if edit.MessageID != 60 || !strings.Contains(edit.Text, "(26%)") {
		t.Errorf("card not refreshed: %+v", edit)
	}

	// The next message is an ordinary quick add again.
	e.api.reset()
	e.handle(textUpdate(alice, "5000"))
	if e.svc.wishes.savingsCount() != 1 || !strings.Contains(e.api.lastSent(t).Text, "Новое желание") {
		t.Error("a message after the saving was booked as another saving")
	}
}

func TestSavingWithoutPriceUsesAnyCurrency(t *testing.T) {
	e := newTestEnv(t)
	w := e.addPricedWish(t, "Путешествие", nil, domain.StatusProgress)
	photoCard := &models.Message{ID: 61, Chat: privateChat(alice), Photo: []models.PhotoSize{{FileID: "x"}}}
	e.handle(callbackUpdate(alice, callback{op: opWishSave, id: int64(w.ID)}.String(), photoCard))
	if prompt := e.api.lastSent(t); !strings.Contains(prompt.Text, "в €") || !strings.Contains(prompt.Text, "50 $") {
		t.Errorf("prompt %q", prompt.Text)
	}
	// A bare amount (not a reply) is still the answer.
	e.handle(textUpdate(alice, "$50"))
	if saved := e.svc.wishes.savings; len(saved) != 1 || saved[0].Amount != (domain.Money{Minor: 5000, Currency: "USD"}) {
		t.Fatalf("savings %+v", saved)
	}
	captions := e.api.of("EditMessageCaption")
	if len(captions) != 1 || !strings.Contains(captions[0].(*tg.EditMessageCaptionParams).Caption, "Накоплено") {
		t.Errorf("photo card not refreshed: %+v", captions)
	}

	// From now on the wish is saved for in dollars.
	e.handle(callbackUpdate(alice, callback{op: opWishSave, id: int64(w.ID)}.String(), cardMessage(alice, 62)))
	p := e.savingPrompt(t)
	e.handle(replyUpdate(alice, "30", p.promptID))
	if saved := e.svc.wishes.savings; len(saved) != 2 || saved[1].Amount.Currency != "USD" {
		t.Errorf("savings %+v", saved)
	}
}

func TestSavingPromptDoesNotSwallowOtherMessages(t *testing.T) {
	e := newTestEnv(t)
	w := e.addPricedWish(t, "Диван", nil, domain.StatusProgress)
	e.handle(callbackUpdate(alice, callback{op: opWishSave, id: int64(w.ID)}.String(), cardMessage(alice, 60)))
	p := e.savingPrompt(t)

	// A new wish typed instead of an answer is not money put aside.
	e.api.reset()
	e.handle(textUpdate(alice, "Поездка в Токио 1200€"))
	if e.svc.wishes.savingsCount() != 0 {
		t.Fatal("a quick-add message was booked as a saving")
	}
	if _, ok := e.app.savings.get(alice); ok {
		t.Error("the abandoned prompt is still pending")
	}
	if !strings.Contains(e.api.lastSent(t).Text, "Новое желание") {
		t.Error("the message did not start a draft")
	}
	deleted := e.api.of("DeleteMessage")
	if len(deleted) != 1 || deleted[0].(*tg.DeleteMessageParams).MessageID != p.promptID {
		t.Errorf("abandoned prompt not removed: %+v", deleted)
	}
}

func TestSavingPromptCancelAndCommands(t *testing.T) {
	e := newTestEnv(t)
	w := e.addPricedWish(t, "Диван", nil, domain.StatusProgress)
	ask := callbackUpdate(alice, callback{op: opWishSave, id: int64(w.ID)}.String(), cardMessage(alice, 60))

	e.handle(ask)
	e.handle(commandUpdate(alice, "cancel"))
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "оставил как было") {
		t.Errorf("/cancel reply %q", msg)
	}
	if _, ok := e.app.savings.get(alice); ok {
		t.Fatal("/cancel kept the prompt")
	}

	e.handle(ask)
	e.handle(commandUpdate(alice, "list"))
	if _, ok := e.app.savings.get(alice); ok {
		t.Fatal("another command kept the prompt")
	}

	// Asking for a draft field replaces the saving prompt, and vice versa.
	e.handle(ask)
	e.handle(textUpdate(alice, "Лампа"))
	if _, ok := e.app.savings.get(alice); ok {
		t.Fatal("prompt survived a new draft")
	}
	e.handle(e.draftCallback(t, callback{op: opDraftField, field: fieldPrice}))
	e.handle(ask)
	if d := e.draft(t); d.awaiting != fieldNone {
		t.Error("the draft still awaits a price after «Отложить»")
	}
	e.handle(e.draftCallback(t, callback{op: opDraftField, field: fieldPrice}))
	if _, ok := e.app.savings.get(alice); ok {
		t.Error("the saving prompt survived a draft prompt")
	}
	e.handle(textUpdate(alice, "300"))
	if e.svc.wishes.savingsCount() != 0 || e.draft(t).price == nil {
		t.Error("the answer went to the wrong prompt")
	}
}

func TestSavingPromptExpires(t *testing.T) {
	e := newTestEnv(t)
	w := e.addPricedWish(t, "Диван", nil, domain.StatusProgress)
	e.handle(callbackUpdate(alice, callback{op: opWishSave, id: int64(w.ID)}.String(), cardMessage(alice, 60)))
	e.clock.Advance(draftTTL)
	e.handle(textUpdate(alice, "5000"))
	if e.svc.wishes.savingsCount() != 0 {
		t.Error("an expired prompt booked a saving")
	}
}

func TestSavingButtonRules(t *testing.T) {
	e := newTestEnv(t)
	price := domain.Money{Minor: 10_000, Currency: "EUR"}
	want := e.addPricedWish(t, "Лампа", &price, domain.StatusWant)
	if canSave(want) {
		t.Error("«Отложить» on a «Хотим» wish without savings")
	}
	saved := price
	want.Saved = &saved
	if !canSave(want) {
		t.Error("no «Отложить» on a wish with savings")
	}
	done := e.addPricedWish(t, "Кресло", &price, domain.StatusDone)
	done.Saved = &saved
	if canSave(done) {
		t.Error("«Отложить» on a fulfilled wish")
	}

	// A stale button of a fulfilled wish does not ask for money.
	e.handle(callbackUpdate(alice, callback{op: opWishSave, id: int64(done.ID)}.String(), cardMessage(alice, 60)))
	if got := e.api.answers(); !slices.Equal(got, []string{"Это желание уже сбылось ✨"}) {
		t.Errorf("answers %v", got)
	}
	if _, ok := e.app.savings.get(alice); ok {
		t.Error("prompt for a fulfilled wish")
	}

	// A saving on a «Хотим» wish moves it to «Копим».
	e.svc.wishes.put(domain.Wish{ID: want.ID, Title: want.Title, Price: &price, Status: domain.StatusWant, AuthorID: alice})
	e.handle(callbackUpdate(alice, callback{op: opWishSave, id: int64(want.ID)}.String(), cardMessage(alice, 61)))
	e.handle(replyUpdate(alice, "25,50", e.savingPrompt(t).promptID))
	got, _ := e.svc.wishes.Get(context.Background(), want.ID)
	if got.Status != domain.StatusProgress || got.Saved == nil || got.Saved.Minor != 2550 {
		t.Errorf("wish after saving %+v", got)
	}
	if !strings.Contains(e.api.lastEdit(t).Text, "⏳ Копим") {
		t.Errorf("card %q", e.api.lastEdit(t).Text)
	}
}

func TestParseSavingAmount(t *testing.T) {
	for in, want := range map[string]domain.Money{
		"5000":      {Minor: 500_000, Currency: "RUB"},
		" 5 000,50": {Minor: 500_050, Currency: "RUB"},
		"5 000 ₽":   {Minor: 500_000, Currency: "RUB"},
		"€50":       {Minor: 5000, Currency: "EUR"},
		"50 $.":     {Minor: 5000, Currency: "USD"},
	} {
		if got, err := parseSavingAmount(in, "RUB"); err != nil || got != want {
			t.Errorf("parseSavingAmount(%q) = %+v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "пять", "Поездка 1200€", "1200€ на диван", "-5", "0"} {
		if got, err := parseSavingAmount(bad, "RUB"); err == nil {
			t.Errorf("parseSavingAmount(%q) accepted as %+v", bad, got)
		}
	}
}

func TestSavingPromptForWishFulfilledMeanwhile(t *testing.T) {
	e := newTestEnv(t)
	w := e.addPricedWish(t, "Диван", nil, domain.StatusProgress)
	e.handle(callbackUpdate(alice, callback{op: opWishSave, id: int64(w.ID)}.String(), cardMessage(alice, 60)))
	p := e.savingPrompt(t)
	if _, err := e.svc.wishes.SetStatus(context.Background(), bob, w.ID, domain.StatusDone); err != nil {
		t.Fatal(err)
	}
	e.handle(replyUpdate(alice, "5000", p.promptID))
	if e.svc.wishes.savingsCount() != 0 {
		t.Error("saved for a fulfilled wish")
	}
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "уже сбылось") {
		t.Errorf("reply %q", msg)
	}
	if _, ok := e.app.savings.get(alice); ok {
		t.Error("prompt still pending")
	}

	// A wish deleted meanwhile ends the prompt too.
	w2 := e.addPricedWish(t, "Кресло", nil, domain.StatusProgress)
	e.handle(callbackUpdate(alice, callback{op: opWishSave, id: int64(w2.ID)}.String(), cardMessage(alice, 61)))
	p = e.savingPrompt(t)
	if err := e.svc.wishes.Delete(context.Background(), bob, w2.ID); err != nil {
		t.Fatal(err)
	}
	e.handle(replyUpdate(alice, "5000", p.promptID))
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "удалили") {
		t.Errorf("reply %q", msg)
	}
	if _, ok := e.app.savings.get(alice); ok {
		t.Error("prompt still pending")
	}
}
