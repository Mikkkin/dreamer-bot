package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func createWish(t *testing.T, h *harness, user domain.UserID, body map[string]any) string {
	t.Helper()
	rec := h.call(http.MethodPost, "/api/wishes", user, body)
	expectStatus(t, rec, http.StatusCreated)
	return strconv.FormatInt(decode[wishJSON](t, rec).ID, 10)
}

func TestWishCreateShape(t *testing.T) {
	h := newHarness(t)
	rec := h.call(http.MethodPost, "/api/wishes", alice, map[string]any{
		"title": "  Поездка в Токио ",
		"note":  "Весной, на сакуру",
		"link":  "example.com/tour",
		"price": map[string]any{"amount": "1 200,50", "currency": "eur"},
		"hot":   true,
	})
	expectStatus(t, rec, http.StatusCreated)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "title", "note", "category_id", "link", "price", "status", "hot", "author", "images", "created_at", "updated_at", "fulfilled_at"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q in %s", key, rec.Body.String())
		}
	}
	if len(raw) != 13 {
		t.Errorf("unexpected keys in %s", rec.Body.String())
	}
	for key, want := range map[string]string{
		"category_id":  "null",
		"fulfilled_at": "null",
		"images":       "[]",
		"price":        mustJSON(t, priceJSON{Amount: "1200.50", Currency: "EUR", Formatted: domain.Money{Minor: 120050, Currency: "EUR"}.Format()}),
		"author":       `{"id":111,"name":"Алиса"}`,
		"created_at":   `"2026-09-27T10:05:00Z"`,
		"status":       `"want"`,
		"link":         `"https://example.com/tour"`,
		"title":        `"Поездка в Токио"`,
	} {
		if got := string(raw[key]); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
}

func TestWishDefaultCurrency(t *testing.T) {
	h := newHarness(t)
	rec := h.call(http.MethodPost, "/api/wishes", alice, map[string]any{"title": "Книга", "price": map[string]any{"amount": "15"}})
	expectStatus(t, rec, http.StatusCreated)
	if w := decode[wishJSON](t, rec); w.Price == nil || w.Price.Currency != "EUR" || w.Price.Formatted != (domain.Money{Minor: 1500, Currency: "EUR"}).Format() {
		t.Fatalf("unexpected price %+v", w.Price)
	}
}

func TestWishInputValidation(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		ctype  string
		status int
		code   string
		field  string
	}{
		{"unknown field", `{"title":"x","bogus":1}`, "", 400, "validation", "bogus"},
		{"unknown nested field", `{"title":"x","price":{"amount":"1","currency":"EUR","rate":2}}`, "", 400, "validation", "rate"},
		{"two values", `{"title":"x"}{"title":"y"}`, "", 400, "validation", ""},
		{"trailing garbage", `{"title":"x"} x`, "", 400, "validation", ""},
		{"wrong type", `{"title":5}`, "", 400, "validation", "title"},
		{"number amount", `{"title":"x","price":{"amount":12,"currency":"EUR"}}`, "", 400, "validation", "price.amount"},
		{"malformed", `{"title":`, "", 400, "validation", ""},
		{"empty body", ``, "", 400, "validation", ""},
		{"not an object", `["x"]`, "", 400, "validation", "-"},
		{"empty title", `{"title":"   "}`, "", 400, "validation", "title"},
		{"bad link", `{"title":"x","link":"javascript:alert(1)"}`, "", 400, "validation", "link"},
		{"bad amount", `{"title":"x","price":{"amount":"abc","currency":"EUR"}}`, "", 400, "validation", "price"},
		{"bad currency", `{"title":"x","price":{"amount":"1","currency":"XYZ"}}`, "", 400, "validation", "currency"},
		{"unknown category", `{"title":"x","category_id":42}`, "", 400, "validation", "category_id"},
		{"text/plain", `{"title":"x"}`, "text/plain", 415, "unsupported_media", ""},
		{"form", `title=x`, "application/x-www-form-urlencoded", 415, "unsupported_media", ""},
		{"too large", `{"title":"x","note":"` + strings.Repeat("я", 40<<10) + `"}`, "", 413, "too_large", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			header := http.Header{"Authorization": {tma(alice)}}
			header.Set("Content-Type", "application/json; charset=utf-8")
			if c.ctype != "" {
				header.Set("Content-Type", c.ctype)
			}
			rec := h.do(http.MethodPost, "/api/wishes", strings.NewReader(c.body), header)
			expectError(t, rec, c.status, c.code, c.field)
			if len(h.db.wishes) != 0 {
				t.Fatal("nothing may be created from invalid input")
			}
		})
	}
}

