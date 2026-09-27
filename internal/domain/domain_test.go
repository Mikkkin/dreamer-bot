package domain

import (
	"strings"
	"testing"
	"time"
)

func TestParseAmount(t *testing.T) {
	cases := []struct {
		in    string
		minor int64
	}{
		{"1200", 120000},
		{"1 200", 120000},
		{"1 200,50", 120050},
		{"1,200.50", 120050},
		{"1.200,5", 120050},
		{"99.9", 9990},
		{"99,90", 9990},
		{"1,200", 120000},
		{"15 000", 1500000},
		{"100.", 10000},
		{"0,01", 1},
		{"12.345", 1234500},
	}
	for _, c := range cases {
		m, err := ParseAmount(c.in, "EUR")
		if err != nil {
			t.Fatalf("ParseAmount(%q): unexpected error %v", c.in, err)
		}
		if m.Minor != c.minor {
			t.Errorf("ParseAmount(%q) = %d, want %d", c.in, m.Minor, c.minor)
		}
	}
}

func TestParseAmountRejects(t *testing.T) {
	for _, in := range []string{"", "abc", "-5", "0", "1.234.5x", "9999999999999", "1,5,00.123"} {
		if _, err := ParseAmount(in, "EUR"); err == nil {
			t.Errorf("ParseAmount(%q): expected error", in)
		}
	}
	if _, err := ParseAmount("10", "XYZ"); err == nil {
		t.Error("unknown currency must be rejected")
	}
}

func TestMoneyFormatting(t *testing.T) {
	m := Money{Minor: 120050, Currency: "EUR"}
	if got := m.Decimal(); got != "1200.50" {
		t.Errorf("Decimal = %q", got)
	}
	if got := m.Format(); got != "1 200,50 €" {
		t.Errorf("Format = %q", got)
	}
	whole := Money{Minor: 1500000, Currency: "RUB"}
	if got := whole.Format(); got != "15 000 ₽" {
		t.Errorf("Format = %q", got)
	}
	back, err := ParseAmount(m.Decimal(), m.Currency)
	if err != nil || back != m {
		t.Errorf("Decimal must round-trip, got %v %v", back, err)
	}
}

func TestSumByCurrency(t *testing.T) {
	sums := SumByCurrency([]Money{{100, "RUB"}, {250, "EUR"}, {50, "RUB"}})
	if len(sums) != 2 || sums[0] != (Money{250, "EUR"}) || sums[1] != (Money{150, "RUB"}) {
		t.Fatalf("unexpected sums %v", sums)
	}
}

