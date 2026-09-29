package bot

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// waitFor polls cond until it holds or a generous deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (e *notifyEnv) addRecipe(t *testing.T, title string) domain.Recipe {
	t.Helper()
	r, err := e.svc.recipes.Create(context.Background(), alice, domain.RecipeDraft{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *notifyEnv) sentTexts() []string {
	var out []string
	for _, p := range e.api.of("SendMessage") {
		out = append(out, p.(*tg.SendMessageParams).Text)
	}
	return out
}

// fromBob is the reverse of recipients(): Аня acts, Дима is told.
func fromBob() service.Recipients {
	return service.Recipients{
		Actor: domain.User{ID: bob, FirstName: "Аня"},
		To:    []domain.User{{ID: alice, FirstName: "Дима", HasChat: true}},
	}
}

func pendingDue(n *notifier, id domain.RecipeID, actor domain.UserID) (time.Time, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	p, ok := n.updates[updateKey{recipe: id, actor: actor}]
	if !ok {
		return time.Time{}, false
	}
	return p.due, true
}

func TestNotifierCoalescesRecipeEdits(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.quiet = 40 * time.Millisecond
	rec := e.addRecipe(t, "Паста карбонара")

	// One edit in the Mini App: the fields, then two photos.
	for range 3 {
		e.n.RecipeUpdated(context.Background(), recipients(), rec)
	}
	waitFor(t, "the edit notice", func() bool { return e.api.count("SendMessage") > 0 })
	time.Sleep(4 * e.n.quiet)

	sent := e.api.of("SendMessage")
	if len(sent) != 1 {
		t.Fatalf("%d messages for one burst of edits, want 1", len(sent))
	}
	p := sent[0].(*tg.SendMessageParams)
	if want := "✏️ <b>Дима</b> · изменения в рецепте\n«Паста карбонара»"; p.Text != want || p.ChatID != int64(bob) {
		t.Errorf("sent %v %q, want %q", p.ChatID, p.Text, want)
	}
	if urls := webAppURLs(p.ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?recipe=1"}) {
		t.Errorf("open button %v", urls)
	}

	// A later edit is a new burst with its own notice.
	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	waitFor(t, "the second edit notice", func() bool { return e.api.count("SendMessage") == 2 })
	e.n.drain()
	if n := e.api.count("SendMessage"); n != 2 {
		t.Errorf("%d messages after two bursts", n)
	}
}

func TestNotifierQuietPeriodRestartsOnEveryEdit(t *testing.T) {
	e := newNotifyEnv(t, 0)
	clk := newClock()
	e.n.now, e.n.quiet = clk.Now, time.Hour
	rec := e.addRecipe(t, "Борщ")

	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	first, ok := pendingDue(e.n, rec.ID, alice)
	if !ok || !first.Equal(clk.Now().Add(time.Hour)) {
		t.Fatalf("first edit due %v, %v", first, ok)
	}
	clk.Advance(50 * time.Minute)
	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	second, _ := pendingDue(e.n, rec.ID, alice)
	if got := second.Sub(first); got != 50*time.Minute {
		t.Errorf("the quiet period moved by %v, want 50m (it restarts after the last edit)", got)
	}
	if e.api.count("SendMessage") != 0 {
		t.Fatal("announced before the recipe stayed unchanged for the quiet period")
	}

	// Shutdown announces the pending burst right away, once.
	e.n.drain()
	if n := e.api.count("SendMessage"); n != 1 {
		t.Errorf("%d messages on shutdown, want 1", n)
	}
	if _, ok := pendingDue(e.n, rec.ID, alice); ok {
		t.Error("burst still pending after delivery")
	}
}

func TestNotifierEditRightAfterCreateOnlyAnnouncesCreation(t *testing.T) {
	e := newNotifyEnv(t, 60*time.Millisecond)
	e.n.quiet = 10 * time.Millisecond
	rec := e.addRecipe(t, "Паста")

	e.n.RecipeCreated(context.Background(), recipients(), rec)
	// The creation flow uploads photos right after creating the recipe.
	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	waitFor(t, "the creation notice", func() bool { return e.api.count("SendMessage") > 0 })
	time.Sleep(10 * e.n.quiet)

	texts := e.sentTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], "новый рецепт") {
		t.Fatalf("sent %q, want only the creation notice", texts)
	}

	// Once the creation notice is out, edits are announced again.
	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	waitFor(t, "the edit notice", func() bool { return e.api.count("SendMessage") == 2 })
	e.n.drain()
	if texts := e.sentTexts(); !strings.Contains(texts[1], "изменения в рецепте") {
		t.Errorf("second notice %q", texts[1])
	}
}