func TestValidationMessagesAreCapitalized(t *testing.T) {
	h := newHarness(t)
	e := expectError(t, h.call(http.MethodPost, "/api/wishes", alice, map[string]any{"title": ""}), 400, "validation", "title")
	if e.Error.Message != "Название обязательно" {
		t.Fatalf("message = %q", e.Error.Message)
	}
}

func TestWishPatch(t *testing.T) {
	h := newHarness(t)
	cat := decode[categoryJSON](t, h.call(http.MethodPost, "/api/categories", alice, map[string]string{"name": "Путешествия", "emoji": "✈️"}))
	id := createWish(t, h, alice, map[string]any{
		"title":       "Камера",
		"note":        "Плёночная",
		"category_id": cat.ID,
		"link":        "https://example.com/cam",
		"price":       map[string]string{"amount": "300", "currency": "USD"},
	})
	path := "/api/wishes/" + id

	t.Run("absent fields are untouched", func(t *testing.T) {
		w := decode[wishJSON](t, h.call(http.MethodPatch, path, alice, map[string]any{"hot": true}))
		if !w.Hot || w.Link == nil || w.Price == nil || w.CategoryID == nil || w.Note != "Плёночная" {
			t.Fatalf("unexpected wish %+v", w)
		}
	})
	t.Run("null clears link, price, category and note", func(t *testing.T) {
		rec := h.call(http.MethodPatch, path, alice, `{"link":null,"price":null,"category_id":null,"note":null}`)
		expectStatus(t, rec, http.StatusOK)
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		for _, key := range []string{"link", "price", "category_id"} {
			if string(raw[key]) != "null" {
				t.Errorf("%s = %s, want null", key, raw[key])
			}
		}
		if string(raw["note"]) != `""` || string(raw["title"]) != `"Камера"` {
			t.Errorf("note = %s, title = %s", raw["note"], raw["title"])
		}
	})
	t.Run("set values", func(t *testing.T) {
		w := decode[wishJSON](t, h.call(http.MethodPatch, path, alice, map[string]any{
			"title": "Камера Leica", "link": "leica.com", "price": map[string]string{"amount": "1200.5", "currency": "EUR"}, "category_id": cat.ID,
		}))
		if w.Title != "Камера Leica" || *w.Link != "https://leica.com" || w.Price.Amount != "1200.50" || *w.CategoryID != cat.ID {
			t.Fatalf("unexpected wish %+v", w)
		}
	})
	t.Run("rejections leave the wish unchanged", func(t *testing.T) {
		for _, c := range []struct{ body, field string }{
			{`{"title":null}`, "title"},
			{`{"title":""}`, "title"},
			{`{"bogus":1}`, "bogus"},
			{`{"link":"ftp://x"}`, "link"},
			{`{"price":{"amount":"1"},"extra":true}`, "extra"},
			{`{"price":{"amount":"1","fee":1}}`, "price"},
			{`{"hot":"yes"}`, "hot"},
			{`{"category_id":"2"}`, "category_id"},
			{`null`, ""},
		} {
			expectError(t, h.call(http.MethodPatch, path, alice, c.body), 400, "validation", c.field)
		}
		w := decode[wishJSON](t, h.call(http.MethodGet, path, alice, nil))
		if w.Title != "Камера Leica" {
			t.Fatalf("wish changed by a rejected patch: %+v", w)
		}
	})
	t.Run("missing wish", func(t *testing.T) {
		expectError(t, h.call(http.MethodPatch, "/api/wishes/999", alice, `{"hot":true}`), 404, "not_found", "")
	})
}

