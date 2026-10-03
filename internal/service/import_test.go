package service_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/media"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// fakeImporter stands in for internal/recipeimport. Links are recognised by
// a small pattern, results are built from the post's code, and every call
// is recorded.
type fakeImporter struct {
	mu     sync.Mutex
	parser string // of URL imports; "" = rules
	image  []byte
	err    error
	calls  []importCall
	// gate, when set, holds FromURL until it is closed; entered gets a
	// value as each call starts.
	gate    chan struct{}
	entered chan struct{}
}

type importCall struct {
	url, text string
	actor     domain.UserID
}

var postLink = regexp.MustCompile(`instagram\.com/(p|reels?|tv)/([A-Za-z0-9_-]{5,40})`)

func (f *fakeImporter) Canonical(raw string) (string, bool) {
	m := postLink.FindStringSubmatch(raw)
	if m == nil {
		return "", false
	}
	return "https://www.instagram.com/" + strings.TrimSuffix(m[1], "s") + "/" + m[2] + "/", true
}

func (f *fakeImporter) PostKey(raw string) (string, bool) {
	m := postLink.FindStringSubmatch(raw)
	if m == nil {
		return "", false
	}
	return m[2], true
}

func (f *fakeImporter) FromURL(_ context.Context, actor domain.UserID, raw string) (service.ImportResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, importCall{url: raw, actor: actor})
	gate, entered, err, parser := f.gate, f.entered, f.err, f.parser
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if gate != nil {
		<-gate
	}
	if err != nil {
		return service.ImportResult{}, err
	}
	link, _ := f.Canonical(raw)
	if parser == "" {
		parser = service.ImportParserRules
	}
	two := 2
	return service.ImportResult{
		Draft: domain.RecipeDraft{
			Title: "Маринад для шашлыка",
			Link:  &link,
			Body:  "1. Нарезать лук\n2. Смешать",
			Ingredients: []domain.Ingredient{
				{Name: "Лук", Quantity: &domain.Quantity{Hundredths: 300, Unit: domain.UnitPiece}},
				{Name: "Соль", Quantity: &domain.Quantity{Unit: domain.UnitToTaste}},
			},
			Servings: &two,
		},
		Image: f.image,
		Report: service.ImportReport{
			Source:     service.ImportSourceInstagram,
			Parser:     parser,
			Confidence: 0.95,
			Image:      f.image != nil,
			Warnings:   []string{"1 строка не распознана"},
		},
	}, nil
}

func (f *fakeImporter) FromText(_ context.Context, text string) (service.ImportResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, importCall{text: text})
	if f.err != nil {
		return service.ImportResult{}, f.err
	}
	return service.ImportResult{
		Draft:  domain.RecipeDraft{Title: "Рецепт", Body: "1. Смешать"},
		Report: service.ImportReport{Source: service.ImportSourceText, Parser: service.ImportParserLLM, Confidence: 0.4},
	}, nil
}

func (f *fakeImporter) recorded() []importCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func withImporter(im service.RecipeImporter) envOption {
	return func(d *service.Deps) { d.Importer = im }
}