func TestNotifierSkipsEditsOfDeletedRecipe(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.quiet = time.Hour
	rec := e.addRecipe(t, "Паста")
	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	e.n.RecipeUpdated(context.Background(), recipients(), domain.Recipe{ID: 404, Title: "Никогда не было"})
	if err := e.svc.recipes.Delete(context.Background(), alice, rec.ID); err != nil {
		t.Fatal(err)
	}
	e.n.drain() // flushes both bursts
	if calls := e.api.all(); len(calls) != 0 {
		t.Errorf("announced edits of deleted recipes: %+v", calls)
	}
}

func TestNotifierEditsByBothPartnersAreSeparate(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.quiet = time.Hour
	rec := e.addRecipe(t, "Паста")
	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	e.n.RecipeUpdated(context.Background(), fromBob(), rec)
	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	e.n.drain()
	var chats []int64
	for _, p := range e.api.of("SendMessage") {
		chats = append(chats, chatOf(p.(*tg.SendMessageParams).ChatID))
	}
	slices.Sort(chats)
	if !slices.Equal(chats, []int64{int64(alice), int64(bob)}) {
		t.Errorf("notices went to %v, want one to each partner", chats)
	}
}

func TestNotifierDropsEditsAfterDrain(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.drain()
	e.n.RecipeUpdated(context.Background(), recipients(), e.addRecipe(t, "Паста"))
	e.n.drain()
	if len(e.api.all()) != 0 || len(e.n.updates) != 0 {
		t.Error("edit announced or kept after shutdown")
	}
}

func TestNotifierRecipeCookedOffersRating(t *testing.T) {
	e := newNotifyEnv(t, time.Hour) // cooking is announced right away
	rec := e.addRecipe(t, "Паста карбонара")
	cook := domain.Cook{ID: 17, RecipeID: rec.ID, CookedBy: alice,
		Ratings: []domain.Rating{{UserID: alice, Stars: 5}}}
	e.n.RecipeCooked(context.Background(), recipients(), rec, cook)
	e.n.RecipeCooked(context.Background(), recipients(), rec, domain.Cook{ID: 18, RecipeID: rec.ID, CookedBy: alice})
	e.n.drain()

	sent := e.api.of("SendMessage")
	if len(sent) != 2 {
		t.Fatalf("%d messages", len(sent))
	}
	// Delivery order is not defined; tell the notices apart by their cook.
	rated, plain := sent[0].(*tg.SendMessageParams), sent[1].(*tg.SendMessageParams)
	if !strings.Contains(strings.Join(callbackData(rated.ReplyMarkup), " "), ":17:") {
		rated, plain = plain, rated
	}
	if want := "🍳 <b>Дима</b> · приготовлено «Паста карбонара» ⭐⭐⭐⭐⭐\nОцените тоже:"; rated.Text != want {
		t.Errorf("text %q, want %q", rated.Text, want)
	}
	if want := "🍳 <b>Дима</b> · приготовлено «Паста карбонара»\nОцените тоже:"; plain.Text != want {
		t.Errorf("text without the author's rating %q", plain.Text)
	}
	var want []string
	for s := 1; s <= maxStars; s++ {
		want = append(want, callback{op: opRecipeRate, id: int64(rec.ID), cook: 17, stars: s}.String())
	}
	if got := callbackData(rated.ReplyMarkup); !slices.Equal(got, want) {
		t.Errorf("rating buttons %v, want %v", got, want)
	}
	if urls := webAppURLs(rated.ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?recipe=1"}) {
		t.Errorf("open button %v", urls)
	}
}

func TestNotifierRecipeRatedWithComment(t *testing.T) {
	e := newNotifyEnv(t, 0)
	rec := e.addRecipe(t, "Паста карбонара")
	rating := domain.Rating{UserID: bob, Stars: 4, Comment: "<b>Идеально</b> & " + scriptInjected}
	e.n.RecipeRated(context.Background(), fromBob(), rec, domain.Cook{ID: 1}, rating)
	e.n.RecipeRated(context.Background(), fromBob(), rec, domain.Cook{ID: 1}, domain.Rating{UserID: bob, Stars: 3})
	e.n.drain()

	texts := e.sentTexts()
	slices.Sort(texts)
	if len(texts) != 2 {
		t.Fatalf("sent %q", texts)
	}
	if texts[0] != "⭐ <b>Аня</b> · оценка 3/5 — «Паста карбонара»" {
		t.Errorf("rating without a comment %q", texts[0])
	}
	want := "⭐ <b>Аня</b> · оценка 4/5 — «Паста карбонара»\n<i>&lt;b&gt;Идеально&lt;/b&gt; &amp; &lt;script&gt;"
	if !strings.HasPrefix(texts[1], want) {
		t.Errorf("rating with a comment %q, want prefix %q", texts[1], want)
	}
	assertSafeHTML(t, texts[1])
	if p := e.api.lastSent(t); p.ChatID != int64(alice) || p.ReplyMarkup != nil {
		t.Errorf("sent to %v with buttons %v", p.ChatID, p.ReplyMarkup)
	}
}

