package bot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func (e *testEnv) addWish(t *testing.T, title string, status domain.Status) domain.Wish {
	t.Helper()
	w, err := e.svc.wishes.Create(context.Background(), alice, domain.WishDraft{Title: title})
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

func TestWishListTabsAndPaging(t *testing.T) {
	e := newTestEnv(t)
	for i := range 10 {
		e.addWish(t, fmt.Sprintf("Желание %d", i+1), domain.StatusWant)
	}
	e.addWish(t, "Копим", domain.StatusProgress)
	e.addWish(t, "Сбылось", domain.StatusDone)

	e.handle(commandUpdate(alice, "list"))
	sent := e.api.lastSent(t)
	texts := buttonTexts(sent.ReplyMarkup)
	for _, want := range []string{selectedMark + "💭 Хотим 10", "⏳ Копим 1", "✨ Сбылось 1", "1/2", "▶️", "Желание 10"} {
		if !slices.Contains(texts, want) {
			t.Errorf("list lacks button %q: %v", want, texts)
		}
	}
	if slices.Contains(texts, "Желание 2") {
		t.Error("first page shows items of the second page")
	}
	next := callback{op: opWishList, status: domain.StatusWant, page: 1}.String()
	if !slices.Contains(callbackData(sent.ReplyMarkup), next) {
		t.Fatalf("no next-page button %q", next)
	}

	e.api.reset()
	e.handle(callbackUpdate(alice, next, cardMessage(alice, 50)))
	edit := e.api.lastEdit(t)
	if edit.MessageID != 50 || e.api.count("SendMessage") != 0 {
		t.Error("paging did not edit the list in place")
	}
	if texts := buttonTexts(edit.ReplyMarkup); !slices.Contains(texts, "Желание 1") || !slices.Contains(texts, "◀️") {
		t.Errorf("second page %v", texts)
	}

	// A page beyond the end (the list shrank) is clamped.
	e.handle(callbackUpdate(alice, callback{op: opWishList, status: domain.StatusDone, page: 7}.String(), cardMessage(alice, 50)))
	if texts := buttonTexts(e.api.lastEdit(t).ReplyMarkup); !slices.Contains(texts, "Сбылось") {
		t.Errorf("clamped page %v", texts)
	}
}

func TestWishCardWithCoverAndStatusChange(t *testing.T) {
	e := newTestEnv(t)
	w := e.addWish(t, "Поездка в Токио", domain.StatusWant)
	if _, err := e.svc.wishes.AddImage(context.Background(), alice, w.ID, strings.NewReader("jpeg")); err != nil {
		t.Fatal(err)
	}
	e.svc.images.data[1] = []byte("jpeg")
	e.svc.users.items[alice] = domain.User{ID: alice, FirstName: "Дима"}

	e.handle(callbackUpdate(alice, callback{op: opWishOpen, id: int64(w.ID)}.String(), cardMessage(alice, 50)))
	photos := e.api.of("SendPhoto")
	if len(photos) != 1 {
		t.Fatalf("%d photos sent", len(photos))
	}
	p := photos[0].(*tg.SendPhotoParams)
	if !strings.Contains(p.Caption, "<b>Поездка в Токио</b>") || !strings.Contains(p.Caption, "Дима · 27.09.2026") {
		t.Errorf("caption %q", p.Caption)
	}
	if urls := webAppURLs(p.ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?wish=1"}) {
		t.Errorf("open button %v", urls)
	}
	if _, ok := p.Photo.(*models.InputFileUpload); !ok {
		t.Errorf("cover not uploaded: %T", p.Photo)
	}

	// Marking it done edits the caption and celebrates.
	card := &models.Message{ID: 60, Chat: privateChat(alice), Photo: []models.PhotoSize{{FileID: "x"}}}
	e.api.reset()
	e.handle(callbackUpdate(alice, callback{op: opWishStatus, id: int64(w.ID), status: domain.StatusDone}.String(), card))
	captions := e.api.of("EditMessageCaption")
	if len(captions) != 1 || !strings.Contains(captions[0].(*tg.EditMessageCaptionParams).Caption, "✨ Сбылось") {
		t.Fatalf("caption edits %+v", captions)
	}
	if !slices.Contains(buttonTexts(captions[0].(*tg.EditMessageCaptionParams).ReplyMarkup), selectedMark+"✨ Сбылось") {
		t.Error("current status not marked")
	}
	if msg := e.api.lastSent(t); !strings.Contains(msg.Text, "Ура! Мечта сбылась") || msg.ReplyParameters.MessageID != 60 {
		t.Errorf("celebration %+v", msg)
	}

	// Pressing «Сбылось» again does not celebrate twice.
	e.api.reset()
	e.handle(callbackUpdate(alice, callback{op: opWishStatus, id: int64(w.ID), status: domain.StatusDone}.String(), card))
	if e.api.count("SendMessage") != 0 {
		t.Error("celebrated a wish that was already fulfilled")
	}
}

func TestWishDeleteNeedsConfirmation(t *testing.T) {
	e := newTestEnv(t)
	w := e.addWish(t, "Лампа", domain.StatusWant)
	card := cardMessage(alice, 70)

	e.handle(callbackUpdate(alice, callback{op: opWishAskDelete, id: int64(w.ID)}.String(), card))
	kb := e.api.of("EditMessageReplyMarkup")
	if len(kb) != 1 || !slices.Contains(buttonTexts(kb[0].(*tg.EditMessageReplyMarkupParams).ReplyMarkup), "Да, удалить") {
		t.Fatalf("no confirmation: %+v", kb)
	}
	if _, err := e.svc.wishes.Get(context.Background(), w.ID); err != nil {
		t.Fatal("deleted before confirmation")
	}

	e.handle(callbackUpdate(alice, callback{op: opWishKeep, id: int64(w.ID)}.String(), card))
	if _, err := e.svc.wishes.Get(context.Background(), w.ID); err != nil {
		t.Fatal("«Отмена» deleted the wish")
	}

	e.api.reset()
	e.handle(callbackUpdate(alice, callback{op: opWishDelete, id: int64(w.ID)}.String(), card))
	if _, err := e.svc.wishes.Get(context.Background(), w.ID); err == nil {
		t.Fatal("wish not deleted")
	}
	if e.api.count("DeleteMessage") != 1 {
		t.Error("card not removed")
	}
	if got := e.api.answers(); !slices.Equal(got, []string{"Удалено 🗑"}) {
		t.Errorf("answers %v", got)
	}
}

func TestEveryCallbackIsAnswered(t *testing.T) {
	e := newTestEnv(t)
	payloads := []string{
		"garbage", "x", "w:o:404", "w:s:404:done", "w:d:404", "w:y:404", "w:n:404",
		"r:o:404", "r:y:404", "k:404", "l:w:want:0", "l:r:0", "d:99:ok",
	}
	for _, data := range payloads {
		e.api.reset()
		e.handle(callbackUpdate(alice, data, cardMessage(alice, 1)))
		if n := e.api.count("AnswerCallbackQuery"); n != 1 {
			t.Errorf("%q answered %d times", data, n)
		}
	}
	e.api.reset()
	e.handle(callbackUpdate(alice, "garbage", nil))
	if got := e.api.answers(); !slices.Equal(got, []string{"Кнопка устарела"}) {
		t.Errorf("garbage answered %v", got)
	}
}

func TestCookAndRecipes(t *testing.T) {
	e := newTestEnv(t)
	e.handle(commandUpdate(alice, "cook"))
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "Пока нет ни одного рецепта") {
		t.Errorf("empty /cook reply %q", msg)
	}

	for _, title := range []string{"Борщ", "Паста"} {
		if _, err := e.svc.recipes.Create(context.Background(), alice, domain.RecipeDraft{Title: title, Body: "Готовить с любовью"}); err != nil {
			t.Fatal(err)
		}
	}
	e.svc.recipes.random = []domain.RecipeID{1, 1, 2}
	e.api.reset()
	e.handle(commandUpdate(alice, "cook"))
	card := e.api.lastSent(t)
	if !strings.Contains(card.Text, "Борщ") {
		t.Fatalf("/cook card %q", card.Text)
	}
	again := callback{op: opCookAgain, id: 1}.String()
	if !slices.Contains(callbackData(card.ReplyMarkup), again) {
		t.Fatalf("no reroll button: %v", callbackData(card.ReplyMarkup))
	}

	e.api.reset()
	e.handle(callbackUpdate(alice, again, cardMessage(alice, 80)))
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "Паста") {
		t.Errorf("reroll repeated the same recipe: %q", msg)
	}
	oldKB := e.api.of("EditMessageReplyMarkup")
	if len(oldKB) != 1 || slices.Contains(callbackData(oldKB[0].(*tg.EditMessageReplyMarkupParams).ReplyMarkup), again) {
		t.Error("previous card kept its reroll button")
	}

	e.api.reset()
	e.handle(commandUpdate(alice, "recipes"))
	list := e.api.lastSent(t)
	if !strings.Contains(list.Text, "2 рецепта") || !slices.Contains(buttonTexts(list.ReplyMarkup), "🍳 Паста") {
		t.Errorf("recipe list %q %v", list.Text, buttonTexts(list.ReplyMarkup))
	}
}

