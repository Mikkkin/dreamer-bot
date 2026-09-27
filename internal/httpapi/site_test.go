package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func TestStaticSite(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/", "/index.html", "/?wish=12"} {
		rec := h.do(http.MethodGet, path, nil, nil)
		expectStatus(t, rec, http.StatusOK)
		if rec.Body.String() != "<!doctype html><title>app</title>" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: unexpected index %q %v", path, rec.Body.String(), rec.Header())
		}
	}

	etag := h.do(http.MethodGet, "/", nil, nil).Header().Get("ETag")
	if etag == "" {
		t.Fatal("index must carry an ETag")
	}
	expectStatus(t, h.do(http.MethodGet, "/", nil, http.Header{"If-None-Match": {etag}}), http.StatusNotModified)

	rec := h.do(http.MethodGet, "/assets/app-abc123.js", nil, nil)
	expectStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("asset Content-Type = %q", ct)
	}
	expectStatus(t, h.do(http.MethodHead, "/assets/app-abc123.js", nil, nil), http.StatusOK)
	expectStatus(t, h.do(http.MethodGet, "/favicon.svg", nil, nil), http.StatusOK)

	for _, path := range []string{"/.gitkeep", "/assets/", "/assets", "/nope", "/wishes/12"} {
		rec := h.do(http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404 (never the index)", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<title>app</title>") {
			t.Errorf("%s: served the index", path)
		}
	}
	for _, path := range []string{"/api/", "/api/unknown", "/media/1"} {
		rec := h.do(http.MethodGet, path, nil, nil)
		expectError(t, rec, http.StatusNotFound, "not_found", "")
	}

	rec = h.do(http.MethodPost, "/", nil, nil)
	expectStatus(t, rec, http.StatusMethodNotAllowed)
	if rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatal("405 must list the allowed methods")
	}
}

func TestStaticSiteNotBuilt(t *testing.T) {
	for name, static := range map[string]fstest.MapFS{"only gitkeep": {".gitkeep": {}}, "nil": nil} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, func(o *Options, _ *tuning) {
				if static == nil {
					o.Static = nil
				} else {
					o.Static = static
				}
			})
			rec := h.do(http.MethodGet, "/", nil, nil)
			expectStatus(t, rec, http.StatusOK)
			if !strings.Contains(rec.Body.String(), "Мини-приложение не собрано") {
				t.Fatalf("unexpected page %q", rec.Body.String())
			}
			expectStatus(t, h.do(http.MethodGet, "/assets/app.js", nil, nil), http.StatusNotFound)
		})
	}
}

func TestHealthz(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/healthz", nil, nil)
	expectStatus(t, rec, http.StatusOK)
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q", rec.Body.String())
	}

	failing := newHarness(t, func(o *Options, _ *tuning) {
		o.Health = func(context.Context) error { return errors.New("database is locked") }
	})
	rec = failing.do(http.MethodGet, "/healthz", nil, nil)
	expectStatus(t, rec, http.StatusServiceUnavailable)
	if strings.Contains(rec.Body.String(), "locked") {
		t.Fatal("health failures must not leak details")
	}
}

func TestMe(t *testing.T) {
	h := newHarness(t)
	expectStatus(t, h.call(http.MethodGet, "/api/me", bob, nil), http.StatusOK)
	rec := h.call(http.MethodGet, "/api/me", alice, nil)
	expectStatus(t, rec, http.StatusOK)

	want := `{"user":{"id":111,"name":"Алиса"},"partners":[{"id":222,"name":"Боб"}],` +
		`"currencies":[{"code":"EUR","symbol":"€"},{"code":"USD","symbol":"$"},{"code":"RUB","symbol":"₽"},{"code":"GBP","symbol":"£"}],` +
		`"default_currency":"EUR","limits":{"title_max":120,"note_max":2000,"link_max":2048,"images_per_wish":10,` +
		`"image_max_bytes":1024,"category_name_max":32,"recipe_body_max":10000,"images_per_recipe":10}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("me =\n%s\nwant\n%s", got, want)
	}
}

func TestMePartnersEmptyIsArray(t *testing.T) {
	h := newHarness(t)
	rec := h.call(http.MethodGet, "/api/me", alice, nil)
	expectStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"partners":[]`) {
		t.Fatalf("partners must be [] not null: %s", rec.Body.String())
	}
}

func TestStatsShape(t *testing.T) {
	h := newHarness(t)
	travel := domain.Category{ID: 2, Name: "Путешествия", Emoji: "✈️", Position: 1}
	h.db.stats = domain.Stats{
		Overall: map[domain.Status]domain.StatusTotals{
			domain.StatusWant: {Count: 8, Sums: []domain.Money{{Minor: 345000, Currency: "EUR"}}},
			domain.StatusDone: {Count: 7},
		},
		Categories: []domain.CategoryStats{
			{Category: &travel, ByStatus: map[domain.Status]domain.StatusTotals{domain.StatusWant: {Count: 3}}},
			{Category: nil, ByStatus: nil},
		},
		Recipes:           12,
		FulfilledThisYear: 5,
	}
	rec := h.call(http.MethodGet, "/api/stats", alice, nil)
	expectStatus(t, rec, http.StatusOK)

	var got struct {
		Overall    map[string]json.RawMessage `json:"overall"`
		Categories []struct {
			Category json.RawMessage            `json:"category"`
			ByStatus map[string]json.RawMessage `json:"by_status"`
		} `json:"categories"`
		FulfilledThisYear int `json:"fulfilled_this_year"`
		Recipes           int `json:"recipes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	checkStatuses := func(where string, m map[string]json.RawMessage) {
		t.Helper()
		if len(m) != 3 {
			t.Errorf("%s: want exactly 3 status keys, got %v", where, m)
		}
		for _, key := range []string{"want", "progress", "done"} {
			if _, ok := m[key]; !ok {
				t.Errorf("%s: missing %q", where, key)
			}
		}
	}
	checkStatuses("overall", got.Overall)
	wantSum := mustJSON(t, priceJSON{Amount: "3450", Currency: "EUR", Formatted: domain.Money{Minor: 345000, Currency: "EUR"}.Format()})
	if string(got.Overall["want"]) != `{"count":8,"sums":[`+wantSum+`]}` {
		t.Errorf("overall.want = %s", got.Overall["want"])
	}
	if string(got.Overall["progress"]) != `{"count":0,"sums":[]}` {
		t.Errorf("overall.progress = %s", got.Overall["progress"])
	}
	if len(got.Categories) != 2 {
		t.Fatalf("categories = %d", len(got.Categories))
	}
	for _, c := range got.Categories {
		checkStatuses("category", c.ByStatus)
	}
	if string(got.Categories[0].Category) != `{"id":2,"name":"Путешествия","emoji":"✈️","position":1}` || string(got.Categories[1].Category) != "null" {
		t.Errorf("categories: %s", rec.Body.String())
	}
	if got.FulfilledThisYear != 5 || got.Recipes != 12 {
		t.Errorf("counters: %+v", got)
	}
}
