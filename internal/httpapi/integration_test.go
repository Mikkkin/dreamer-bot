package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	stdjpeg "image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/media"
	"github.com/Mikkkin/dreamer-bot/internal/service"
	"github.com/Mikkkin/dreamer-bot/internal/storage/sqlite"
)

// realStack is the handler over the real use cases, SQLite and the media
// store, served by an httptest server.
type realStack struct {
	t   *testing.T
	ctx context.Context
	srv *httptest.Server
}

func newRealStack(t *testing.T) *realStack {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	dir := t.TempDir()
	quiet := slog.New(slog.DiscardHandler)
	db, err := sqlite.Open(ctx, filepath.Join(dir, "dreamer.db"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := media.NewStore(filepath.Join(dir, "images"), 5<<20)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(service.Deps{Repos: db, Media: store, Whitelist: []domain.UserID{alice, bob}, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(Options{
		Services:        svc,
		Validator:       auth.NewInitDataValidator(testToken, 24*time.Hour, nil),
		Whitelist:       auth.NewWhitelist([]domain.UserID{alice, bob}),
		Signer:          auth.NewMediaSigner(testToken, nil),
		DefaultCurrency: "EUR",
		MaxImageBytes:   5 << 20,
		Log:             quiet,
		Health:          db.Ping,
	}))
	t.Cleanup(srv.Close)
	return &realStack{t: t, ctx: ctx, srv: srv}
}

func (st *realStack) call(method, path string, user domain.UserID, body io.Reader, ctype string) *http.Response {
	st.t.Helper()
	req, err := http.NewRequestWithContext(st.ctx, method, st.srv.URL+path, body)
	if err != nil {
		st.t.Fatal(err)
	}
	if user != 0 {
		req.Header.Set("Authorization", "tma "+signInitData(testToken, user, time.Now()))
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := st.srv.Client().Do(req)
	if err != nil {
		st.t.Fatal(err)
	}
	st.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// json sends a JSON body (none when empty) as user, checks the status and
// decodes the response into out when it is not nil. It returns the body.
func (st *realStack) json(user domain.UserID, method, path, body string, want int, out any) string {
	st.t.Helper()
	var r io.Reader
	ctype := ""
	if body != "" {
		r, ctype = strings.NewReader(body), "application/json"
	}
	resp := st.call(method, path, user, r, ctype)
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		st.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			st.t.Fatalf("%s %s: %v: %s", method, path, err, data)
		}
	}
	return string(data)
}

// TestRealStack drives the handler over the real use cases, SQLite and the
// media store, to catch contract drift between the layers.
func TestRealStack(t *testing.T) {
	st := newRealStack(t)
	call := st.call
	jsonCall := func(method, path string, body string, want int, out any) {
		t.Helper()
		st.json(alice, method, path, body, want, out)
	}

	if resp := call(http.MethodGet, "/healthz", 0, nil, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
	var categories struct {
		Categories []categoryJSON `json:"categories"`
	}
	jsonCall(http.MethodGet, "/api/categories", "", http.StatusOK, &categories)
	if len(categories.Categories) != len(domain.DefaultCategories()) {
		t.Fatalf("default categories not seeded: %+v", categories)
	}
	catID := strconv.FormatInt(categories.Categories[1].ID, 10)

	var wish wishJSON
	jsonCall(http.MethodPost, "/api/wishes",
		`{"title":"Поездка в Токио","category_id":`+catID+`,"link":"example.com","price":{"amount":"1 200,50","currency":"EUR"}}`,
		http.StatusCreated, &wish)
	if wish.Author.Name != "Алиса" || wish.Price == nil || wish.Price.Amount != "1200.50" {
		t.Fatalf("unexpected wish %+v", wish)
	}
	wishPath := "/api/wishes/" + strconv.FormatInt(wish.ID, 10)

	var patched wishJSON
	jsonCall(http.MethodPatch, wishPath, `{"link":null,"hot":true}`, http.StatusOK, &patched)
	if patched.Link != nil || !patched.Hot || patched.Price == nil {
		t.Fatalf("patch: %+v", patched)
	}
	jsonCall(http.MethodPatch, wishPath, `{"category_id":987654}`, http.StatusBadRequest, nil)

	// Upload a real JPEG through multipart and fetch the signed thumbnail.
	var photo bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for x := range 800 {
		src.Set(x, x%600, color.RGBA{R: 200, A: 255})
	}
	if err := stdjpeg.Encode(&photo, src, nil); err != nil {
		t.Fatal(err)
	}
	body, ctype := multipartBody(t, formPart{field: "file", filename: "photo.jpg", data: photo.Bytes()})
	resp := call(http.MethodPost, wishPath+"/images", alice, body, ctype)
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d %s", resp.StatusCode, data)
	}
	var img imageJSON
	if err := json.Unmarshal(data, &img); err != nil {
		t.Fatal(err)
	}
	thumb := call(http.MethodGet, img.ThumbURL, 0, nil, "")
	if thumb.StatusCode != http.StatusOK || thumb.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb: %d %v", thumb.StatusCode, thumb.Header)
	}
	cfg, err := stdjpeg.DecodeConfig(thumb.Body)
	if err != nil || cfg.Width != 480 || cfg.Height != 600 {
		t.Fatalf("thumb is not a 480x600 JPEG: %+v %v", cfg, err)
	}
	notImage, ntype := multipartBody(t, formPart{field: "file", filename: "x.gif", data: []byte("GIF89a....")})
	if resp := call(http.MethodPost, wishPath+"/images", alice, notImage, ntype); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("non-image upload: %d", resp.StatusCode)
	}

	jsonCall(http.MethodPut, wishPath+"/status", `{"status":"done"}`, http.StatusOK, &patched)
	var stats map[string]json.RawMessage
	jsonCall(http.MethodGet, "/api/stats", "", http.StatusOK, &stats)
	if !strings.Contains(string(stats["overall"]), `"done":{"count":1`) {
		t.Fatalf("stats: %s", stats["overall"])
	}

	var recipe recipeJSON
	jsonCall(http.MethodGet, "/api/recipes/random", "", http.StatusNotFound, nil)
	jsonCall(http.MethodPost, "/api/recipes", `{"title":"Борщ","body":"Свёкла"}`, http.StatusCreated, &recipe)
	jsonCall(http.MethodGet, "/api/recipes/random", "", http.StatusOK, &recipe)

	var me meJSON
	if resp := call(http.MethodGet, "/api/me", bob, nil, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("bob /api/me: %d", resp.StatusCode)
	}
	jsonCall(http.MethodGet, "/api/me", "", http.StatusOK, &me)
	if me.User.Name != "Алиса" || len(me.Partners) != 1 || me.Partners[0].Name != "Боб" {
		t.Fatalf("me: %+v", me)
	}

	jsonCall(http.MethodDelete, wishPath, "", http.StatusNoContent, nil)
	if resp := call(http.MethodGet, img.ThumbURL, 0, nil, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("image of a deleted wish: %d", resp.StatusCode)
	}
}

// TestRealStackKitchen covers the iteration-2 features on the real stack:
// recipe tags, ingredients and КБЖУ, the cooking history, the shopping list
// and savings.
func TestRealStackKitchen(t *testing.T) {
	st := newRealStack(t)
	st.json(bob, http.MethodGet, "/api/me", "", http.StatusOK, nil) // bob becomes known
	id := func(v int64) string { return strconv.FormatInt(v, 10) }

	var me meJSON
	st.json(alice, http.MethodGet, "/api/me", "", http.StatusOK, &me)
	if len(me.Units) != len(domain.Units) || me.Limits.IngredientsPerRecipe != domain.MaxIngredientsPerRecipe {
		t.Fatalf("me: %+v", me)
	}

	// Default tags are seeded by the migration.
	var tags struct {
		Tags []recipeTagJSON `json:"tags"`
	}
	st.json(alice, http.MethodGet, "/api/recipe-tags", "", http.StatusOK, &tags)
	if len(tags.Tags) != len(domain.DefaultRecipeTags()) {
		t.Fatalf("default recipe tags not seeded: %+v", tags.Tags)
	}
	tag := func(kind, name string) int64 {
		t.Helper()
		for _, tg := range tags.Tags {
			if tg.Kind == kind && tg.Name == name {
				return tg.ID
			}
		}
		t.Fatalf("no %s tag %q", kind, name)
		return 0
	}
	italian, dinner, second := tag("cuisine", "Итальянская"), tag("course", "Ужин"), tag("course", "Второе")
	var georgian recipeTagJSON
	st.json(alice, http.MethodPost, "/api/recipe-tags", `{"kind":"cuisine","name":"Грузинская","emoji":"🍢"}`, http.StatusCreated, &georgian)
	st.json(alice, http.MethodPost, "/api/recipe-tags", `{"kind":"cuisine","name":"грузинская","emoji":"🍢"}`, http.StatusConflict, nil)

	// A recipe with every optional part round-trips through storage.
	var recipe recipeJSON
	st.json(alice, http.MethodPost, "/api/recipes", `{"title":"Паста карбонара","cuisine_id":`+id(italian)+
		`,"course_ids":[`+id(dinner)+`,`+id(second)+`],"ingredients":[{"name":"Спагетти","amount":"320","unit":"г"},`+
		`{"name":"Соль","amount":null,"unit":"по вкусу"},{"name":"Яйца","amount":"4","unit":"шт"}],`+
		`"nutrition":{"kcal":"150","protein":"12.5","fat":"6","carbs":"10.4","weight_g":800,"servings":4}}`,
		http.StatusCreated, &recipe)
	rp := "/api/recipes/" + id(recipe.ID)
	if recipe.CuisineID == nil || *recipe.CuisineID != italian || len(recipe.CourseIDs) != 2 || recipe.CourseIDs[0] != dinner ||
		len(recipe.Ingredients) != 3 || recipe.Ingredients[1].Amount != nil || *recipe.Ingredients[1].Unit != "по вкусу" ||
		recipe.Nutrition == nil || recipe.Nutrition.PerServing == nil || recipe.Nutrition.PerServing.Kcal != "300" ||
		recipe.Nutrition.PerDish.Carbs != "83.2" {
		t.Fatalf("created recipe: %+v", recipe)
	}
	var stored recipeJSON
	st.json(alice, http.MethodGet, rp, "", http.StatusOK, &stored)
	if mustJSON(t, stored) != mustJSON(t, recipe) {
		t.Fatalf("recipe changed in storage:\n%s\n%s", mustJSON(t, stored), mustJSON(t, recipe))
	}
	st.json(alice, http.MethodPost, "/api/recipes", `{"title":"x","cuisine_id":`+id(dinner)+`}`, http.StatusBadRequest, nil)
	st.json(alice, http.MethodPatch, rp, `{"course_ids":[`+id(georgian.ID)+`]}`, http.StatusBadRequest, nil)
	var filtered struct {
		Recipes []recipeJSON `json:"recipes"`
	}
	st.json(alice, http.MethodGet, "/api/recipes?cuisine="+id(italian)+"&course="+id(second), "", http.StatusOK, &filtered)
	if len(filtered.Recipes) != 1 {
		t.Fatalf("tag filter: %+v", filtered)
	}
	st.json(alice, http.MethodGet, "/api/recipes?cuisine="+id(georgian.ID), "", http.StatusOK, &filtered)
	if len(filtered.Recipes) != 0 {
		t.Fatalf("tag filter: %+v", filtered)
	}

	// Cooking with ratings from both partners.
	var cook cookJSON
	st.json(alice, http.MethodPost, rp+"/cooks", `{"stars":5,"comment":"Идеально"}`, http.StatusCreated, &cook)
	st.json(bob, http.MethodPut, rp+"/cooks/"+id(cook.ID)+"/rating", `{"stars":3}`, http.StatusOK, nil)
	st.json(bob, http.MethodPut, rp+"/cooks/"+id(cook.ID)+"/rating", `{"stars":4,"comment":"Солоновато"}`, http.StatusOK, &cook)
	if len(cook.Ratings) != 2 {
		t.Fatalf("each person rates a cooking once: %+v", cook.Ratings)
	}
	st.json(bob, http.MethodPost, rp+"/cooks", `{}`, http.StatusCreated, nil)
	st.json(alice, http.MethodPost, rp+"/cooks", `{"stars":6}`, http.StatusBadRequest, nil)
	st.json(alice, http.MethodGet, rp, "", http.StatusOK, &stored)
	if c := stored.Cooking; c.Count != 2 || c.RatingAvg == nil || *c.RatingAvg != "4.5" || c.RatingCount != 2 || c.LastCookedAt == nil {
		t.Fatalf("cooking summary: %+v", c)
	}
	var cooks struct {
		Cooks []cookJSON `json:"cooks"`
	}
	st.json(alice, http.MethodGet, rp+"/cooks", "", http.StatusOK, &cooks)
	if len(cooks.Cooks) != 2 || cooks.Cooks[1].ID != cook.ID || cooks.Cooks[0].CookedBy.Name != "Боб" {
		t.Fatalf("cooks: %+v", cooks)
	}

	// Shopping list from the recipe, merging and clearing.
	var items struct {
		Items []shoppingItemJSON `json:"items"`
	}
	st.json(alice, http.MethodPost, rp+"/shopping", `{"positions":[0,0,2]}`, http.StatusCreated, &items)
	if len(items.Items) != 2 || items.Items[0].RecipeID == nil || *items.Items[0].RecipeID != recipe.ID {
		t.Fatalf("from recipe: %+v", items.Items)
	}
	spaghetti := items.Items[0]
	st.json(bob, http.MethodPost, "/api/shopping", `{"items":[{"name":"спагетти","amount":"180","unit":"г"},{"name":"Хлеб"}]}`, http.StatusCreated, &items)
	if items.Items[0].ID != spaghetti.ID || items.Items[0].Quantity == nil || *items.Items[0].Quantity.Amount != "500" {
		t.Fatalf("merge: %+v", items.Items)
	}
	bread := items.Items[1]
	st.json(alice, http.MethodPost, rp+"/shopping", `{"positions":[9]}`, http.StatusBadRequest, nil)
	st.json(alice, http.MethodPatch, "/api/shopping/"+id(bread.ID), `{"amount":"2"}`, http.StatusBadRequest, nil)
	var patched shoppingItemJSON
	st.json(alice, http.MethodPatch, "/api/shopping/"+id(bread.ID), `{"checked":true}`, http.StatusOK, &patched)
	if !patched.Checked || patched.Name != "Хлеб" {
		t.Fatalf("checked: %+v", patched)
	}
	if got := strings.TrimSpace(st.json(alice, http.MethodPost, "/api/shopping/clear-checked", "", http.StatusOK, nil)); got != `{"removed":1}` {
		t.Fatalf("clear-checked: %s", got)
	}
	st.json(alice, http.MethodGet, "/api/shopping", "", http.StatusOK, &items)
	if len(items.Items) != 2 {
		t.Fatalf("list after clearing: %+v", items.Items)
	}

	// Deleting a tag keeps the recipe; deleting the recipe keeps the items.
	st.json(alice, http.MethodDelete, "/api/recipe-tags/"+id(italian), "", http.StatusNoContent, nil)
	st.json(alice, http.MethodGet, rp, "", http.StatusOK, &stored)
	if stored.CuisineID != nil || len(stored.CourseIDs) != 2 {
		t.Fatalf("recipe after deleting its cuisine: %+v", stored)
	}
	st.json(alice, http.MethodDelete, rp, "", http.StatusNoContent, nil)
	st.json(alice, http.MethodGet, "/api/shopping", "", http.StatusOK, &items)
	if len(items.Items) != 2 || items.Items[0].RecipeID != nil {
		t.Fatalf("items must outlive their recipe: %+v", items.Items)
	}

	// «Копим».
	var wish wishJSON
	st.json(alice, http.MethodPost, "/api/wishes", `{"title":"Диван","price":{"amount":"45000","currency":"RUB"}}`, http.StatusCreated, &wish)
	wp := "/api/wishes/" + id(wish.ID)
	var saving savingJSON
	st.json(bob, http.MethodPost, wp+"/savings", `{"amount":{"amount":"12 000"},"note":"аванс"}`, http.StatusCreated, &saving)
	if saving.Amount.Currency != "RUB" || saving.User.Name != "Боб" || saving.Note != "аванс" {
		t.Fatalf("saving: %+v", saving)
	}
	st.json(alice, http.MethodPost, wp+"/savings", `{"amount":{"amount":"5000","currency":"RUB"}}`, http.StatusCreated, nil)
	st.json(alice, http.MethodPost, wp+"/savings", `{"amount":{"amount":"5","currency":"EUR"}}`, http.StatusBadRequest, nil)
	st.json(alice, http.MethodGet, wp, "", http.StatusOK, &wish)
	if s := wish.Saved; wish.Status != "progress" || s == nil || s.Total.Amount != "17000" || s.Percent == nil || *s.Percent != 37 ||
		s.Count == nil || *s.Count != 2 {
		t.Fatalf("saved: %s %+v", wish.Status, wish.Saved)
	}
	var savings struct {
		Savings []savingJSON `json:"savings"`
	}
	st.json(alice, http.MethodGet, wp+"/savings", "", http.StatusOK, &savings)
	if len(savings.Savings) != 2 || savings.Savings[1].ID != saving.ID {
		t.Fatalf("savings must be newest first: %+v", savings)
	}
	st.json(alice, http.MethodDelete, wp+"/savings/"+id(saving.ID), "", http.StatusNoContent, nil)
	st.json(alice, http.MethodGet, wp, "", http.StatusOK, &wish)
	if s := wish.Saved; s == nil || s.Total.Amount != "5000" || *s.Count != 1 {
		t.Fatalf("saved after removing a contribution: %+v", s)
	}

	var stats struct {
		RecipesCooked int         `json:"recipes_cooked"`
		Saved         []priceJSON `json:"saved"`
	}
	st.json(alice, http.MethodGet, "/api/stats", "", http.StatusOK, &stats)
	if len(stats.Saved) != 1 || stats.Saved[0].Amount != "5000" || stats.Saved[0].Currency != "RUB" {
		t.Fatalf("stats: %+v", stats)
	}
}