func TestStartHelpStats(t *testing.T) {
	e := newTestEnv(t)
	e.handle(commandUpdate(alice, "start"))
	start := e.api.lastSent(t)
	if !strings.Contains(start.Text, "Привет, Дима") || !slices.Equal(webAppURLs(start.ReplyMarkup), []string{testWebApp}) {
		t.Errorf("/start %q %v", start.Text, webAppURLs(start.ReplyMarkup))
	}
	e.handle(commandUpdate(alice, "help"))
	if !strings.Contains(e.api.lastSent(t).Text, "Как пользоваться") {
		t.Error("/help")
	}
	e.handle(commandUpdate(alice, "stats"))
	if !strings.Contains(e.api.lastSent(t).Text, "Статистика") {
		t.Error("/stats")
	}
	e.handle(commandUpdate(alice, "nope"))
	if !strings.Contains(e.api.lastSent(t).Text, "не знаю") {
		t.Error("unknown command")
	}

	// Without a Mini App URL there is no web_app button at all.
	e.app.web.set("")
	e.handle(commandUpdate(alice, "start"))
	if e.api.lastSent(t).ReplyMarkup != nil {
		t.Error("web_app button without a URL")
	}
}

func TestMessageFallbacks(t *testing.T) {
	e := newTestEnv(t)
	e.addWish(t, "Лампа", domain.StatusWant)

	// A list requested from a photo card cannot be edited into text: it is
	// sent as a new message.
	photoCard := &models.Message{ID: 90, Chat: privateChat(alice), Photo: []models.PhotoSize{{FileID: "x"}}}
	e.handle(callbackUpdate(alice, callback{op: opWishList, status: domain.StatusWant}.String(), photoCard))
	if e.api.count("EditMessageText") != 0 || e.api.count("SendMessage") != 1 {
		t.Errorf("photo message: %+v", e.api.all())
	}

	// A card that cannot be deleted any more is marked as deleted instead.
	e.api.reset()
	e.api.fail["DeleteMessage"] = fmt.Errorf("%w, Bad Request: message can't be deleted", tg.ErrorBadRequest)
	e.handle(callbackUpdate(alice, callback{op: opWishDelete, id: 1}.String(), cardMessage(alice, 91)))
	if edit := e.api.lastEdit(t); edit.MessageID != 91 || !strings.Contains(edit.Text, "Удалено") {
		t.Errorf("fallback edit %+v", edit)
	}
}

