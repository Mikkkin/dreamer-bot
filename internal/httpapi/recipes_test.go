package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestRecipeLifecycle(t *testing.T) {
	h := newHarness(t)
	expectError(t, h.call(http.MethodGet, "/api/recipes/random", alice, nil), 404, "not_found", "")

	rec := h.call(http.MethodPost, "/api/recipes", alice, map[string]any{
		"title": "Паста карбонара", "link": "https://example.com/carbonara", "body": "1. Сварить пасту\n2. Смешать",
	})
	expectStatus(t, rec, http.StatusCreated)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 15 || string(raw["images"]) != "[]" || string(raw["author"]) != `{"id":111,"name":"Алиса"}` {
		t.Fatalf("unexpected recipe shape %s", rec.Body.String())
	}
	for key, want := range map[string]string{
		"cuisine_id":  "null",
		"course_ids":  "[]",
		"ingredients": "[]",
		"servings":    "null",
		"nutrition":   "null",
		"cooking":     `{"count":0,"last_cooked_at":null,"rating_avg":null,"rating_count":0}`,
	} {
		if got := string(raw[key]); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
	created := decode[recipeJSON](t, rec)
	path := "/api/recipes/" + strconv.FormatInt(created.ID, 10)

	expectStatus(t, h.call(http.MethodPost, "/api/recipes", alice, map[string]any{"title": "Борщ", "link": nil}), http.StatusCreated)
	list := decode[struct {
		Recipes []recipeJSON `json:"recipes"`
	}](t, h.call(http.MethodGet, "/api/recipes?q=%D0%BF%D0%B0%D1%81%D1%82", alice, nil)).Recipes
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("q filter: %+v", list)
	}
	expectStatus(t, h.call(http.MethodGet, "/api/recipes/random", alice, nil), http.StatusOK)

	rec = h.call(http.MethodPatch, path, alice, `{"link":null,"body":""}`)
	expectStatus(t, rec, http.StatusOK)
	if got := decode[recipeJSON](t, rec); got.Link != nil || got.Body != "" || got.Title != "Паста карбонара" {
		t.Fatalf("patch: %+v", got)
	}
	expectError(t, h.call(http.MethodPatch, path, alice, `{"category_id":1}`), 400, "validation", "category_id")
	expectError(t, h.call(http.MethodPatch, path, alice, `{"title":null}`), 400, "validation", "title")
	expectError(t, h.call(http.MethodPost, "/api/recipes", alice, `{"title":"x","status":"done"}`), 400, "validation", "status")

	expectStatus(t, h.call(http.MethodDelete, path, alice, nil), http.StatusNoContent)
	expectError(t, h.call(http.MethodGet, path, alice, nil), 404, "not_found", "")
}

func TestCategoryLifecycle(t *testing.T) {
	h := newHarness(t)
	rec := h.call(http.MethodPost, "/api/categories", alice, map[string]string{"name": "Книги", "emoji": "📚"})
	expectStatus(t, rec, http.StatusCreated)
	c := decode[categoryJSON](t, rec)
	if c.Name != "Книги" || c.Emoji != "📚" || c.ID == 0 {
		t.Fatalf("unexpected category %+v", c)
	}
	path := "/api/categories/" + strconv.FormatInt(c.ID, 10)

	expectError(t, h.call(http.MethodPost, "/api/categories", alice, map[string]string{"name": "книги", "emoji": "📖"}), 409, "conflict", "")
	expectError(t, h.call(http.MethodPost, "/api/categories", alice, map[string]string{"name": "Фильмы", "emoji": "abc"}), 400, "validation", "emoji")

	rec = h.call(http.MethodPatch, path, alice, map[string]string{"emoji": "📖"})
	expectStatus(t, rec, http.StatusOK)
	if got := decode[categoryJSON](t, rec); got.Name != "Книги" || got.Emoji != "📖" {
		t.Fatalf("partial patch must keep the name: %+v", got)
	}
	expectError(t, h.call(http.MethodPatch, "/api/categories/999", alice, map[string]string{"name": "X"}), 404, "not_found", "")

	list := decode[struct {
		Categories []categoryJSON `json:"categories"`
	}](t, h.call(http.MethodGet, "/api/categories", alice, nil)).Categories
	if len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("list: %+v", list)
	}

	expectStatus(t, h.call(http.MethodDelete, path, alice, nil), http.StatusNoContent)
	expectError(t, h.call(http.MethodDelete, path, alice, nil), 404, "not_found", "")
}