func TestNormalizeLink(t *testing.T) {
	ok := map[string]string{
		"https://example.com/a?b=1": "https://example.com/a?b=1",
		"HTTP://Example.com":        "http://Example.com",
		"ozon.ru/item/42":           "https://ozon.ru/item/42",
	}
	for in, want := range ok {
		got, err := NormalizeLink(in)
		if err != nil || got != want {
			t.Errorf("NormalizeLink(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{"", "javascript:alert(1)", "javascript://alert(1)", "ftp://x.org", "https://user:pw@x.org", "https://", "data:text/html,hi", "https://" + strings.Repeat("a", MaxLinkLen)}
	for _, in := range bad {
		if _, err := NormalizeLink(in); err == nil {
			t.Errorf("NormalizeLink(%q): expected error", in)
		}
	}
}

func TestNewWishValidation(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	w, err := NewWish(WishDraft{Title: "  Поездка в Токио  "}, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if w.Title != "Поездка в Токио" || w.Link != nil || w.Price != nil || w.Status != StatusWant {
		t.Fatalf("unexpected wish %+v", w)
	}
	if _, err := NewWish(WishDraft{Title: "   "}, 1, now); err == nil {
		t.Error("empty title must be rejected")
	}
	if _, err := NewWish(WishDraft{Title: strings.Repeat("я", MaxTitleLen+1)}, 1, now); err == nil {
		t.Error("long title must be rejected")
	}
	if _, err := NewWish(WishDraft{Title: "x", Note: strings.Repeat("я", MaxNoteLen+1)}, 1, now); err == nil {
		t.Error("long note must be rejected")
	}
	bad := "javascript:alert(1)"
	if _, err := NewWish(WishDraft{Title: "x", Link: &bad}, 1, now); err == nil {
		t.Error("javascript link must be rejected")
	}
	if _, err := NewWish(WishDraft{Title: "x", Price: &Money{Minor: -1, Currency: "EUR"}}, 1, now); err == nil {
		t.Error("negative price must be rejected")
	}
}

func TestWishApplyIsAtomic(t *testing.T) {
	now := time.Now()
	link := "https://example.com"
	w, _ := NewWish(WishDraft{Title: "Камера", Link: &link}, 1, now)
	bad := "ftp://nope"
	err := w.Apply(WishPatch{Title: Some("Новая"), Link: Some(&bad)}, now)
	if err == nil || w.Title != "Камера" {
		t.Fatalf("failed patch must not mutate: %v %+v", err, w)
	}
	if err := w.Apply(WishPatch{Link: Some[*string](nil)}, now); err != nil || w.Link != nil {
		t.Fatalf("explicit nil must clear link: %v %+v", err, w)
	}
}

func TestWishSetStatus(t *testing.T) {
	now := time.Now()
	w, _ := NewWish(WishDraft{Title: "Море"}, 1, now)
	fulfilled, err := w.SetStatus(StatusDone, now)
	if err != nil || !fulfilled || w.FulfilledAt == nil {
		t.Fatalf("done must stamp FulfilledAt: %v %v %+v", fulfilled, err, w)
	}
	if again, _ := w.SetStatus(StatusDone, now); again {
		t.Error("repeated done is not a new fulfilment")
	}
	if _, err := w.SetStatus(StatusWant, now); err != nil || w.FulfilledAt != nil {
		t.Fatalf("leaving done must clear FulfilledAt: %+v", w)
	}
	if _, err := w.SetStatus("bogus", now); err == nil {
		t.Error("unknown status must be rejected")
	}
}

func TestCategoryValidation(t *testing.T) {
	if _, err := NewCategory("Путешествия", "✈️", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, e := range []string{"", "ab", "🙂 🙂", "1"} {
		if _, err := NewCategory("Имя", e, time.Now()); err == nil {
			t.Errorf("emoji %q must be rejected", e)
		}
	}
	if _, err := NewCategory(strings.Repeat("я", MaxCategoryNameLen+1), "🙂", time.Now()); err == nil {
		t.Error("long name must be rejected")
	}
}

func TestRecipeValidation(t *testing.T) {
	now := time.Now()
	r, err := NewRecipe(RecipeDraft{Title: " Паста карбонара ", Body: "1. Сварить пасту\n2. Смешать"}, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Паста карбонара" || r.Link != nil || r.Body != "1. Сварить пасту\n2. Смешать" {
		t.Fatalf("unexpected recipe %+v", r)
	}
	if _, err := NewRecipe(RecipeDraft{Title: ""}, 1, now); err == nil {
		t.Error("empty title must be rejected")
	}
	if _, err := NewRecipe(RecipeDraft{Title: "x", Body: strings.Repeat("я", MaxRecipeBodyLen+1)}, 1, now); err == nil {
		t.Error("long body must be rejected")
	}
	bad := "javascript:alert(1)"
	if _, err := NewRecipe(RecipeDraft{Title: "x", Link: &bad}, 1, now); err == nil {
		t.Error("javascript link must be rejected")
	}
	link := "eda.ru/recepty/123"
	if err := r.Apply(RecipePatch{Link: Some(&link), Body: Some("")}, now); err != nil || r.Link == nil || *r.Link != "https://eda.ru/recepty/123" || r.Body != "" {
		t.Fatalf("patch failed: %v %+v", err, r)
	}
}
