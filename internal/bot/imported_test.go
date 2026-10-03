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

// Import answers with f.importer; without one, Instagram is "down".
func (f *fakeRecipes) Import(ctx context.Context, actor domain.UserID, in service.ImportInput) (domain.Recipe, service.ImportReport, error) {
	f.mu.Lock()
	f.inputs = append(f.inputs, in)
	fn := f.importer
	f.mu.Unlock()
	if fn == nil {
		return domain.Recipe{}, service.ImportReport{}, domain.ErrExternalUnavailable
	}
	return fn(ctx, actor, in)
}

func (f *fakeRecipes) importInputs() []service.ImportInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.inputs)
}

func pendingImportDue(n *notifier, id domain.RecipeID) (time.Time, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	p, ok := n.imports[id]
	if !ok {
		return time.Time{}, false
	}
	return p.due, true
}

// importedRecipe is a recipe as an Instagram import leaves it.
func (e *notifyEnv) importedRecipe(t *testing.T) domain.Recipe {
	t.Helper()
	link := "https://www.instagram.com/reel/DItfAhKCJ3h/"
	servings := 4
	r, err := e.svc.recipes.Create(context.Background(), alice, domain.RecipeDraft{
		Title: "Сырники",
		Link:  &link,
		Body:  "1. Смешать\n2. Обжарить",
		Ingredients: []domain.Ingredient{
			{Name: "Творог", Quantity: quantity(t, "400", "г")},
			{Name: "Яйцо", Quantity: quantity(t, "1", "шт")},
		},
		Servings: &servings,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *notifyEnv) sentTo(user domain.UserID) []string {
	var out []string
	for _, c := range e.api.all() {
		switch p := c.params.(type) {
		case *tg.SendMessageParams:
			if chatOf(p.ChatID) == int64(user) {
				out = append(out, p.Text)
			}
		case *tg.SendPhotoParams:
			if chatOf(p.ChatID) == int64(user) {
				out = append(out, p.Caption)
			}
		}
	}
	return out
}

func TestNotifierImportWaitsForTheReviewWindow(t *testing.T) {
	e := newNotifyEnv(t, 0)
	clk := newClock()
	e.n.now, e.n.importQuiet, e.n.quiet = clk.Now, time.Hour, time.Hour
	rec := e.importedRecipe(t)

	e.n.RecipeImported(context.Background(), recipients(), rec)
	first, ok := pendingImportDue(e.n, rec.ID)
	if !ok || !first.Equal(clk.Now().Add(time.Hour)) {
		t.Fatalf("import notice due %v, %v", first, ok)
	}

	// The importer fixes the parse: the window restarts and the edit is
	// not announced on its own.
	clk.Advance(50 * time.Minute)
	e.n.RecipeUpdated(context.Background(), recipients(), rec)
	second, _ := pendingImportDue(e.n, rec.ID)
	if got := second.Sub(first); got != 50*time.Minute {
		t.Errorf("the review window moved by %v, want 50m", got)
	}
	if _, ok := pendingDue(e.n, rec.ID, alice); ok {
		t.Error("the importer's edit became an edit notice")
	}
	// The partner's own edit is a separate burst, told to the importer.
	e.n.RecipeUpdated(context.Background(), fromBob(), rec)
	if _, ok := pendingDue(e.n, rec.ID, bob); !ok {
		t.Error("the partner's edit was swallowed by the import window")
	}
	if n := len(e.api.all()); n != 0 {
		t.Fatalf("%d calls before the window ended", n)
	}

	// Shutdown delivers the pending notice right away, once.
	e.n.drain()
	toBob := e.sentTo(bob)
	if len(toBob) != 1 {
		t.Fatalf("partner got %q, want only the import notice", toBob)
	}
	want := "📥 <b>Дима</b> добавил(а) рецепт из Instagram: «Сырники»\n2 ингредиента · 2 шага · 4 порции"
	if !strings.HasPrefix(toBob[0], want) {
		t.Errorf("notice %q, want prefix %q", toBob[0], want)
	}
	if toAlice := e.sentTo(alice); len(toAlice) != 1 || !strings.Contains(toAlice[0], "изменения в рецепте") {
		t.Errorf("importer got %q, want the partner's edit notice", toAlice)
	}
	if _, ok := pendingImportDue(e.n, rec.ID); ok {
		t.Error("import notice still pending after delivery")
	}
}

func TestNotifierImportDeliveredWithCover(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.importQuiet = 20 * time.Millisecond
	rec := e.importedRecipe(t)
	rec.Images = []domain.Image{{ID: 9}}
	e.svc.recipes.put(rec)
	e.svc.images.data[9] = []byte("jpeg")

	e.n.RecipeImported(context.Background(), recipients(), domain.Recipe{ID: rec.ID, Title: "черновое"})
	waitFor(t, "the import notice", func() bool { return e.api.count("SendPhoto") > 0 })
	e.n.drain()

	photos := e.api.of("SendPhoto")
	if len(photos) != 1 || e.api.count("SendMessage") != 0 {
		t.Fatalf("%d photos, %d messages", len(photos), e.api.count("SendMessage"))
	}
	p := photos[0].(*tg.SendPhotoParams)
	if p.ChatID != int64(bob) || !strings.HasPrefix(p.Caption, "📥 <b>Дима</b> добавил(а) рецепт из Instagram: «Сырники»") {
		t.Errorf("sent %v %q (the notice must show the recipe as it is now)", p.ChatID, p.Caption)
	}
	if urls := webAppURLs(p.ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?recipe=1"}) {
		t.Errorf("open button %v", urls)
	}
}

func TestNotifierImportOfTextIsNotFromInstagram(t *testing.T) {
	e := newNotifyEnv(t, 0)
	rec := e.addRecipe(t, "Борщ")
	e.n.RecipeImported(context.Background(), recipients(), rec)
	e.n.drain()
	if texts := e.sentTexts(); len(texts) != 1 || texts[0] != "📥 <b>Дима</b> добавил(а) рецепт: «Борщ»" {
		t.Errorf("sent %q", texts)
	}
}

func TestNotifierImportDroppedWhenDeleted(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.importQuiet = time.Hour
	gone := e.importedRecipe(t)
	forgotten := e.importedRecipe(t)
	e.n.RecipeImported(context.Background(), recipients(), gone)
	e.n.RecipeImported(context.Background(), recipients(), forgotten)

	// Deleted in the Mini App: found gone when the notice reloads it.
	if err := e.svc.recipes.Delete(context.Background(), alice, gone.ID); err != nil {
		t.Fatal(err)
	}
	// Deleted from the chat: dropped at once.
	if !e.n.forgetImport(forgotten.ID) {
		t.Fatal("pending import notice not found")
	}
	if e.n.forgetImport(forgotten.ID) {
		t.Error("an import notice was dropped twice")
	}
	waitFor(t, "the dropped notice to stop waiting", func() bool {
		e.n.mu.Lock()
		defer e.n.mu.Unlock()
		_, ok := e.n.imports[forgotten.ID]
		return !ok
	})
	e.n.drain()
	if calls := e.api.all(); len(calls) != 0 {
		t.Errorf("announced deleted imports: %+v", calls)
	}
}

func TestNotifierDropsImportsAfterDrain(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.drain()
	e.n.RecipeImported(context.Background(), recipients(), e.addRecipe(t, "Паста"))
	e.n.drain()
	if len(e.api.all()) != 0 || len(e.n.imports) != 0 {
		t.Error("import announced or kept after shutdown")
	}
}
