package bot

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// draftCallback builds the payload of a button on alice's current draft.
func (e *testEnv) draftCallback(t *testing.T, c callback) *models.Update {
	t.Helper()
	d, ok := e.app.drafts.get(alice)
	if !ok {
		t.Fatal("alice has no draft")
	}
	c.draft = d.id
	return callbackUpdate(alice, c.String(), cardMessage(alice, d.cardID))
}

func (e *testEnv) draft(t *testing.T) draft {
	t.Helper()
	d, ok := e.app.drafts.get(alice)
	if !ok {
		t.Fatal("alice has no draft")
	}
	return d
}

func TestQuickAddTextShowsDraftCard(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Поездка в Токио 1200€\nвесной"))

	sent := e.api.lastSent(t)
	if sent.ParseMode != models.ParseModeHTML {
		t.Error("draft card must use HTML parse mode")
	}
	price := domain.Money{Minor: 120000, Currency: "EUR"}.Format()
	for _, want := range []string{"Новое желание", "Поездка в Токио", price, "весной"} {
		if !strings.Contains(sent.Text, want) {
			t.Errorf("card text %q lacks %q", sent.Text, want)
		}
	}
	d := e.draft(t)
	if d.cardID == 0 || d.kind != kindWish || d.price == nil {
		t.Fatalf("draft %+v", d)
	}
	data := callbackData(sent.ReplyMarkup)
	for _, want := range []callback{
		{op: opDraftKind, draft: d.id, kind: kindRecipe},
		{op: opDraftField, draft: d.id, field: fieldPrice},
		{op: opDraftSave, draft: d.id},
		{op: opDraftCancel, draft: d.id},
	} {
		if !slices.Contains(data, want.String()) {
			t.Errorf("keyboard lacks %q: %v", want.String(), data)
		}
	}
	texts := buttonTexts(sent.ReplyMarkup)
	if !slices.Contains(texts, selectedMark+"✨ Желание") || !slices.Contains(texts, "💰 "+price) {
		t.Errorf("buttons %v", texts)
	}
}

func TestDraftKindToggleEditsCardInPlace(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Паста карбонара\n1. Сварить пасту"))
	e.api.reset()

	e.handle(e.draftCallback(t, callback{op: opDraftKind, kind: kindRecipe}))

	if e.api.count("SendMessage") != 0 {
		t.Error("toggling the kind sent a new message instead of editing the card")
	}
	edit := e.api.lastEdit(t)
	if !strings.Contains(edit.Text, "Новый рецепт") {
		t.Errorf("card text %q", edit.Text)
	}
	texts := buttonTexts(edit.ReplyMarkup)
	if !slices.Contains(texts, selectedMark+"🍳 Рецепт") || !slices.Contains(texts, "📝 Текст рецепта ✓") {
		t.Errorf("recipe keyboard %v", texts)
	}
	if slices.ContainsFunc(texts, func(s string) bool { return strings.HasPrefix(s, "💰") }) {
		t.Error("recipe keyboard offers a price")
	}
	if e.api.count("AnswerCallbackQuery") != 1 {
		t.Errorf("callback answered %d times", e.api.count("AnswerCallbackQuery"))
	}
	if e.draft(t).kind != kindRecipe {
		t.Error("kind not stored")
	}
}

func TestAlbumAggregatesIntoOneDraft(t *testing.T) {
	e := newTestEnv(t)
	// Telegram delivers an album as separate messages; the caption may sit
	// on any of them.
	e.handle(photoUpdate(alice, "p1", "album-1", ""))
	e.handle(photoUpdate(alice, "p2", "album-1", "Отель у моря 250€"))
	e.handle(photoUpdate(alice, "p3", "album-1", ""))

	if n := e.api.count("SendMessage"); n != 1 {
		t.Fatalf("album produced %d cards, want 1", n)
	}
	d := e.draft(t)
	if !slices.Equal(d.photos, []string{"p1", "p2", "p3"}) {
		t.Errorf("photos %v (the largest size must be picked)", d.photos)
	}
	if d.title != "Отель у моря" || d.price == nil || d.price.Minor != 25000 {
		t.Errorf("caption not merged: %+v", d)
	}
	if !strings.Contains(e.api.lastEdit(t).Text, "Фото: 3") {
		t.Error("card does not show the photo count")
	}
}

func TestPhotoJoinRules(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Лампа"))
	first := e.draft(t).id

	e.handle(photoUpdate(alice, "p1", "", ""))
	if d := e.draft(t); d.id != first || len(d.photos) != 1 {
		t.Fatal("a photo without caption did not join the recent draft")
	}

	e.clock.Advance(photoJoinWindow + time.Minute)
	e.handle(photoUpdate(alice, "p2", "", ""))
	if d := e.draft(t); d.id == first || len(d.photos) != 1 {
		t.Fatal("a photo joined a draft idle for longer than the join window")
	}

	second := e.draft(t).id
	e.handle(photoUpdate(alice, "p3", "", "Кресло"))
	if d := e.draft(t); d.id == second || d.title != "Кресло" {
		t.Fatal("a photo with a caption must start a new draft")
	}
}