func TestWishLifecycle(t *testing.T) {
	h := newHarness(t)
	cat := decode[categoryJSON](t, h.call(http.MethodPost, "/api/categories", alice, map[string]string{"name": "Дом", "emoji": "🏠"}))
	first := createWish(t, h, alice, map[string]any{"title": "Диван", "category_id": cat.ID})
	second := createWish(t, h, alice, map[string]any{"title": "Море"})

	list := func(query string) []wishJSON {
		t.Helper()
		rec := h.call(http.MethodGet, "/api/wishes"+query, alice, nil)
		expectStatus(t, rec, http.StatusOK)
		return decode[struct {
			Wishes []wishJSON `json:"wishes"`
		}](t, rec).Wishes
	}
	if got := list(""); len(got) != 2 || strconv.FormatInt(got[0].ID, 10) != second {
		t.Fatalf("list must be newest first: %+v", got)
	}
	if got := list("?category=none"); len(got) != 1 || got[0].Title != "Море" {
		t.Fatalf("category=none: %+v", got)
	}
	if got := list("?category=" + strconv.FormatInt(cat.ID, 10)); len(got) != 1 || got[0].Title != "Диван" {
		t.Fatalf("category filter: %+v", got)
	}
	if got := list("?q=%D0%BC%D0%BE%D1%80"); len(got) != 1 || got[0].Title != "Море" {
		t.Fatalf("q filter: %+v", got)
	}

	rec := h.call(http.MethodPut, "/api/wishes/"+first+"/status", alice, map[string]string{"status": "done"})
	expectStatus(t, rec, http.StatusOK)
	if w := decode[wishJSON](t, rec); w.Status != "done" || w.FulfilledAt == nil || *w.FulfilledAt != "2026-09-27T10:05:00Z" {
		t.Fatalf("unexpected wish %+v", w)
	}
	if got := list("?status=done"); len(got) != 1 {
		t.Fatalf("status filter: %+v", got)
	}
	if got := list("?status=want"); len(got) != 1 || got[0].Title != "Море" {
		t.Fatalf("status filter: %+v", got)
	}

	for _, q := range []string{"?status=bogus", "?category=abc", "?category=0", "?q=" + strings.Repeat("a", 101)} {
		expectError(t, h.call(http.MethodGet, "/api/wishes"+q, alice, nil), 400, "validation", "-")
	}
	expectError(t, h.call(http.MethodPut, "/api/wishes/"+first+"/status", alice, map[string]string{"status": "maybe"}), 400, "validation", "status")

	rec = h.call(http.MethodDelete, "/api/wishes/"+first, alice, nil)
	expectStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Fatal("204 must have no body")
	}
	expectError(t, h.call(http.MethodGet, "/api/wishes/"+first, alice, nil), 404, "not_found", "")
	expectError(t, h.call(http.MethodDelete, "/api/wishes/"+first, alice, nil), 404, "not_found", "")
}

func TestAuthorNamesResolved(t *testing.T) {
	h := newHarness(t)
	expectStatus(t, h.call(http.MethodGet, "/api/me", bob, nil), http.StatusOK) // bob becomes known
	rec := h.call(http.MethodPost, "/api/wishes", bob, map[string]any{"title": "Велосипед"})
	expectStatus(t, rec, http.StatusCreated)
	if a := decode[wishJSON](t, rec).Author; a.ID != int64(bob) || a.Name != "Боб" {
		t.Fatalf("author = %+v", a)
	}
}