func TestRecipeDeleteAndKeep(t *testing.T) {
	e := newTestEnv(t)
	r, err := e.svc.recipes.Create(context.Background(), alice, domain.RecipeDraft{Title: "Борщ"})
	if err != nil {
		t.Fatal(err)
	}
	card := cardMessage(alice, 95)
	e.handle(callbackUpdate(alice, callback{op: opRecipeAskDelete, id: int64(r.ID)}.String(), card))
	e.handle(callbackUpdate(alice, callback{op: opRecipeKeep, id: int64(r.ID)}.String(), card))
	kbs := e.api.of("EditMessageReplyMarkup")
	if len(kbs) != 2 || !slices.Contains(buttonTexts(kbs[1].(*tg.EditMessageReplyMarkupParams).ReplyMarkup), "🗑 Удалить") {
		t.Fatalf("keep did not restore the card buttons: %+v", kbs)
	}
	e.handle(callbackUpdate(alice, callback{op: opRecipeDelete, id: int64(r.ID)}.String(), card))
	if _, err := e.svc.recipes.Get(context.Background(), r.ID); err == nil {
		t.Error("recipe not deleted")
	}
}

func TestUserErrorMapping(t *testing.T) {
	e := newTestEnv(t)
	tests := map[error]string{
		&domain.ValidationError{Field: "title", Message: "название обязательно"}: "Название обязательно",
		fmt.Errorf("wrap: %w", domain.ErrNotFound):                               "Не нашёл",
		domain.ErrLimitExceeded:                              "лимит",
		domain.ErrImageTooLarge:                              "слишком большое",
		domain.ErrImageUnsupported:                           "JPEG, PNG или WebP",
		domain.ErrConflict:                                   "уже есть",
		fmt.Errorf("db: disk I/O error at /data/dreamer.db"): "Что-то пошло не так",
	}
	for err, want := range tests {
		got := e.app.userError(err, "test")
		if !strings.Contains(got, want) {
			t.Errorf("userError(%v) = %q, want %q", err, got, want)
		}
		if strings.Contains(got, "/data") {
			t.Errorf("internal detail leaked to the user: %q", got)
		}
	}
	if !strings.Contains(e.logs.String(), "disk I/O") {
		t.Error("unexpected error not logged")
	}
}