func TestNotifierWishSavedShowsProgress(t *testing.T) {
	e := newNotifyEnv(t, 0)
	price := domain.Money{Minor: 4_500_000, Currency: "RUB"}
	w, err := e.svc.wishes.Create(context.Background(), bob, domain.WishDraft{Title: "Диван", Price: &price})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := e.svc.wishes.AddSaving(ctx, bob, w.ID, domain.Money{Minor: 1_200_000, Currency: "RUB"}, ""); err != nil {
		t.Fatal(err)
	}
	s, err := e.svc.wishes.AddSaving(ctx, bob, w.ID, domain.Money{Minor: 500_000, Currency: "RUB"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// The notifier reloads the wish, so a stale total from the caller does not matter.
	e.n.WishSaved(ctx, fromBob(), w, s)
	e.n.WishSaved(ctx, fromBob(), domain.Wish{ID: 404, Title: "Удалено"}, s)
	e.n.drain()

	sent := e.api.of("SendMessage")
	if len(sent) != 1 {
		t.Fatalf("%d messages (a deleted wish must not be announced)", len(sent))
	}
	p := sent[0].(*tg.SendMessageParams)
	saved := domain.Money{Minor: 1_700_000, Currency: "RUB"}
	want := "💰 <b>Аня</b> · отложено " + s.Amount.Format() + " на «Диван»\n" +
		"Накоплено " + amountDigits(saved) + " из " + price.Format() + " (37%)"
	if p.Text != want || p.ChatID != int64(alice) || p.ReplyMarkup != nil {
		t.Errorf("sent %v %q, want %q", p.ChatID, p.Text, want)
	}
}

func TestNotifierWishSavedFullAmount(t *testing.T) {
	e := newNotifyEnv(t, 0)
	price := domain.Money{Minor: 10_000, Currency: "EUR"}
	w, _ := e.svc.wishes.Create(context.Background(), bob, domain.WishDraft{Title: "Лампа", Price: &price})
	s, err := e.svc.wishes.AddSaving(context.Background(), bob, w.ID, price, "")
	if err != nil {
		t.Fatal(err)
	}
	e.n.WishSaved(context.Background(), fromBob(), w, s)
	e.n.drain()
	if text := e.api.lastSent(t).Text; !strings.Contains(text, "(100%)") || !strings.Contains(text, "Всё накоплено") {
		t.Errorf("text %q", text)
	}
}

func TestNotifierEscapesNewNotices(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.quiet = time.Hour
	r := recipients()
	r.Actor.FirstName = "<b>Злодей</b>"
	rec, err := e.svc.recipes.Create(context.Background(), alice, domain.RecipeDraft{Title: scriptInjected})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := e.svc.wishes.Create(context.Background(), alice, domain.WishDraft{Title: scriptInjected})
	s, err := e.svc.wishes.AddSaving(context.Background(), alice, w.ID, domain.Money{Minor: 100, Currency: "EUR"}, "")
	if err != nil {
		t.Fatal(err)
	}
	e.n.RecipeUpdated(context.Background(), r, rec)
	e.n.RecipeCooked(context.Background(), r, rec, domain.Cook{ID: 1, RecipeID: rec.ID, CookedBy: alice})
	e.n.RecipeRated(context.Background(), r, rec, domain.Cook{ID: 1}, domain.Rating{UserID: alice, Stars: 2, Comment: scriptInjected})
	e.n.WishSaved(context.Background(), r, w, s)
	e.n.drain()
	texts := e.sentTexts()
	if len(texts) != 4 {
		t.Fatalf("sent %d notices", len(texts))
	}
	for _, text := range texts {
		if strings.Contains(text, "<script>") || strings.Contains(text, "<b>Злодей") {
			t.Errorf("unescaped notification %q", text)
		}
		assertSafeHTML(t, text)
	}
}