func TestNewTextReplacesDraft(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Лампа"))
	old := e.draft(t)
	e.api.reset()

	e.handle(textUpdate(alice, "Кресло"))
	edit := e.api.lastEdit(t)
	if edit.MessageID != old.cardID || !strings.Contains(edit.Text, "Черновик заменён") {
		t.Errorf("old card not retired: %+v", edit)
	}
	if edit.ReplyMarkup != nil {
		t.Error("retired card keeps its buttons")
	}
	if d := e.draft(t); d.title != "Кресло" || d.id == old.id {
		t.Errorf("new draft %+v", d)
	}

	// Buttons of the replaced card are stale now.
	e.api.reset()
	stale := callback{op: opDraftSave, draft: old.id}
	e.handle(callbackUpdate(alice, stale.String(), cardMessage(alice, old.cardID)))
	if got := e.api.answers(); !slices.Equal(got, []string{"Черновик устарел"}) {
		t.Errorf("answers %v", got)
	}
	if len(e.svc.wishes.created) != 0 {
		t.Error("a stale button saved something")
	}
}

func TestAwaitingFieldFlow(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Диван"))
	e.api.reset()

	e.handle(e.draftCallback(t, callback{op: opDraftField, field: fieldPrice}))
	prompt := e.api.lastSent(t)
	if fr, ok := prompt.ReplyMarkup.(*models.ForceReply); !ok || !fr.ForceReply {
		t.Fatalf("prompt markup %#v, want ForceReply", prompt.ReplyMarkup)
	}
	if !strings.Contains(prompt.Text, "/cancel") || !strings.Contains(prompt.Text, "«-»") {
		t.Errorf("prompt %q must explain /cancel and clearing", prompt.Text)
	}
	if d := e.draft(t); d.awaiting != fieldPrice || d.promptID == 0 {
		t.Fatalf("draft not awaiting: %+v", d)
	}

	// A wrong value is explained and the draft keeps waiting.
	e.api.reset()
	e.handle(textUpdate(alice, "дорого"))
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "Не получилось распознать сумму") {
		t.Errorf("error reply %q", msg)
	}
	if e.draft(t).awaiting != fieldPrice {
		t.Fatal("draft stopped awaiting after a bad value")
	}

	e.api.reset()
	e.handle(textUpdate(alice, "15 000 ₽"))
	d := e.draft(t)
	if d.price == nil || *d.price != (domain.Money{Minor: 1500000, Currency: "RUB"}) || d.awaiting != fieldNone {
		t.Fatalf("price not filled: %+v", d)
	}
	if e.api.count("DeleteMessage") != 2 {
		t.Errorf("prompt and answer not cleaned up: %d deletes", e.api.count("DeleteMessage"))
	}
	if !strings.Contains(e.api.lastEdit(t).Text, d.price.Format()) {
		t.Error("card not updated with the price")
	}

	// «-» clears the value.
	e.handle(e.draftCallback(t, callback{op: opDraftField, field: fieldPrice}))
	e.handle(textUpdate(alice, "-"))
	if e.draft(t).price != nil {
		t.Error("price not cleared")
	}
}

func TestCancelCommandStepsBack(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Диван"))
	e.handle(e.draftCallback(t, callback{op: opDraftField, field: fieldLink}))

	e.handle(commandUpdate(alice, "cancel"))
	if d := e.draft(t); d.awaiting != fieldNone {
		t.Fatal("/cancel did not stop awaiting")
	}
	e.handle(commandUpdate(alice, "cancel"))
	if _, ok := e.app.drafts.get(alice); ok {
		t.Fatal("second /cancel did not discard the draft")
	}
	e.handle(commandUpdate(alice, "cancel"))
	if msg := e.api.lastSent(t).Text; !strings.Contains(msg, "Отменять нечего") {
		t.Errorf("reply %q", msg)
	}
}

func TestDraftTTLExpiryMakesButtonsStale(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Лампа"))
	upd := e.draftCallback(t, callback{op: opDraftHot})
	e.clock.Advance(draftTTL + time.Second)
	e.api.reset()
	e.handle(upd)
	if got := e.api.answers(); !slices.Equal(got, []string{"Черновик устарел"}) {
		t.Errorf("answers %v", got)
	}
}

