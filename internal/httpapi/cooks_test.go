package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func cookPath(r recipeJSON, c cookJSON) string {
	return recipePath(r) + "/cooks/" + strconv.FormatInt(c.ID, 10)
}

func TestCookAndRate(t *testing.T) {
	h := newHarness(t)
	expectStatus(t, h.call(http.MethodGet, "/api/me", bob, nil), http.StatusOK) // bob becomes known
	r := createRecipe(t, h, map[string]any{"title": "Паста карбонара"})

	rec := h.call(http.MethodPost, recipePath(r)+"/cooks", alice, `{"stars":5,"comment":"Идеально"}`)
	expectStatus(t, rec, http.StatusCreated)
	want := `{"id":2,"recipe_id":1,"cooked_by":{"id":111,"name":"Алиса"},"cooked_at":"2026-09-27T10:05:00Z",` +
		`"ratings":[{"user":{"id":111,"name":"Алиса"},"stars":5,"comment":"Идеально","rated_at":"2026-09-27T10:05:00Z"}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("cook =\n%s\nwant\n%s", got, want)
	}
	if in := h.db.calls.cookRating; in == nil || in.Stars != 5 || in.Comment != "Идеально" {
		t.Fatalf("rating passed to the service: %+v", in)
	}
	cook := decode[cookJSON](t, rec)

	// Bob rates the same cooking; rating again replaces his stars.
	rec = h.call(http.MethodPut, cookPath(r, cook)+"/rating", bob, `{"stars":3,"comment":""}`)
	expectStatus(t, rec, http.StatusOK)
	rec = h.call(http.MethodPut, cookPath(r, cook)+"/rating", bob, `{"stars":4,"comment":"Солоновато"}`)
	expectStatus(t, rec, http.StatusOK)
	rated := decode[cookJSON](t, rec)
	if len(rated.Ratings) != 2 || rated.Ratings[1].User.Name != "Боб" || rated.Ratings[1].Stars != 4 || rated.Ratings[1].Comment != "Солоновато" {
		t.Fatalf("ratings: %+v", rated.Ratings)
	}

	var raw map[string]json.RawMessage
	_ = json.Unmarshal(h.call(http.MethodGet, recipePath(r), alice, nil).Body.Bytes(), &raw)
	if got := string(raw["cooking"]); got != `{"count":1,"last_cooked_at":"2026-09-27T10:05:00Z","rating_avg":"4.5","rating_count":2}` {
		t.Fatalf("cooking summary = %s", got)
	}

	// A second cooking without a rating keeps the recipe and the average.
	for _, body := range []string{`{}`, `{"stars":null}`, `{"stars":null,"comment":"  "}`} {
		rec = h.call(http.MethodPost, recipePath(r)+"/cooks", bob, body)
		expectStatus(t, rec, http.StatusCreated)
		if h.db.calls.cookRating != nil {
			t.Fatalf("%s must cook without a rating", body)
		}
		if c := decode[cookJSON](t, rec); mustJSON(t, c.Ratings) != "[]" {
			t.Fatalf("ratings must be [] not null: %s", rec.Body.String())
		}
	}
	list := decode[struct {
		Cooks []cookJSON `json:"cooks"`
	}](t, h.call(http.MethodGet, recipePath(r)+"/cooks", alice, nil)).Cooks
	if len(list) != 4 || list[3].ID != cook.ID {
		t.Fatalf("history must be newest first: %+v", list)
	}
	got := decode[recipeJSON](t, h.call(http.MethodGet, recipePath(r), alice, nil))
	if got.Cooking.Count != 4 || got.Cooking.RatingAvg == nil || *got.Cooking.RatingAvg != "4.5" {
		t.Fatalf("cooking = %+v", got.Cooking)
	}

	expectStatus(t, h.call(http.MethodDelete, cookPath(r, cook), alice, nil), http.StatusNoContent)
	expectError(t, h.call(http.MethodDelete, cookPath(r, cook), alice, nil), http.StatusNotFound, "not_found", "")
	got = decode[recipeJSON](t, h.call(http.MethodGet, recipePath(r), alice, nil))
	if got.Cooking.Count != 3 || got.Cooking.RatingAvg != nil || got.Cooking.RatingCount != 0 {
		t.Fatalf("summary after removing the rated cook: %+v", got.Cooking)
	}
}

func TestRatingValidation(t *testing.T) {
	h := newHarness(t)
	r := createRecipe(t, h, map[string]any{"title": "Суп"})
	cook := decode[cookJSON](t, h.call(http.MethodPost, recipePath(r)+"/cooks", alice, `{}`))
	h.db.calls = fakeCalls{}

	long := strings.Repeat("я", domain.MaxRatingCommentLen+1)
	cookCases := []struct{ body, field string }{
		{`{"stars":0}`, "stars"},
		{`{"stars":6}`, "stars"},
		{`{"stars":-1}`, "stars"},
		{`{"stars":4.5}`, "stars"},
		{`{"stars":"5"}`, "stars"},
		{`{"stars":99999999999999999999}`, "stars"},
		{`{"comment":"Вкусно"}`, "stars"},
		{`{"stars":5,"comment":"` + long + `"}`, "comment"},
		{`{"stars":5,"cooked_at":"2020-01-01T00:00:00Z"}`, "cooked_at"},
		{`{"stars":5,"user_id":222}`, "user_id"},
		{`[]`, "-"},
	}
	for _, c := range cookCases {
		expectError(t, h.call(http.MethodPost, recipePath(r)+"/cooks", alice, c.body), http.StatusBadRequest, "validation", c.field)
	}
	if h.db.calls.cooked {
		t.Fatal("invalid ratings must be rejected before the use case runs")
	}
	expectError(t, h.call(http.MethodPost, recipePath(r)+"/cooks", alice, nil), http.StatusUnsupportedMediaType, "unsupported_media", "")

	rateCases := []struct{ body, field string }{
		{`{}`, "stars"},
		{`{"stars":null}`, "stars"},
		{`{"stars":0,"comment":"x"}`, "stars"},
		{`{"stars":6}`, "stars"},
		{`{"stars":3,"comment":"` + long + `"}`, "comment"},
		{`{"stars":3,"user":{"id":222}}`, "user"},
	}
	for _, c := range rateCases {
		expectError(t, h.call(http.MethodPut, cookPath(r, cook)+"/rating", alice, c.body), http.StatusBadRequest, "validation", c.field)
	}
	for _, stars := range []int{1, 5} {
		rec := h.call(http.MethodPut, cookPath(r, cook)+"/rating", alice, map[string]int{"stars": stars})
		expectStatus(t, rec, http.StatusOK)
		if got := decode[cookJSON](t, rec).Ratings; len(got) != 1 || got[0].Stars != stars {
			t.Fatalf("stars %d: %+v", stars, got)
		}
	}
}

func TestCookNotFound(t *testing.T) {
	h := newHarness(t)
	r := createRecipe(t, h, map[string]any{"title": "Суп"})
	cook := decode[cookJSON](t, h.call(http.MethodPost, recipePath(r)+"/cooks", alice, `{}`))
	other := createRecipe(t, h, map[string]any{"title": "Каша"})

	expectError(t, h.call(http.MethodPost, "/api/recipes/999/cooks", alice, `{}`), 404, "not_found", "")
	expectError(t, h.call(http.MethodGet, "/api/recipes/999/cooks", alice, nil), 404, "not_found", "")
	// A cooking is addressed through its own recipe only.
	expectError(t, h.call(http.MethodPut, cookPath(other, cook)+"/rating", alice, `{"stars":5}`), 404, "not_found", "")
	expectError(t, h.call(http.MethodDelete, cookPath(other, cook), alice, nil), 404, "not_found", "")
	for _, path := range []string{recipePath(r) + "/cooks/0/rating", recipePath(r) + "/cooks/x/rating", "/api/recipes/x/cooks/1/rating"} {
		expectError(t, h.call(http.MethodPut, path, alice, `{"stars":5}`), 404, "not_found", "")
	}
}
