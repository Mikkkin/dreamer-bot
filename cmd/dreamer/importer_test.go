package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/recipeimport"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

func TestImporterCanonicalLinks(t *testing.T) {
	a := newRecipeImporter(nil, nil)
	for raw, want := range map[string]string{
		"https://www.instagram.com/reel/DItfAhKCJ3h/":                   "https://www.instagram.com/reel/DItfAhKCJ3h/",
		"Смотри https://instagram.com/reels/DItfAhKCJ3h?igsh=abc#c":     "https://www.instagram.com/reel/DItfAhKCJ3h/",
		"https://m.instagram.com/p/DVs8ssPihOG/?utm_source=ig_web_copy": "https://www.instagram.com/p/DVs8ssPihOG/",
	} {
		if got, ok := a.Canonical(raw); !ok || got != want {
			t.Errorf("Canonical(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "https://example.com/reel/DItfAhKCJ3h/", "https://www.instagram.com/v_ogorod/", "https://instagram.com.evil.example/p/DVs8ssPihOG/"} {
		if got, ok := a.Canonical(raw); ok {
			t.Errorf("Canonical(%q) = %q, want no link", raw, got)
		}
	}
}

func TestImporterErrors(t *testing.T) {
	cases := []struct {
		in   error
		want []error
		not  []error
	}{
		{recipeimport.ErrRecipeInVideo, []error{service.ErrRecipeInVideo, service.ErrNotARecipe}, nil},
		{fmt.Errorf("llm: %w", recipeimport.ErrNotARecipe), []error{service.ErrNotARecipe}, []error{service.ErrRecipeInVideo}},
		{recipeimport.ErrNoCaption, []error{domain.ErrExternalUnavailable}, []error{service.ErrNotARecipe}},
	}
	for _, c := range cases {
		got := importError(c.in)
		for _, w := range c.want {
			if !errors.Is(got, w) {
				t.Errorf("importError(%v) = %v, want it to match %v", c.in, got, w)
			}
		}
		for _, w := range c.not {
			if errors.Is(got, w) {
				t.Errorf("importError(%v) = %v must not match %v", c.in, got, w)
			}
		}
	}
	if v, ok := domain.AsValidation(importError(recipeimport.ErrBadURL)); !ok || v.Field != "url" {
		t.Errorf("a bad link must be a validation error on url, got %v", importError(recipeimport.ErrBadURL))
	}
	if importError(nil) != nil {
		t.Error("nil must stay nil")
	}
}

func TestImporterVideoQuotaPerUser(t *testing.T) {
	a := newRecipeImporter(nil, nil)
	first := a.videos(111)
	if again := a.videos(111); again != first {
		t.Error("a user must keep one quota")
	}
	if a.videos(222) == first {
		t.Error("every user needs an own quota")
	}
}

// Text imports run the real rules without any network.
func TestImporterFromText(t *testing.T) {
	a := newRecipeImporter(nil, nil)
	res, err := a.FromText(context.Background(), "Блины\n\nИнгредиенты:\nМука — 200 г\nМолоко — 500 мл\nЯйца — 2 шт\n\nПриготовление:\n1. Смешать всё.\n2. Жарить на сковороде.")
	if err != nil {
		t.Fatalf("FromText: %v", err)
	}
	if res.Draft.Title != "Блины" || len(res.Draft.Ingredients) != 3 || res.Report.Source != "text" || res.Report.Parser != "rules" || res.Image != nil {
		t.Errorf("result = %+v", res)
	}
	if _, err := a.FromText(context.Background(), "Всем привет! Сегодня был чудесный день."); !errors.Is(err, service.ErrNotARecipe) {
		t.Errorf("not a recipe = %v, want service.ErrNotARecipe", err)
	}
}