func TestDraftCategoryChips(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Отель"))
	e.handle(e.draftCallback(t, callback{op: opDraftCategories}))
	edit := e.api.lastEdit(t)
	texts := buttonTexts(edit.ReplyMarkup)
	if !slices.Contains(texts, "✈️ Путешествия") || !slices.Contains(texts, selectedMark+"Без категории") || !slices.Contains(texts, "← Назад") {
		t.Fatalf("category chips %v", texts)
	}
	kb := edit.ReplyMarkup.(*models.InlineKeyboardMarkup)
	if len(kb.InlineKeyboard[0]) != 2 {
		t.Errorf("chips must come two per row, got %d", len(kb.InlineKeyboard[0]))
	}

	e.handle(e.draftCallback(t, callback{op: opDraftCategory, id: 2}))
	d := e.draft(t)
	if d.category == nil || *d.category != 2 || d.categoryLabel != "✈️ Путешествия" || d.view != viewMain {
		t.Fatalf("category not chosen: %+v", d)
	}
	if !slices.Contains(buttonTexts(e.api.lastEdit(t).ReplyMarkup), "🏷 ✈️ Путешествия") {
		t.Error("main keyboard does not show the category")
	}

	e.api.reset()
	e.handle(e.draftCallback(t, callback{op: opDraftCategory, id: 99}))
	if got := e.api.answers(); len(got) != 1 || !strings.Contains(got[0], "удалили") {
		t.Errorf("unknown category answered %v", got)
	}
}

func TestSaveWishDownloadsPhotos(t *testing.T) {
	e := newTestEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/file/bot"+testToken+"/photos/") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	defer srv.Close()
	e.api.link = srv.URL

	e.handle(textUpdate(alice, "Отель у моря 250€"))
	e.handle(photoUpdate(alice, "p1", "", ""))
	e.handle(e.draftCallback(t, callback{op: opDraftHot}))
	cardID := e.draft(t).cardID
	e.api.reset()

	e.handle(e.draftCallback(t, callback{op: opDraftSave}))

	if len(e.svc.wishes.created) != 1 {
		t.Fatalf("created %d wishes", len(e.svc.wishes.created))
	}
	got := e.svc.wishes.created[0]
	if got.Title != "Отель у моря" || !got.Hot || got.Price == nil || got.Price.Minor != 25000 {
		t.Errorf("wish draft %+v", got)
	}
	if imgs := e.svc.wishes.added[1]; len(imgs) != 1 || string(imgs[0]) != "jpeg-bytes" {
		t.Errorf("images added: %q", imgs)
	}
	if _, ok := e.app.drafts.get(alice); ok {
		t.Error("draft survived saving")
	}
	edit := e.api.lastEdit(t)
	if edit.MessageID != cardID || !strings.Contains(edit.Text, "Сохранено ✨") {
		t.Errorf("saved card %+v", edit)
	}
	if urls := webAppURLs(edit.ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?wish=1"}) {
		t.Errorf("open button %v", urls)
	}
	if got := e.api.answers(); !slices.Equal(got, []string{"Сохраняю…"}) {
		t.Errorf("answers %v", got)
	}
}

func TestSaveWithoutTitleAsksForIt(t *testing.T) {
	e := newTestEnv(t)
	e.handle(photoUpdate(alice, "p1", "", ""))
	e.api.reset()
	e.handle(e.draftCallback(t, callback{op: opDraftSave}))
	if len(e.svc.wishes.created) != 0 {
		t.Fatal("saved a wish without a title")
	}
	if d := e.draft(t); d.awaiting != fieldTitle {
		t.Errorf("draft awaiting %v, want title", d.awaiting)
	}
	if _, ok := e.api.lastSent(t).ReplyMarkup.(*models.ForceReply); !ok {
		t.Error("no title prompt")
	}
}

func TestSaveInvalidDraftAlerts(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Заметки\n"+strings.Repeat("я", domain.MaxNoteLen+1)))
	e.api.reset()
	e.handle(e.draftCallback(t, callback{op: opDraftSave}))
	answers := e.api.of("AnswerCallbackQuery")
	if len(answers) != 1 {
		t.Fatalf("%d answers", len(answers))
	}
	a := answers[0].(*tg.AnswerCallbackQueryParams)
	if !a.ShowAlert || !strings.Contains(a.Text, "Заметка слишком длинная") {
		t.Errorf("answer %+v", a)
	}
	if len(e.svc.wishes.created) != 0 {
		t.Error("invalid wish created")
	}
}

func TestSaveRecipe(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Паста карбонара\n1. Сварить пасту"))
	e.handle(e.draftCallback(t, callback{op: opDraftKind, kind: kindRecipe}))
	e.handle(e.draftCallback(t, callback{op: opDraftSave}))
	if len(e.svc.recipes.created) != 1 || e.svc.recipes.created[0].Body != "1. Сварить пасту" {
		t.Fatalf("recipes %+v", e.svc.recipes.created)
	}
	if urls := webAppURLs(e.api.lastEdit(t).ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?recipe=1"}) {
		t.Errorf("open button %v", urls)
	}
}
