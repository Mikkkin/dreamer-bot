package bot

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// Default tag IDs of the fakes (see newFakeRecipeTags).
const (
	tagItalian = domain.RecipeTagID(3)
	tagDinner  = domain.RecipeTagID(9)
	tagFirst   = domain.RecipeTagID(10)
)

func quantity(t *testing.T, amount, unit string) *domain.Quantity {
	t.Helper()
	q, err := domain.ParseQuantity(amount, unit)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func (e *testEnv) addRecipe(t *testing.T, d domain.RecipeDraft) domain.Recipe {
	t.Helper()
	r, err := e.svc.recipes.Create(context.Background(), alice, d)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func carbonara(t *testing.T) domain.RecipeDraft {
	cuisine := tagItalian
	return domain.RecipeDraft{
		Title:     "Паста карбонара",
		Body:      "Сварить пасту",
		CuisineID: &cuisine,
		CourseIDs: []domain.RecipeTagID{tagDinner, tagFirst},
		Ingredients: []domain.Ingredient{
			{Name: "Спагетти", Quantity: quantity(t, "320", "г")},
			{Name: "Яйца", Quantity: quantity(t, "4", "шт")},
			{Name: "Соль", Quantity: quantity(t, "", "по вкусу")},
		},
		Nutrition: &domain.Nutrition{KcalPer100: 1500, ProteinPer100: 125, FatPer100: 60, CarbsPer100: 104, WeightGrams: 800, Servings: 4},
	}
}

func (e *testEnv) lastKeyboardEdit(t *testing.T) *tg.EditMessageReplyMarkupParams {
	t.Helper()
	kbs := e.api.of("EditMessageReplyMarkup")
	if len(kbs) == 0 {
		t.Fatal("no EditMessageReplyMarkup call")
	}
	return kbs[len(kbs)-1].(*tg.EditMessageReplyMarkupParams)
}

func (e *testEnv) lastAnswer(t *testing.T) string {
	t.Helper()
	answers := e.api.answers()
	if len(answers) == 0 {
		t.Fatal("callback not answered")
	}
	return answers[len(answers)-1]
}

func TestRecipeCardShowsTagsRatingAndNutrition(t *testing.T) {
	e := newTestEnv(t)
	rec := e.addRecipe(t, carbonara(t))
	rec.Cooking = domain.CookingSummary{Count: 3, RatingSum: 9, RatingCount: 2}
	e.svc.recipes.put(rec)

	e.handle(callbackUpdate(alice, callback{op: opRecipeOpen, id: int64(rec.ID)}.String(), cardMessage(alice, 50)))
	card := e.api.lastSent(t)
	for _, want := range []string{
		"🍳 <b>Паста карбонара</b>\n🍝 Итальянская · 🌙 Ужин · 🍲 Первое\n⭐ 4,5 · готовили 3 раза",
		"🔥 300 ккал · Б 25 · Ж 12 · У 20,8 (на порцию)",
		"<b>🧾 Ингредиенты · 3</b>\n• Спагетти — " + quantity(t, "320", "г").Format() +
			"\n• Яйца — " + quantity(t, "4", "шт").Format() + "\n• Соль — по вкусу",
	} {
		if !strings.Contains(card.Text, want) {
			t.Errorf("card lacks %q:\n%s", want, card.Text)
		}
	}
	texts := buttonTexts(card.ReplyMarkup)
	for _, want := range []string{"🍳 Приготовили", "🛒 В покупки", "📱 Открыть", "🗑 Удалить"} {
		if !slices.Contains(texts, want) {
			t.Errorf("card lacks button %q: %v", want, texts)
		}
	}

	// A caption has room for the ingredient count only.
	meta := entityMeta{author: "Дима", tags: "🍝 Итальянская"}
	caption := renderRecipe(rec, meta, time.UTC, maxCaptionLen, captionBodyExcerpt)
	if !strings.Contains(caption, "🧾 3 ингредиента") || strings.Contains(caption, "Спагетти") {
		t.Errorf("caption %q", caption)
	}

	// Without servings the КБЖУ is per 100 g; without cooking nothing is said.
	rec.Nutrition = &domain.Nutrition{KcalPer100: 1500, ProteinPer100: 125, FatPer100: 60, CarbsPer100: 104}
	rec.Cooking = domain.CookingSummary{}
	out := renderRecipe(rec, meta, time.UTC, maxMessageLen, messageBodyExcerpt)
	if !strings.Contains(out, "🔥 150 ккал · Б 12,5 · Ж 6 · У 10,4 (на 100 г)") || strings.Contains(out, "готовили") {
		t.Errorf("card %q", out)
	}
	rec.Cooking = domain.CookingSummary{Count: 1}
	if out := renderRecipe(rec, meta, time.UTC, maxMessageLen, messageBodyExcerpt); !strings.Contains(out, "🍳 Готовили 1 раз") {
		t.Errorf("cooked without a rating %q", out)
	}

	// No ingredients, no «В покупки».
	plain := e.addRecipe(t, domain.RecipeDraft{Title: "Чай"})
	if texts := buttonTexts(recipeCardKeyboard(plain, "", false)); slices.Contains(texts, "🛒 В покупки") {
		t.Errorf("shopping button without ingredients: %v", texts)
	}
}

func TestRecipeCardSkipsDeletedAndUnavailableTags(t *testing.T) {
	e := newTestEnv(t)
	rec := e.addRecipe(t, carbonara(t))
	if err := e.svc.tags.Delete(context.Background(), alice, tagDinner); err != nil {
		t.Fatal(err)
	}
	if got := e.app.tagLine(context.Background(), rec); got != "🍝 Итальянская · 🍲 Первое" {
		t.Errorf("tag line %q", got)
	}
	e.svc.tags.fail = domain.ErrConflict // storage trouble only hides the tags
	if got := e.app.tagLine(context.Background(), rec); got != "" {
		t.Errorf("tag line %q", got)
	}
}

func TestCookFlowViaCallbacks(t *testing.T) {
	e := newTestEnv(t)
	rec := e.addRecipe(t, domain.RecipeDraft{Title: "Паста"})
	id := int64(rec.ID)
	card := cardMessage(alice, 70)

	e.handle(callbackUpdate(alice, callback{op: opRecipeCookAsk, id: id}.String(), card))
	stars := e.lastKeyboardEdit(t)
	if stars.MessageID != 70 {
		t.Errorf("stars shown on message %d", stars.MessageID)
	}
	if texts := buttonTexts(stars.ReplyMarkup); !slices.Equal(texts, []string{"1⭐", "2⭐", "3⭐", "4⭐", "5⭐", "Без оценки", "← Назад"}) {
		t.Errorf("stars row %v", texts)
	}
	if !slices.Contains(callbackData(stars.ReplyMarkup), "r:c:1:0") {
		t.Errorf("no «Без оценки» payload: %v", callbackData(stars.ReplyMarkup))
	}
	if len(e.svc.recipes.cookList()) != 0 {
		t.Fatal("cooked before a choice")
	}

	// «← Назад» restores the card buttons without cooking.
	e.handle(callbackUpdate(alice, callback{op: opRecipeBack, id: id}.String(), card))
	if texts := buttonTexts(e.lastKeyboardEdit(t).ReplyMarkup); !slices.Contains(texts, "🍳 Приготовили") {
		t.Errorf("back keyboard %v", texts)
	}

	e.api.reset()
	e.handle(callbackUpdate(alice, callback{op: opRecipeCook, id: id, stars: 4}.String(), card))
	cooks := e.svc.recipes.cookList()
	if len(cooks) != 1 || cooks[0].CookedBy != alice || len(cooks[0].Ratings) != 1 || cooks[0].Ratings[0].Stars != 4 {
		t.Fatalf("cooks %+v", cooks)
	}
	if got := e.lastAnswer(t); got != "🍳 Отмечено! ⭐4" {
		t.Errorf("answer %q", got)
	}
	edit := e.api.lastEdit(t)
	if edit.MessageID != 70 || !strings.Contains(edit.Text, "⭐ 4 · готовили 1 раз") {
		t.Errorf("card not refreshed: %+v", edit)
	}
	if texts := buttonTexts(edit.ReplyMarkup); !slices.Contains(texts, "🍳 Приготовили") {
		t.Errorf("card buttons not restored: %v", texts)
	}

	// A double tap on the same card does not record a second cooking.
	e.handle(callbackUpdate(alice, callback{op: opRecipeCook, id: id, stars: 5}.String(), card))
	if n := len(e.svc.recipes.cookList()); n != 1 {
		t.Fatalf("double tap recorded %d cookings", n)
	}
	if got := e.lastAnswer(t); got != "Уже отмечено ✓" {
		t.Errorf("answer %q", got)
	}

	// Cooking again later, without a rating.
	e.clock.Advance(recentActionTTL)
	e.handle(callbackUpdate(alice, callback{op: opRecipeCook, id: id}.String(), card))
	cooks = e.svc.recipes.cookList()
	if len(cooks) != 2 || len(cooks[1].Ratings) != 0 {
		t.Fatalf("cooks %+v", cooks)
	}
	if got := e.lastAnswer(t); got != "🍳 Отмечено!" {
		t.Errorf("answer %q", got)
	}
	if !strings.Contains(e.api.lastEdit(t).Text, "⭐ 4 · готовили 2 раза") {
		t.Errorf("card %q", e.api.lastEdit(t).Text)
	}

	// A photo card is refreshed through its caption.
	photo := &models.Message{ID: 71, Chat: privateChat(alice), Photo: []models.PhotoSize{{FileID: "x"}}}
	e.handle(callbackUpdate(alice, callback{op: opRecipeCook, id: id, stars: 3}.String(), photo))
	if captions := e.api.of("EditMessageCaption"); len(captions) != 1 ||
		!strings.Contains(captions[0].(*tg.EditMessageCaptionParams).Caption, "готовили 3 раза") {
		t.Errorf("caption edits %+v", captions)
	}
}

func TestCookDeletedRecipe(t *testing.T) {
	e := newTestEnv(t)
	card := cardMessage(alice, 70)
	e.handle(callbackUpdate(alice, callback{op: opRecipeCookAsk, id: 404}.String(), card))
	e.handle(callbackUpdate(alice, callback{op: opRecipeCook, id: 404, stars: 3}.String(), cardMessage(alice, 71)))
	for _, got := range e.api.answers() {
		if !strings.Contains(got, "удалили") {
			t.Errorf("answer %q", got)
		}
	}
	if len(e.svc.recipes.cookList()) != 0 {
		t.Error("cooked a deleted recipe")
	}
	// The buttons of the deleted recipe are removed from both cards.
	if kbs := e.api.of("EditMessageReplyMarkup"); len(kbs) != 2 {
		t.Errorf("stale buttons not removed: %d edits", len(kbs))
	}
}

func TestPartnerRatesFromNotification(t *testing.T) {
	e := newTestEnv(t)
	e.svc.users.items[alice] = domain.User{ID: alice, FirstName: "Дима"}
	rec := e.addRecipe(t, domain.RecipeDraft{Title: "Паста"})
	cook, err := e.svc.recipes.Cook(context.Background(), alice, rec.ID, &service.RatingInput{Stars: 5})
	if err != nil {
		t.Fatal(err)
	}
	notice := cardMessage(bob, 90)
	rate := func(stars int) string {
		return callback{op: opRecipeRate, id: int64(rec.ID), cook: int64(cook.ID), stars: stars}.String()
	}

	e.handle(callbackUpdate(bob, rate(4), notice))
	if got := e.api.answers(); !slices.Equal(got, []string{"Спасибо! ⭐4"}) {
		t.Errorf("answers %v", got)
	}
	ratings := e.svc.recipes.cookList()[0].Ratings
	if len(ratings) != 2 || ratings[1].UserID != bob || ratings[1].Stars != 4 {
		t.Fatalf("ratings %+v", ratings)
	}
	edit := e.api.lastEdit(t)
	want := "🍳 <b>Дима</b> · приготовлено «Паста» ⭐⭐⭐⭐⭐\nВаша оценка: ⭐⭐⭐⭐ — спасибо!"
	if edit.MessageID != 90 || edit.Text != want {
		t.Errorf("notice %q, want %q", edit.Text, want)
	}
	if texts := buttonTexts(edit.ReplyMarkup); !slices.Contains(texts, selectedMark+"4⭐") || !slices.Contains(texts, "Открыть ✨") {
		t.Errorf("notice buttons %v", texts)
	}

	// Rating again replaces the rating.
	e.handle(callbackUpdate(bob, rate(5), notice))
	ratings = e.svc.recipes.cookList()[0].Ratings
	if len(ratings) != 2 || ratings[1].Stars != 5 {
		t.Errorf("re-rating %+v", ratings)
	}

	// The rating counts even when the notice is inaccessible.
	e.handle(callbackUpdate(bob, rate(3), nil))
	if ratings = e.svc.recipes.cookList()[0].Ratings; ratings[1].Stars != 3 {
		t.Errorf("rating without the message %+v", ratings)
	}

	// A cooking of another recipe (a forged payload) is not found.
	e.api.reset()
	other := callback{op: opRecipeRate, id: int64(rec.ID) + 1, cook: int64(cook.ID), stars: 1}.String()
	e.handle(callbackUpdate(bob, other, notice))
	if got := e.lastAnswer(t); !strings.Contains(got, "удалили") {
		t.Errorf("answer %q", got)
	}
	if ratings = e.svc.recipes.cookList()[0].Ratings; ratings[1].Stars != 3 {
		t.Errorf("forged rating applied: %+v", ratings)
	}
}

func TestAddRecipeToShopping(t *testing.T) {
	e := newTestEnv(t)
	rec := e.addRecipe(t, carbonara(t))
	card := cardMessage(alice, 75)
	shop := callback{op: opRecipeShop, id: int64(rec.ID)}.String()

	e.handle(callbackUpdate(alice, shop, card))
	if got := e.lastAnswer(t); got != "Добавлено в список: 3" {
		t.Errorf("answer %q", got)
	}
	items := e.svc.shopping.snapshot()
	if len(items) != 3 || items[0].Name != "Спагетти" || items[0].RecipeID == nil || *items[0].RecipeID != rec.ID {
		t.Fatalf("items %+v", items)
	}

	// A double tap does not double the amounts.
	e.handle(callbackUpdate(alice, shop, card))
	if got := e.lastAnswer(t); got != "Уже в списке ✓" {
		t.Errorf("answer %q", got)
	}
	if q := e.svc.shopping.snapshot()[0].Quantity.Format(); q != quantity(t, "320", "г").Format() || len(e.svc.shopping.fromRecipe) != 1 {
		t.Errorf("double tap added again: %s, %d calls", q, len(e.svc.shopping.fromRecipe))
	}

	// A deliberate repeat later (a double portion) merges the amounts.
	e.clock.Advance(recentActionTTL)
	e.handle(callbackUpdate(alice, shop, card))
	if q := e.svc.shopping.snapshot()[0].Quantity.Format(); q != quantity(t, "640", "г").Format() {
		t.Errorf("repeat quantity %s", q)
	}

	// A stale button of a recipe whose ingredients are gone.
	empty := e.addRecipe(t, domain.RecipeDraft{Title: "Чай"})
	e.handle(callbackUpdate(alice, callback{op: opRecipeShop, id: int64(empty.ID)}.String(), cardMessage(alice, 76)))
	if got := e.lastAnswer(t); got != "В рецепте нет ингредиентов" {
		t.Errorf("answer %q", got)
	}
	e.handle(callbackUpdate(alice, callback{op: opRecipeShop, id: 404}.String(), cardMessage(alice, 77)))
	if got := e.lastAnswer(t); !strings.Contains(got, "удалили") {
		t.Errorf("answer %q", got)
	}
}

func TestRecipeListShowsRating(t *testing.T) {
	e := newTestEnv(t)
	rec := e.addRecipe(t, domain.RecipeDraft{Title: "Паста"})
	rec.Cooking = domain.CookingSummary{Count: 2, RatingSum: 9, RatingCount: 2}
	e.svc.recipes.put(rec)
	e.handle(commandUpdate(alice, "recipes"))
	if texts := buttonTexts(e.api.lastSent(t).ReplyMarkup); !slices.Contains(texts, "🍳 Паста · ⭐ 4,5") {
		t.Errorf("list buttons %v", texts)
	}
}
