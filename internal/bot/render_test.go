package bot

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

var (
	tagPattern     = regexp.MustCompile(`<[^>]*>`)
	allowedTags    = regexp.MustCompile(`^</?(b|i|code)>$|^<a href="[^"<>]*">$|^</a>$`)
	scriptInjected = "<script>alert(1)</script>"
)

// visibleLen is what Telegram counts: text without tags, entities decoded,
// in UTF-16 units.
func visibleLen(s string) int {
	return utf16Len(html.UnescapeString(tagPattern.ReplaceAllString(s, "")))
}

// assertSafeHTML fails if s contains any tag other than the ones the bot
// emits itself, and checks that tags are balanced.
func assertSafeHTML(t *testing.T, s string) {
	t.Helper()
	depth := 0
	for _, tag := range tagPattern.FindAllString(s, -1) {
		if !allowedTags.MatchString(tag) {
			t.Errorf("unexpected tag %q in %q", tag, s)
		}
		if strings.HasPrefix(tag, "</") {
			depth--
		} else {
			depth++
		}
	}
	if depth != 0 {
		t.Errorf("unbalanced tags in %q", s)
	}
}

func hostileWish() domain.Wish {
	link := `https://evil.example/"><script>x</script>`
	return domain.Wish{
		ID:        7,
		Title:     scriptInjected + " & <b>жирный</b>",
		Note:      "<i>note</i> & " + scriptInjected,
		Link:      &link,
		Status:    domain.StatusWant,
		CreatedAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
	}
}

func TestUserContentIsEscaped(t *testing.T) {
	w := hostileWish()
	meta := entityMeta{category: "<u>cat</u>", author: "<b>Дима</b>"}
	rec := domain.Recipe{ID: 3, Title: scriptInjected, Body: scriptInjected, Link: w.Link}
	d := draft{title: scriptInjected, text: scriptInjected, link: w.Link, categoryLabel: "<i>x", category: new(domain.CategoryID)}

	outputs := map[string]string{
		"wish caption":   renderWish(w, meta, time.UTC, maxCaptionLen, captionNoteExcerpt),
		"wish text":      renderWish(w, meta, time.UTC, maxMessageLen, messageNoteExcerpt),
		"recipe":         renderRecipe(rec, meta, time.UTC, maxCaptionLen, captionBodyExcerpt),
		"draft":          renderDraft(d),
		"saved":          renderSaved(d, 1),
		"wish created":   renderWishCreated("<b>Дима</b>", w, maxCaptionLen),
		"wish fulfilled": renderWishFulfilled("<script>", w, maxCaptionLen),
		"recipe created": renderRecipeCreated("<i>", rec, maxMessageLen),
		"setup":          renderSetupReply(42),
	}
	for name, out := range outputs {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(out, "<script>") || strings.Contains(out, "<u>") {
				t.Errorf("raw user markup in %q", out)
			}
			assertSafeHTML(t, out)
		})
	}
	if !strings.Contains(outputs["wish text"], "&lt;script&gt;alert(1)&lt;/script&gt; &amp; &lt;b&gt;") {
		t.Errorf("title not escaped as text: %q", outputs["wish text"])
	}
}

func TestLinksOnlyForWebURLs(t *testing.T) {
	h := newHTML(maxMessageLen)
	h.Link("https://www.shop.example/a?b=1&c=\"2\"", "shop").Text(" ").Link("javascript:alert(1)", "js").Text(" ").
		Link("https://user:pw@evil.example/", "creds").Text(" ").Link("tg://resolve?domain=x", "tg")
	out := h.String()
	if strings.Count(out, "<a ") != 1 {
		t.Fatalf("expected exactly one link: %q", out)
	}
	if !strings.Contains(out, `<a href="https://www.shop.example/a?b=1&amp;c=&#34;2&#34;">shop</a>`) {
		t.Errorf("href not escaped: %q", out)
	}
	if linkLabel("https://www.shop.example/path") != "shop.example" {
		t.Errorf("label %q", linkLabel("https://www.shop.example/path"))
	}
}

func TestCaptionTruncation(t *testing.T) {
	w := hostileWish()
	w.Title = strings.Repeat("Я", domain.MaxTitleLen)
	w.Note = strings.Repeat("🔥 & <длинная заметка> ", 400) // far beyond any limit, 2-unit emoji
	meta := entityMeta{category: "✈️ Путешествия", author: strings.Repeat("Имя", 50)}
	for name, tc := range map[string]struct {
		out   string
		limit int
	}{
		"caption": {renderWish(w, meta, time.UTC, maxCaptionLen, 5000), maxCaptionLen},
		"message": {renderWish(w, meta, time.UTC, maxMessageLen, 5000), maxMessageLen},
	} {
		if n := visibleLen(tc.out); n > tc.limit {
			t.Errorf("%s: %d visible units, limit %d", name, n, tc.limit)
		}
		if !strings.Contains(tc.out, "…") {
			t.Errorf("%s: truncation not marked", name)
		}
		assertSafeHTML(t, tc.out)
	}
}

