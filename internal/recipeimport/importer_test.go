package recipeimport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type fakeLLM struct {
	parsed Parsed
	err    error
	calls  int
}

func (f *fakeLLM) Parse(context.Context, string) (Parsed, error) {
	f.calls++
	return f.parsed, f.err
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/captions/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// mustBeValidDraft runs the draft through the same validation as a recipe
// created by hand.
func mustBeValidDraft(t *testing.T, d domain.RecipeDraft) {
	t.Helper()
	if _, err := domain.NewRecipe(d, 1, time.Now()); err != nil {
		t.Fatalf("the draft does not validate: %v (%+v)", err, d)
	}
}

func TestFromText(t *testing.T) {
	llm := &fakeLLM{}
	im := Importer{LLM: llm}
	res, err := im.FromText(context.Background(), readFixture(t, "17-holodnyj-sup"))
	if err != nil {
		t.Fatal(err)
	}
	mustBeValidDraft(t, res.Draft)
	d := res.Draft
	if d.Title != "Холодный суп на кефире с хрустящим сюрпризом" || d.Link != nil || d.Servings == nil || *d.Servings != 6 || len(d.Ingredients) != 12 {
		t.Errorf("draft = %+v", d)
	}
	if res.Report.Source != SourceText || res.Report.Parser != ParserRules || res.Image != nil || res.Report.Image {
		t.Errorf("report = %+v", res.Report)
	}
	if llm.calls != 0 {
		t.Errorf("the LLM was asked about a confident parse")
	}

	res, err = im.FromText(context.Background(), readFixture(t, "02-yagodnyj-chizkejk"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Draft.Body, "1. Раздробите печенье") || !strings.Contains(res.Draft.Body, "\n8. Перед подачей") {
		t.Errorf("body = %q", res.Draft.Body)
	}
	if strings.Contains(res.Draft.Body, sourceHeading) {
		t.Error("a confident parse must not carry the source text")
	}
}

func TestFromTextRejectsNonRecipes(t *testing.T) {
	for _, name := range []string{"34-negative-igra-gryaz", "35-negative-reklama-sbornika", "32-salat-anons-bez-recepta", "33-salat-iz-redki-tolko-heshtegi"} {
		if _, err := (Importer{}).FromText(context.Background(), readFixture(t, name)); !errors.Is(err, ErrNotARecipe) {
			t.Errorf("%s: err = %v, want ErrNotARecipe", name, err)
		}
	}
	if _, err := (Importer{}).FromText(context.Background(), ""); !errors.Is(err, ErrNotARecipe) {
		t.Errorf("empty text: err = %v", err)
	}
}

// lowCaption is a recipe the rules read with low confidence: steps but no
// ingredient list.
const lowCaption = "Сегодня варила борщ\n\nСвеклу натереть и обжарить с морковью.\nДобавить в бульон капусту и варить 20 минут."

func TestFromTextLowConfidence(t *testing.T) {
	ctx := context.Background()

	// Without an LLM the original text is kept in the body.
	res, err := (Importer{}).FromText(ctx, lowCaption)
	if err != nil {
		t.Fatal(err)
	}
	mustBeValidDraft(t, res.Draft)
	if res.Draft.Title != FallbackTitleText || res.Report.Parser != ParserRules || res.Report.Confidence >= DefaultThreshold {
		t.Errorf("draft %+v, report %+v", res.Draft, res.Report)
	}
	if !strings.Contains(res.Draft.Body, "\n\n"+sourceHeading+"\n"+lowCaption) || !strings.HasPrefix(res.Draft.Body, "1. ") {
		t.Errorf("body = %q", res.Draft.Body)
	}

	// The LLM takes over.
	q, _ := domain.ParseQuantity("1", "шт")
	llm := &fakeLLM{parsed: Parsed{
		Ingredients: []domain.Ingredient{{Name: "Свекла", Quantity: q}, {Name: "Капуста"}},
		Steps:       []string{"Натереть свеклу.", "Сварить."},
		Confidence:  0.85,
	}}
	res, err = Importer{LLM: llm}.FromText(ctx, lowCaption)
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != 1 || res.Report.Parser != ParserLLM || len(res.Draft.Ingredients) != 2 || res.Draft.Body != "1. Натереть свеклу.\n2. Сварить." {
		t.Errorf("draft %+v, report %+v", res.Draft, res.Report)
	}

	// The LLM fails: rules with a warning, and the source text kept.
	res, err = Importer{LLM: &fakeLLM{err: ErrLLMUnavailable}}.FromText(ctx, lowCaption)
	if err != nil || res.Report.Parser != ParserRules || len(res.Report.Warnings) == 0 || !strings.Contains(res.Draft.Body, sourceHeading) {
		t.Errorf("draft %+v, report %+v, %v", res.Draft, res.Report, err)
	}

	// The LLM finds no recipe either.
	if _, err := (Importer{LLM: &fakeLLM{}}).FromText(ctx, lowCaption); !errors.Is(err, ErrNotARecipe) {
		t.Errorf("err = %v, want ErrNotARecipe", err)
	}
}

func TestFromTextBodyLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("Приготовление:\n")
	for b.Len() < 40000 {
		b.WriteString("Перемешать всё очень долго и тщательно, не торопясь.\n")
	}
	res, err := (Importer{}).FromText(context.Background(), b.String())
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(res.Draft.Body); n > domain.MaxRecipeBodyLen {
		t.Errorf("body has %d runes", n)
	}
	mustBeValidDraft(t, res.Draft)
}

func TestFromURL(t *testing.T) {
	jpeg := []byte("\xff\xd8\xff fake jpeg")
	caption := readFixture(t, "10-keksy-na-kefire")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host + r.URL.Path {
		case "www.instagram.com/p/DEMOpost004/":
			_, _ = w.Write([]byte(postPage(`1,024 likes, 31 comments - demo.cook on March 1, 2026: "`+caption+`".`,
				"https://scontent-ams2-1.cdninstagram.com/v/cover.jpg")))
		case "www.instagram.com/reel/NOIMAGE01/":
			_, _ = w.Write([]byte(postPage(`1 like - x on March 1, 2026: "`+caption+`".`, "https://scontent.cdninstagram.com/v/missing.jpg")))
		case "www.instagram.com/p/NOTRECIPE/":
			_, _ = w.Write([]byte(postPage(`1 like - x on March 1, 2026: "`+readFixture(t, "34-negative-igra-gryaz")+`".`, "")))
		case "scontent-ams2-1.cdninstagram.com/v/cover.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(jpeg)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	im := Importer{Fetcher: testFetcher(srv, nil)}
	ctx := context.Background()

	res, err := im.FromURL(ctx, "Глянь https://instagram.com/p/DEMOpost004/?igsh=abc")
	if err != nil {
		t.Fatal(err)
	}
	mustBeValidDraft(t, res.Draft)
	if res.Draft.Link == nil || *res.Draft.Link != "https://www.instagram.com/p/DEMOpost004/" {
		t.Errorf("link = %v", res.Draft.Link)
	}
	if res.Draft.Title != "Кексы на кефире с изюмом" || len(res.Draft.Ingredients) != 10 || res.Draft.Body != "1. Печь при 175°C 25–30 минут, до сухой шпажки." {
		t.Errorf("draft = %+v", res.Draft)
	}
	if string(res.Image) != string(jpeg) || !res.Report.Image || res.Report.Source != SourceInstagram {
		t.Errorf("image %d bytes, report %+v", len(res.Image), res.Report)
	}

	res, err = im.FromURL(ctx, "https://www.instagram.com/reel/NOIMAGE01/")
	if err != nil || res.Image != nil || res.Report.Image || len(res.Report.Warnings) == 0 {
		t.Errorf("a missing cover must be a warning: %+v, %v", res.Report, err)
	}
	if _, err := im.FromURL(ctx, "https://example.com/p/DEMOpost004/"); !errors.Is(err, ErrBadURL) {
		t.Errorf("err = %v, want ErrBadURL", err)
	}
	if _, err := im.FromURL(ctx, "https://www.instagram.com/p/GONE00001/"); !errors.Is(err, ErrUnavailable) || !errors.Is(err, domain.ErrExternalUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if _, err := im.FromURL(ctx, "https://www.instagram.com/p/NOTRECIPE/"); !errors.Is(err, ErrNotARecipe) {
		t.Errorf("err = %v, want ErrNotARecipe", err)
	}
}

func TestFromURLUntitledFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(postPage(`1 like - x on March 1, 2026: "`+readFixture(t, "29-pirog-s-vishnej")+`".`, "")))
	}))
	defer srv.Close()
	res, err := Importer{Fetcher: testFetcher(srv, nil)}.FromURL(context.Background(), "https://www.instagram.com/p/DEMOpost005/")
	if err != nil || res.Draft.Title != FallbackTitleInstagram {
		t.Fatalf("title = %q, %v", res.Draft.Title, err)
	}
	mustBeValidDraft(t, res.Draft)
}

// Every fixture that is a recipe yields a draft that passes the normal
// recipe validation.
func TestGoldenDraftsValidate(t *testing.T) {
	for _, c := range loadGolden(t) {
		res, err := (Importer{}).FromText(context.Background(), c.caption)
		if errors.Is(err, ErrNotARecipe) {
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if _, err := domain.NewRecipe(res.Draft, 1, time.Now()); err != nil {
			t.Errorf("%s: %v", c.id, err)
		}
	}
}