func realMedia(t *testing.T) service.MediaStore {
	t.Helper()
	store, err := media.NewStore(filepath.Join(t.TempDir(), "media"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestImportFromURL(t *testing.T) {
	im := &fakeImporter{image: pngBytes(t)}
	e := newEnv(t, withImporter(im), withMedia(realMedia(t)))
	e.couple(t)
	ctx := context.Background()

	r, report, err := e.svc.Recipes.Import(ctx, dima, service.ImportInput{URL: "  Смотри https://instagram.com/reels/DItfAhKCJ3h/?igsh=abc  "})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if r.ID == 0 || r.Title != "Маринад для шашлыка" || r.AuthorID != dima || len(r.Ingredients) != 2 || r.Servings == nil || *r.Servings != 2 {
		t.Errorf("recipe = %+v", r)
	}
	if r.Link == nil || *r.Link != "https://www.instagram.com/reel/DItfAhKCJ3h/" {
		t.Errorf("link = %v, want the canonical post URL", r.Link)
	}
	if len(r.Images) != 1 || r.Images[0].Width != 40 {
		t.Errorf("cover = %+v, want the re-encoded 40×30 picture", r.Images)
	}
	want := service.ImportReport{Source: "instagram", Parser: "rules", Confidence: 0.95, Image: true, Warnings: []string{"1 строка не распознана"}}
	if !reportsEqual(report, want) {
		t.Errorf("report = %+v, want %+v", report, want)
	}
	if calls := im.recorded(); len(calls) != 1 || calls[0].url != "https://www.instagram.com/reel/DItfAhKCJ3h/" || calls[0].actor != dima {
		t.Errorf("importer calls = %+v, want one import of the canonical URL for Dima (his video quota)", calls)
	}
	stored, err := e.svc.Recipes.Get(ctx, r.ID)
	if err != nil || len(stored.Images) != 1 {
		t.Fatalf("stored = %+v, %v", stored, err)
	}

	// One notice, of its own kind, carrying the cover; the photo is not
	// announced as an edit.
	events := e.notifier.all()
	if kinds := e.notifier.kinds(); !slices.Equal(kinds, []string{"recipe_imported"}) {
		t.Fatalf("events = %v, want only recipe_imported", kinds)
	}
	if ev := events[0]; ev.recipe.ID != r.ID || len(ev.recipe.Images) != 1 || !slices.Equal(ids(ev.r.To), []domain.UserID{anya}) || ev.r.Actor.ID != dima {
		t.Errorf("notice = %+v", ev)
	}
}

func TestImportFromText(t *testing.T) {
	im := &fakeImporter{}
	e := newEnv(t, withImporter(im))
	r, report, err := e.svc.Recipes.Import(context.Background(), anya, service.ImportInput{Text: "\n Мука 200 г, яйца 2 шт \n"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if r.Title != "Рецепт" || r.Link != nil || len(r.Images) != 0 {
		t.Errorf("recipe = %+v", r)
	}
	if report.Source != "text" || report.Parser != "llm" || report.Image || report.Duplicate || report.Warnings == nil {
		t.Errorf("report = %+v", report)
	}
	if calls := im.recorded(); len(calls) != 1 || calls[0].text != "Мука 200 г, яйца 2 шт" {
		t.Errorf("importer calls = %+v, want the trimmed text", calls)
	}
}

func TestImportValidatesInput(t *testing.T) {
	im := &fakeImporter{}
	e := newEnv(t, withImporter(im))
	ctx := context.Background()
	cases := []struct {
		name  string
		in    service.ImportInput
		field string
	}{
		{"nothing", service.ImportInput{URL: " ", Text: "\n"}, "url"},
		{"both", service.ImportInput{URL: "https://www.instagram.com/p/Abcde12345/", Text: "Мука"}, "text"},
		{"long link", service.ImportInput{URL: "https://www.instagram.com/p/Abcde12345/?x=" + strings.Repeat("a", domain.MaxLinkLen)}, "url"},
		{"long text", service.ImportInput{Text: strings.Repeat("щ", service.MaxImportTextLen+1)}, "text"},
		{"not a post", service.ImportInput{URL: "https://example.com/reel/Abcde12345/"}, "url"},
		{"profile", service.ImportInput{URL: "https://www.instagram.com/v_ogorod/"}, "url"},
	}
	for _, c := range cases {
		_, _, err := e.svc.Recipes.Import(ctx, dima, c.in)
		wantValidation(t, c.name, err, c.field)
	}
	if _, _, err := e.svc.Recipes.Import(ctx, dima, service.ImportInput{Text: strings.Repeat("щ", service.MaxImportTextLen)}); err != nil {
		t.Errorf("text at the limit: %v", err)
	}
	if calls := im.recorded(); len(calls) != 1 {
		t.Errorf("invalid input reached the importer: %+v", calls)
	}
}

func TestImportReturnsTheRecipeOfAnImportedPost(t *testing.T) {
	im := &fakeImporter{image: pngBytes(t)}
	e := newEnv(t, withImporter(im), withMedia(realMedia(t)))
	e.couple(t)
	ctx := context.Background()

	first, _, err := e.svc.Recipes.Import(ctx, dima, service.ImportInput{URL: "https://www.instagram.com/reel/DItfAhKCJ3h/"})
	if err != nil {
		t.Fatal(err)
	}
	again, report, err := e.svc.Recipes.Import(ctx, anya, service.ImportInput{URL: "https://m.instagram.com/reel/DItfAhKCJ3h?igsh=xyz#c"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || !report.Duplicate || !report.Image || report.Source != "instagram" || report.Warnings == nil {
		t.Errorf("second import = %d, %+v; want recipe %d as a duplicate", again.ID, report, first.ID)
	}
	if n, _ := e.db.CountRecipes(ctx); n != 1 {
		t.Errorf("recipes = %d, want 1", n)
	}
	if calls := im.recorded(); len(calls) != 1 {
		t.Errorf("the duplicate reached Instagram: %+v", calls)
	}
	if kinds := e.notifier.kinds(); !slices.Equal(kinds, []string{"recipe_imported"}) {
		t.Errorf("events = %v, want one notice", kinds)
	}

	// A link typed by hand counts too.
	link := "https://instagram.com/p/HandTyped1/"
	manual := e.createRecipe(t, anya, domain.RecipeDraft{Title: "Свой", Link: &link})
	got, report, err := e.svc.Recipes.Import(ctx, dima, service.ImportInput{URL: "https://www.instagram.com/p/HandTyped1/?utm_source=ig"})
	if err != nil || got.ID != manual.ID || !report.Duplicate || report.Image {
		t.Errorf("import of a hand-typed link = %d, %+v, %v; want recipe %d", got.ID, report, err, manual.ID)
	}
}

// Two imports of one post at the same time make one recipe: the second
// waits for the first and gets it as a duplicate.
func TestConcurrentImportsOfOnePost(t *testing.T) {
	im := &fakeImporter{gate: make(chan struct{}), entered: make(chan struct{}, 2)}
	e := newEnv(t, withImporter(im))
	ctx := context.Background()

	type outcome struct {
		r      domain.Recipe
		report service.ImportReport
		err    error
	}
	results := make(chan outcome, 2)
	run := func(user domain.UserID, url string) {
		r, report, err := e.svc.Recipes.Import(ctx, user, service.ImportInput{URL: url})
		results <- outcome{r, report, err}
	}
	go run(dima, "https://www.instagram.com/p/SamePost1/")
	<-im.entered
	go run(anya, "https://instagram.com/p/SamePost1/?igsh=1")
	select {
	case <-im.entered:
		t.Fatal("the second import reached Instagram while the first was running")
	case <-time.After(50 * time.Millisecond):
	}
	close(im.gate)
	a, b := <-results, <-results
	if a.err != nil || b.err != nil {
		t.Fatalf("errors: %v, %v", a.err, b.err)
	}
	if a.r.ID != b.r.ID || a.report.Duplicate == b.report.Duplicate {
		t.Errorf("outcomes = %+v / %+v; want one new recipe and one duplicate of it", a, b)
	}
	if n, _ := e.db.CountRecipes(ctx); n != 1 {
		t.Errorf("recipes = %d, want 1", n)
	}

	// A waiting import gives up with its context.
	gate := make(chan struct{})
	im.mu.Lock()
	im.gate, im.entered = gate, make(chan struct{}, 1)
	im.mu.Unlock()
	go run(dima, "https://www.instagram.com/p/OtherPost/")
	<-im.entered
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, _, err := e.svc.Recipes.Import(short, anya, service.ImportInput{URL: "https://www.instagram.com/p/OtherPost/"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting import = %v, want its deadline", err)
	}
	close(gate)
	if o := <-results; o.err != nil || o.report.Duplicate {
		t.Errorf("first import = %+v", o)
	}
}

func TestImportErrorsCreateNothing(t *testing.T) {
	for _, want := range []error{
		fmt.Errorf("parse: %w", service.ErrNotARecipe),
		fmt.Errorf("parse: %w", service.ErrRecipeInVideo),
		fmt.Errorf("fetch: %w", domain.ErrExternalUnavailable),
	} {
		im := &fakeImporter{err: want}
		e := newEnv(t, withImporter(im))
		e.couple(t)
		ctx := context.Background()
		if _, _, err := e.svc.Recipes.Import(ctx, dima, service.ImportInput{URL: "https://www.instagram.com/p/Abcde12345/"}); !errors.Is(err, want) {
			t.Errorf("url import = %v, want %v", err, want)
		}
		if _, _, err := e.svc.Recipes.Import(ctx, dima, service.ImportInput{Text: "просто текст"}); !errors.Is(err, want) {
			t.Errorf("text import = %v, want %v", err, want)
		}
		if n, _ := e.db.CountRecipes(ctx); n != 0 {
			t.Errorf("recipes = %d after a failed import", n)
		}
		if kinds := e.notifier.kinds(); len(kinds) != 0 {
			t.Errorf("events = %v after a failed import", kinds)
		}
	}

	e := newEnv(t)
	if _, _, err := e.svc.Recipes.Import(context.Background(), dima, service.ImportInput{Text: "Мука"}); !errors.Is(err, domain.ErrExternalUnavailable) {
		t.Errorf("import without an importer = %v, want ErrExternalUnavailable", err)
	}
}

// A cover the media pipeline refuses is a warning: the recipe is kept.
func TestImportCoverFailureIsAWarning(t *testing.T) {
	im := &fakeImporter{image: []byte("<html>not a picture</html>")}
	e := newEnv(t, withImporter(im), withMedia(realMedia(t)))
	e.couple(t)
	r, report, err := e.svc.Recipes.Import(context.Background(), dima, service.ImportInput{URL: "https://www.instagram.com/p/Abcde12345/"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if r.ID == 0 || len(r.Images) != 0 {
		t.Errorf("recipe = %+v", r)
	}
	if report.Image || !slices.Contains(report.Warnings, "Обложку не удалось сохранить") || len(report.Warnings) != 2 {
		t.Errorf("report = %+v", report)
	}
	if kinds := e.notifier.kinds(); !slices.Equal(kinds, []string{"recipe_imported"}) {
		t.Errorf("events = %v", kinds)
	}
}

func reportsEqual(a, b service.ImportReport) bool {
	return a.Source == b.Source && a.Parser == b.Parser && a.Confidence == b.Confidence &&
		a.Image == b.Image && a.Duplicate == b.Duplicate && slices.Equal(a.Warnings, b.Warnings)
}

func TestImportFindsAPostUnderAnotherLinkForm(t *testing.T) {
	im := &fakeImporter{}
	e := newEnv(t, withImporter(im), withMedia(realMedia(t)))
	e.couple(t)
	ctx := context.Background()

	first, _, err := e.svc.Recipes.Import(ctx, dima, service.ImportInput{URL: "https://www.instagram.com/reel/DItfAhKCJ3h/"})
	if err != nil {
		t.Fatal(err)
	}
	// The same post as a /p/ link from a profile grid is a duplicate.
	again, report, err := e.svc.Recipes.Import(ctx, anya, service.ImportInput{URL: "https://instagram.com/p/DItfAhKCJ3h/"})
	if err != nil || !report.Duplicate || again.ID != first.ID {
		t.Fatalf("/p/ link of an imported reel = %d, %+v, %v; want the same recipe as a duplicate", again.ID, report, err)
	}
	// A caption pasted for that post finds it too, before any parsing.
	calls := len(im.recorded())
	again, report, err = e.svc.Recipes.Import(ctx, anya, service.ImportInput{Text: "Сырники\nТворог — 400 г", Link: "https://www.instagram.com/p/DItfAhKCJ3h/"})
	if err != nil || !report.Duplicate || again.ID != first.ID || len(im.recorded()) != calls {
		t.Fatalf("caption for an imported post = %d, %+v, %v", again.ID, report, err)
	}
}

func TestImportOfACaptionKeepsItsPostLink(t *testing.T) {
	im := &fakeImporter{}
	e := newEnv(t, withImporter(im), withMedia(realMedia(t)))
	e.couple(t)
	r, _, err := e.svc.Recipes.Import(context.Background(), dima,
		service.ImportInput{Text: "Сырники\nТворог — 400 г", Link: "  https://instagram.com/reels/DItfAhKCJ3h/?igsh=x "})
	if err != nil {
		t.Fatal(err)
	}
	if r.Link == nil || *r.Link != "https://www.instagram.com/reel/DItfAhKCJ3h/" {
		t.Errorf("link = %v, want the post's canonical link", r.Link)
	}
	// A bad link is ignored: the text still imports, without a link.
	r, _, err = e.svc.Recipes.Import(context.Background(), dima, service.ImportInput{Text: "Блины\nМука — 200 г", Link: "https://example.com/x"})
	if err != nil || r.Link != nil {
		t.Errorf("text with a foreign link = %v, %v; want no link", r.Link, err)
	}
}

func TestImportsAreThrottledPerUser(t *testing.T) {
	im := &fakeImporter{}
	e := newEnv(t, withImporter(im), withMedia(realMedia(t)))
	e.couple(t)
	ctx := context.Background()
	text := func(i int) service.ImportInput {
		return service.ImportInput{Text: fmt.Sprintf("Рецепт %d\nМука — 200 г", i)}
	}
	for i := range 5 {
		if _, _, err := e.svc.Recipes.Import(ctx, dima, text(i)); err != nil {
			t.Fatalf("import %d within the burst: %v", i, err)
		}
	}
	if _, _, err := e.svc.Recipes.Import(ctx, dima, text(5)); !errors.Is(err, service.ErrTooManyImports) {
		t.Fatalf("import over the burst = %v, want ErrTooManyImports", err)
	}
	// The limit is per user.
	if _, _, err := e.svc.Recipes.Import(ctx, anya, text(6)); err != nil {
		t.Fatalf("the partner's import: %v", err)
	}
	// A duplicate costs nothing.
	if _, _, err := e.svc.Recipes.Import(ctx, anya, service.ImportInput{URL: "https://www.instagram.com/reel/DItfAhKCJ3h/"}); err != nil {
		t.Fatal(err)
	}
	if _, r, err := e.svc.Recipes.Import(ctx, dima, service.ImportInput{URL: "https://www.instagram.com/p/DItfAhKCJ3h/"}); err != nil || !r.Duplicate {
		t.Fatalf("a duplicate over the limit = %+v, %v; want the duplicate", r, err)
	}
}