func TestRecipeExcerptHint(t *testing.T) {
	r := domain.Recipe{Title: "Борщ", Body: strings.Repeat("Шаг. ", 500), CreatedAt: time.Now()}
	out := renderRecipe(r, entityMeta{author: "Аня"}, time.UTC, maxCaptionLen, captionBodyExcerpt)
	if !strings.Contains(out, fullTextHint) || !strings.Contains(out, "Аня") {
		t.Errorf("recipe caption %q", out)
	}
	if visibleLen(out) > maxCaptionLen {
		t.Errorf("caption too long: %d", visibleLen(out))
	}
	short := renderRecipe(domain.Recipe{Title: "Чай", Body: "Заварить"}, entityMeta{author: "Аня"}, time.UTC, maxCaptionLen, captionBodyExcerpt)
	if strings.Contains(short, fullTextHint) {
		t.Error("hint shown for a complete body")
	}
}

func TestClipAndExcerpt(t *testing.T) {
	if got, whole := clip("привет", 10); got != "привет" || !whole {
		t.Errorf("clip fit: %q %v", got, whole)
	}
	if got, whole := clip("привет мир", 7); got != "привет…" || whole {
		t.Errorf("clip cut: %q %v", got, whole)
	}
	if got, _ := clip("🔥🔥🔥", 4); got != "🔥…" {
		t.Errorf("clip must not split surrogate pairs: %q", got)
	}
	if got, _ := clip("abc", 0); got != "" {
		t.Errorf("clip with no budget: %q", got)
	}
	if got, cut := excerpt("абвгд", 3); got != "аб…" || !cut {
		t.Errorf("excerpt: %q %v", got, cut)
	}
	if got, cut := excerpt("абв", 3); got != "абв" || cut {
		t.Errorf("excerpt fit: %q %v", got, cut)
	}
}

func TestHTMLBuilderStopsAfterCut(t *testing.T) {
	h := newHTML(5)
	h.Bold("абвгдеж").Text("хвост")
	if got := h.String(); got != "<b>абвг…</b>" || !h.Truncated() {
		t.Errorf("got %q", got)
	}
}

func TestRenderStats(t *testing.T) {
	travel := domain.Category{ID: 2, Name: "Путешествия", Emoji: "✈️"}
	eur := func(units int64) domain.Money { return domain.Money{Minor: units * 100, Currency: "EUR"} }
	s := domain.Stats{
		Categories: []domain.CategoryStats{
			{Category: &travel, ByStatus: map[domain.Status]domain.StatusTotals{
				domain.StatusWant:     {Count: 3, Sums: []domain.Money{eur(3000)}},
				domain.StatusProgress: {Count: 1, Sums: []domain.Money{eur(450)}},
				domain.StatusDone:     {Count: 2},
			}},
			{Category: &domain.Category{Name: "Пусто", Emoji: "🫙"}, ByStatus: map[domain.Status]domain.StatusTotals{}},
			{Category: nil, ByStatus: map[domain.Status]domain.StatusTotals{
				domain.StatusWant: {Count: 1, Sums: []domain.Money{{Minor: 5000, Currency: "USD"}}},
			}},
		},
		Overall: map[domain.Status]domain.StatusTotals{
			domain.StatusWant:     {Count: 4, Sums: []domain.Money{eur(3000), {Minor: 5000, Currency: "USD"}}},
			domain.StatusProgress: {Count: 1, Sums: []domain.Money{eur(450)}},
			domain.StatusDone:     {Count: 2},
		},
		Recipes:           12,
		FulfilledThisYear: 5,
	}
	out := renderStats(s)
	wantLine := "✈️ Путешествия — 3 хотим · 1 копим · 2 сбылось · " + eur(3450).Format()
	for _, want := range []string{
		wantLine,
		"📦 Без категории — 1 хотим · " + domain.Money{Minor: 5000, Currency: "USD"}.Format(),
		"💰 Хотим и копим: " + eur(3450).Format() + " + " + domain.Money{Minor: 5000, Currency: "USD"}.Format(),
		"Сбылось в этом году: 5",
		"Рецептов: 12",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Пусто") {
		t.Error("an empty category is listed")
	}
	if empty := renderStats(domain.Stats{}); !strings.Contains(empty, "Пока пусто") {
		t.Errorf("empty stats %q", empty)
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{0: "рецептов", 1: "рецепт", 2: "рецепта", 5: "рецептов", 11: "рецептов",
		12: "рецептов", 21: "рецепт", 22: "рецепта", 111: "рецептов", 104: "рецепта"} {
		if got := plural(n, "рецепт", "рецепта", "рецептов"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
}
