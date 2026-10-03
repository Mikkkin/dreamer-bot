package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/nutrition"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

func (f fakeRecipes) Import(ctx context.Context, actor domain.UserID, in service.ImportInput) (domain.Recipe, service.ImportReport, error) {
	f.db.mu.Lock()
	fn := f.db.importFn
	f.db.mu.Unlock()
	if fn == nil {
		return domain.Recipe{}, service.ImportReport{}, errors.New("no import configured")
	}
	return fn(ctx, actor, in)
}

// importAs makes the fake import create a recipe from draft and report
// report, recording the inputs it receives.
func (h *harness) importAs(draft domain.RecipeDraft, report service.ImportReport) *[]service.ImportInput {
	var seen []service.ImportInput
	h.db.importFn = func(ctx context.Context, actor domain.UserID, in service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		seen = append(seen, in)
		r, err := fakeRecipes{h.db}.Create(ctx, actor, draft)
		return r, report, err
	}
	return &seen
}

func TestImportRecipe(t *testing.T) {
	h := newHarness(t)
	link := "https://www.instagram.com/reel/DEMOreel001/"
	four := 4
	seen := h.importAs(domain.RecipeDraft{
		Title: "Маринад для шашлыка",
		Link:  &link,
		Body:  "1. Нарезать лук",
		Ingredients: []domain.Ingredient{
			{Name: "Свинина", Quantity: &domain.Quantity{Hundredths: 100000, Unit: domain.UnitGram}},
			{Name: "Лук репчатый", Quantity: &domain.Quantity{Hundredths: 300, Unit: domain.UnitPiece}},
			{Name: "Соль", Quantity: &domain.Quantity{Unit: domain.UnitToTaste}},
		},
		Servings: &four,
	}, service.ImportReport{Source: "instagram", Parser: "rules", Confidence: 0.95, Image: true, Warnings: []string{"1 строка не распознана"}})

	rec := h.call(http.MethodPost, "/api/recipes/import", alice, map[string]string{"url": "https://instagram.com/reel/DEMOreel001/?igsh=abc"})
	expectStatus(t, rec, http.StatusCreated)
	var raw struct {
		Recipe map[string]json.RawMessage `json:"recipe"`
		Import map[string]json.RawMessage `json:"import"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Recipe) != 15 || string(raw.Recipe["title"]) != `"Маринад для шашлыка"` || string(raw.Recipe["nutrition_auto"]) == "null" {
		t.Errorf("recipe = %s", rec.Body.String())
	}
	if got := mustJSON(t, raw.Import); got != `{"confidence":0.95,"duplicate":false,"image":true,"parser":"rules","source":"instagram","warnings":["1 строка не распознана"]}` {
		t.Errorf("import = %s", got)
	}
	if len(*seen) != 1 || (*seen)[0] != (service.ImportInput{URL: "https://instagram.com/reel/DEMOreel001/?igsh=abc"}) {
		t.Errorf("service got %+v", *seen)
	}

	// Text goes through as is; the service trims and validates it.
	expectStatus(t, h.call(http.MethodPost, "/api/recipes/import", bob, map[string]string{"text": " Мука — 200 г "}), http.StatusCreated)
	if (*seen)[1] != (service.ImportInput{Text: " Мука — 200 г "}) {
		t.Errorf("service got %+v", (*seen)[1])
	}

	// A post imported before: 200 with the existing recipe.
	existing := decode[struct {
		Recipe recipeJSON `json:"recipe"`
	}](t, rec).Recipe
	h.db.importFn = func(context.Context, domain.UserID, service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		r, err := fakeRecipes{h.db}.Get(context.Background(), domain.RecipeID(existing.ID))
		return r, service.ImportReport{Source: "instagram", Duplicate: true}, err
	}
	rec = h.call(http.MethodPost, "/api/recipes/import", bob, map[string]string{"url": link})
	expectStatus(t, rec, http.StatusOK)
	dup := decode[struct {
		Recipe recipeJSON       `json:"recipe"`
		Import importReportJSON `json:"import"`
	}](t, rec)
	if dup.Recipe.ID != existing.ID || !dup.Import.Duplicate || dup.Import.Warnings == nil {
		t.Errorf("duplicate = %+v", dup)
	}
}

func TestImportRecipeErrors(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		err     error
		status  int
		code    string
		field   string
		message string
	}{
		{name: "validation", body: `{"url":"https://example.com/"}`, err: &domain.ValidationError{Field: "url", Message: "это не ссылка на пост или рилс в Instagram"},
			status: 400, code: "validation", field: "url", message: "Это не ссылка на пост или рилс в Instagram"},
		{name: "not a recipe", body: `{"text":"привет"}`, err: fmt.Errorf("parse: %w", service.ErrNotARecipe),
			status: 422, code: "not_a_recipe", message: "Не нашли в тексте рецепт — вставьте текст с ингредиентами"},
		{name: "recipe in video", body: `{"url":"https://www.instagram.com/reel/Abcde12345/"}`, err: fmt.Errorf("parse: %w", service.ErrRecipeInVideo),
			status: 422, code: "not_a_recipe", message: "Рецепт, похоже, только в видео — вставьте текст рецепта или подписи"},
		{name: "instagram down", body: `{"url":"https://www.instagram.com/p/Abcde12345/"}`, err: fmt.Errorf("fetch: %w", domain.ErrExternalUnavailable),
			status: 503, code: "unavailable", message: "Instagram не отдал пост. Скопируйте текст подписи и вставьте его сюда."},
		{name: "both", body: `{"url":"https://www.instagram.com/p/Abcde12345/","text":"Мука"}`, status: 400, code: "validation", field: "text"},
		{name: "unknown field", body: `{"link":"https://www.instagram.com/p/Abcde12345/"}`, status: 400, code: "validation", field: "link"},
		{name: "not an object", body: `"https://www.instagram.com/p/Abcde12345/"`, status: 400, code: "validation", field: "-"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			called := false
			h.db.importFn = func(context.Context, domain.UserID, service.ImportInput) (domain.Recipe, service.ImportReport, error) {
				called = true
				return domain.Recipe{}, service.ImportReport{}, c.err
			}
			e := expectError(t, h.call(http.MethodPost, "/api/recipes/import", alice, c.body), c.status, c.code, c.field)
			if c.message != "" && e.Error.Message != c.message {
				t.Errorf("message = %q, want %q", e.Error.Message, c.message)
			}
			if called != (c.err != nil) {
				t.Errorf("service called = %v", called)
			}
		})
	}

	h := newHarness(t)
	rec := h.call(http.MethodPost, "/api/recipes/import", alice, nil)
	expectError(t, rec, http.StatusUnsupportedMediaType, "unsupported_media", "")
}

// An import that runs out of time is the same 503: the user can paste the
// caption instead.
func TestImportRecipeDeadline(t *testing.T) {
	h := newHarness(t, func(_ *Options, tu *tuning) { tu.importDeadline = 20 * time.Millisecond })
	h.db.importFn = func(ctx context.Context, _ domain.UserID, _ service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		<-ctx.Done()
		return domain.Recipe{}, service.ImportReport{}, fmt.Errorf("watch video: %w", ctx.Err())
	}
	expectError(t, h.call(http.MethodPost, "/api/recipes/import", alice, `{"url":"https://www.instagram.com/reel/Abcde12345/"}`),
		http.StatusServiceUnavailable, "unavailable", "")
}

// Imports call Instagram (and maybe a model) for the user, so they share
// the stricter per-user limit for external calls.
func TestImportRecipeRateLimit(t *testing.T) {
	h := newHarness(t, func(_ *Options, tu *tuning) { tu.externalRate = rateLimit{every: 0, burst: 2} })
	h.importAs(domain.RecipeDraft{Title: "Борщ"}, service.ImportReport{Source: "text", Parser: "rules"})
	body := `{"text":"Свёкла — 2 шт"}`
	for range 2 {
		expectStatus(t, h.call(http.MethodPost, "/api/recipes/import", alice, body), http.StatusCreated)
	}
	rec := h.call(http.MethodPost, "/api/recipes/import", alice, body)
	expectError(t, rec, http.StatusTooManyRequests, "rate_limited", "")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 must carry Retry-After")
	}
	// Other routes and other users are unaffected.
	expectStatus(t, h.call(http.MethodPost, "/api/recipes", alice, map[string]string{"title": "Щи"}), http.StatusCreated)
	expectStatus(t, h.call(http.MethodPost, "/api/recipes/import", bob, body), http.StatusCreated)
}

// A slow import outlives the server's read and write timeouts: the route
// extends both for its connection, its context is not cancelled, and the
// answer arrives.
func TestImportRecipeOutlivesServerTimeouts(t *testing.T) {
	h := newHarness(t)
	const serverTimeout = 150 * time.Millisecond
	ctxErr := make(chan error, 1)
	h.db.importFn = func(ctx context.Context, actor domain.UserID, _ service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		time.Sleep(3 * serverTimeout)
		ctxErr <- ctx.Err()
		r, err := fakeRecipes{h.db}.Create(ctx, actor, domain.RecipeDraft{Title: "Долгий рецепт"})
		return r, service.ImportReport{Source: "instagram", Parser: "video"}, err
	}
	srv := httptest.NewUnstartedServer(h.handler)
	srv.Config.ReadTimeout = serverTimeout
	srv.Config.WriteTimeout = serverTimeout
	srv.Start()
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/recipes/import", strings.NewReader(`{"url":"https://www.instagram.com/reel/Abcde12345/"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", tma(alice))
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("the slow import lost its connection: %v", err)
	}
	defer resp.Body.Close()
	var body bytes.Buffer
	_, _ = body.ReadFrom(resp.Body)
	if resp.StatusCode != http.StatusCreated || !strings.Contains(body.String(), "Долгий рецепт") {
		t.Fatalf("status %d: %s", resp.StatusCode, body.String())
	}
	if err := <-ctxErr; err != nil {
		t.Errorf("the import's context ended early: %v", err)
	}
}

func TestRecipeNutritionAuto(t *testing.T) {
	h := newHarness(t)
	rec := h.call(http.MethodPost, "/api/recipes", alice, map[string]any{
		"title":    "Карбонара",
		"servings": 4,
		"ingredients": []map[string]any{
			{"name": "Спагетти", "amount": "320", "unit": "г"},
			{"name": "Яйца", "amount": "4", "unit": "шт"},
			{"name": "Гуанчале", "amount": "100", "unit": "г"},
			{"name": "Пармезан", "amount": nil, "unit": nil},
			{"name": "Соль", "amount": nil, "unit": "по вкусу"},
		},
	})
	expectStatus(t, rec, http.StatusCreated)
	var got struct {
		ID            int64           `json:"id"`
		NutritionAuto json.RawMessage `json:"nutrition_auto"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var auto nutritionAutoJSON
	if err := json.Unmarshal(got.NutritionAuto, &auto); err != nil {
		t.Fatalf("nutrition_auto = %s: %v", got.NutritionAuto, err)
	}
	stored := h.db.recipes[domain.RecipeID(got.ID)]
	want := nutrition.Default().Compute(stored.Ingredients, 4)

	cov := auto.Coverage
	if cov.Counted != 2 || cov.Total != 4 || !slices.Equal(cov.Missing, []string{"Гуанчале"}) ||
		!slices.Equal(cov.NoAmount, []string{"Пармезан"}) || !slices.Equal(cov.Skipped, []string{"Соль"}) {
		t.Errorf("coverage = %+v", cov)
	}
	if auto.WeightG != want.WeightGrams || auto.PerDish != valuesOf(want.Total) || auto.Per100g != valuesOf(want.Per100) ||
		auto.PerServing == nil || *auto.PerServing != valuesOf(*want.PerServing) {
		t.Errorf("values = %+v, want %+v", auto, want)
	}
	if len(auto.Items) != 2 || auto.Items[0].Name != "Спагетти" || auto.Items[0].Grams != 320 || auto.Items[0].Food == "" {
		t.Errorf("items = %+v", auto.Items)
	}
	// Each item carries the four macros as one-decimal strings.
	var items []map[string]json.RawMessage
	var shape struct {
		Items json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(got.NutritionAuto, &shape)
	if err := json.Unmarshal(shape.Items, &items); err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		keys := slices.Sorted(func(yield func(string) bool) {
			for k := range it {
				if !yield(k) {
					return
				}
			}
		})
		if !slices.Equal(keys, []string{"carbs", "fat", "food", "grams", "kcal", "name", "protein"}) {
			t.Errorf("item keys = %v", keys)
		}
		for _, k := range []string{"kcal", "protein", "fat", "carbs"} {
			var s string
			if err := json.Unmarshal(it[k], &s); err != nil {
				t.Errorf("%s = %s, want a decimal string", k, it[k])
			}
			if _, err := strconv.ParseFloat(s, 64); err != nil || (strings.Contains(s, ".") && len(s)-strings.Index(s, ".") != 2) {
				t.Errorf("%s = %q, want at most one decimal", k, s)
			}
		}
	}

	// Without servings there is no per-serving value; without anything
	// countable there is no estimate at all.
	path := "/api/recipes/" + strconv.FormatInt(got.ID, 10)
	rec = h.call(http.MethodPatch, path, alice, `{"servings":null}`)
	expectStatus(t, rec, http.StatusOK)
	if s := rec.Body.String(); !strings.Contains(s, `"per_serving":null,"coverage"`) {
		t.Errorf("per_serving without servings: %s", s)
	}
	rec = h.call(http.MethodPatch, path, alice, `{"ingredients":[{"name":"Гуанчале","amount":"100","unit":"г"}]}`)
	expectStatus(t, rec, http.StatusOK)
	if s := rec.Body.String(); !strings.Contains(s, `"nutrition_auto":null`) {
		t.Errorf("nothing countable: %s", s)
	}
}
