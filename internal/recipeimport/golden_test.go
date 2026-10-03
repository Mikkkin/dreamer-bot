package recipeimport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// The golden corpus: real captions with hand-written expected parses (see
// testdata/captions/README.md). The targets come from the build brief:
// at least 90 % of the expected ingredients matched by name, amount and
// unit, and at least 80 % of the recipe captions with the exact number of
// steps. Non-recipes must be rejected.
const (
	targetIngredients = 0.90
	targetSteps       = 0.80
)

type goldenIngredient struct {
	Raw     string  `json:"raw"`
	Name    string  `json:"name"`
	Amount  *string `json:"amount"`
	Unit    *string `json:"unit"`
	Section *string `json:"section"`
}

type goldenFixture struct {
	SourceURL   string             `json:"source_url"`
	Title       *string            `json:"title"`
	Servings    *int               `json:"servings"`
	Ingredients []goldenIngredient `json:"ingredients"`
	Steps       []string           `json:"steps"`
	Notes       string             `json:"notes"`
}

type goldenCase struct {
	id      string
	caption string
	want    goldenFixture
}

func loadGolden(t *testing.T) []goldenCase {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "captions", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	sort.Strings(paths)
	var cases []goldenCase
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var f goldenFixture
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&f); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		caption, err := os.ReadFile(strings.TrimSuffix(path, ".json") + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, goldenCase{id: strings.TrimSuffix(filepath.Base(path), ".json"), caption: string(caption), want: f})
	}
	return cases
}

// stemKey compares names the way a reader would: case, ё/е, punctuation
// and inflection endings do not matter («Красный перец» = «красных
// перца»), the words and their order do.
func stemKey(name string) string {
	fields := strings.FieldsFunc(lowerKeep(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '%'
	})
	for i, f := range fields {
		fields[i] = stemWord(f)
	}
	return strings.Join(fields, " ")
}

var stemEndings = []string{
	"ами", "ями", "ого", "его", "ому", "ему", "ыми", "ими", "ая", "яя", "ое", "ее", "ые", "ие",
	"ый", "ий", "ой", "ей", "ых", "их", "ую", "юю", "ам", "ям", "ах", "ях", "ов", "ев", "ом",
	"ем", "а", "я", "ы", "и", "у", "ю", "е", "о", "ь", "й",
}

func stemWord(w string) string {
	for _, e := range stemEndings {
		if strings.HasSuffix(w, e) && utf8.RuneCountInString(w)-utf8.RuneCountInString(e) >= 3 {
			w = strings.TrimSuffix(w, e)
			break
		}
	}
	switch {
	case strings.HasSuffix(w, "ец"):
		w = strings.TrimSuffix(w, "ец") + "ц"
	case strings.HasSuffix(w, "ок"), strings.HasSuffix(w, "ек"):
		w = w[:len(w)-len("ок")] + "к"
	}
	return w
}

func strictKey(name string) string {
	return collapseSpaces(strings.ReplaceAll(lowerKeep(name), ",", " "))
}

func amountOf(q *domain.Quantity) (string, string) {
	if q == nil {
		return "", ""
	}
	return q.Amount(), string(q.Unit)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type goldenScore struct {
	id                         string
	want, matched, strict, got int
	stepsWant, stepsGot        int
	titleOK, servingsOK        bool
	confidence                 float64
	misses                     []string
	kind                       string // recipe | no_recipe_text | negative
	rejected                   bool
	parsedTitle, expectedTitle string
}

func scoreFixture(c goldenCase) goldenScore {
	p := Parse(c.caption)
	s := goldenScore{
		id: c.id, want: len(c.want.Ingredients), got: len(p.Ingredients),
		stepsWant: len(c.want.Steps), stepsGot: len(p.Steps),
		confidence: p.Confidence, kind: "recipe", rejected: p.notARecipe(),
		parsedTitle: p.Title, expectedTitle: deref(c.want.Title),
	}
	switch {
	case strings.HasPrefix(c.want.Notes, "NEGATIVE"):
		s.kind = "negative"
	case strings.HasPrefix(c.want.Notes, "NO_RECIPE_TEXT"):
		s.kind = "no_recipe_text"
	}
	s.titleOK = strictKey(p.Title) == strictKey(deref(c.want.Title))
	wantServings := 0
	if c.want.Servings != nil {
		wantServings = *c.want.Servings
	}
	s.servingsOK = p.Servings == wantServings
	used := make([]bool, len(p.Ingredients))
	for _, w := range c.want.Ingredients {
		found := false
		for i, g := range p.Ingredients {
			amount, unit := amountOf(g.Quantity)
			if used[i] || stemKey(g.Name) != stemKey(w.Name) || amount != deref(w.Amount) || unit != deref(w.Unit) {
				continue
			}
			used[i], found = true, true
			s.matched++
			if strictKey(g.Name) == strictKey(w.Name) {
				s.strict++
			}
			break
		}
		if !found {
			s.misses = append(s.misses, fmt.Sprintf("%s: want %q %s %s | raw %q", c.id, w.Name, deref(w.Amount), deref(w.Unit), w.Raw))
		}
	}
	return s
}

func TestGoldenCorpus(t *testing.T) {
	cases := loadGolden(t)
	var (
		rows                                  []string
		misses, extras                        []string
		want, matched, strict                 int
		recipes, stepsExact, titles, servings int
	)
	for _, c := range cases {
		s := scoreFixture(c)
		p := Parse(c.caption)
		stepMark := " "
		if s.stepsGot == s.stepsWant {
			stepMark = "✓"
		}
		titleMark := " "
		if s.titleOK {
			titleMark = "✓"
		}
		rows = append(rows, fmt.Sprintf("%-40s %-14s ings %2d/%2d (got %2d)  steps %2d/%2d %s  title %s  servings %d  conf %.2f  rejected=%v",
			s.id, s.kind, s.matched, s.want, s.got, s.stepsGot, s.stepsWant, stepMark, titleMark, p.Servings, s.confidence, s.rejected))
		switch s.kind {
		case "negative":
			if !s.rejected {
				t.Errorf("%s: a non-recipe must be rejected, got %d ingredients, %d steps, confidence %.2f", s.id, s.got, s.stepsGot, s.confidence)
			}
			continue
		case "no_recipe_text":
			if !s.rejected && s.confidence >= minConfidence {
				t.Errorf("%s: a caption without recipe text must be rejected or score below %.1f, got %.2f", s.id, minConfidence, s.confidence)
			}
			continue
		}
		recipes++
		want += s.want
		matched += s.matched
		strict += s.strict
		if s.stepsGot == s.stepsWant {
			stepsExact++
		}
		if s.titleOK {
			titles++
		}
		if s.servingsOK {
			servings++
		}
		if s.rejected {
			t.Errorf("%s: a recipe was rejected (confidence %.2f)", s.id, s.confidence)
		}
		misses = append(misses, s.misses...)
		if extra := s.got - s.matched; extra > 0 {
			extras = append(extras, fmt.Sprintf("%s: %d unexpected", s.id, extra))
		}
	}
	ingRate := float64(matched) / float64(want)
	stepRate := float64(stepsExact) / float64(recipes)
	t.Logf("golden corpus, %d fixtures (%d recipes):\n%s", len(cases), recipes, strings.Join(rows, "\n"))
	t.Logf("ingredients: %d/%d matched by name+amount+unit = %.1f%% (target %.0f%%); exact names %d/%d = %.1f%%",
		matched, want, 100*ingRate, 100*targetIngredients, strict, want, 100*float64(strict)/float64(want))
	t.Logf("steps: %d/%d recipe captions with the exact step count = %.1f%% (target %.0f%%)", stepsExact, recipes, 100*stepRate, 100*targetSteps)
	t.Logf("titles: %d/%d exact; servings: %d/%d exact", titles, recipes, servings, recipes)
	if len(misses) > 0 {
		t.Logf("missed ingredients (%d):\n%s", len(misses), strings.Join(misses, "\n"))
	}
	if len(extras) > 0 {
		t.Logf("unexpected parsed ingredients: %s", strings.Join(extras, "; "))
	}
	if ingRate < targetIngredients {
		t.Errorf("ingredient match rate %.1f%% is below the %.0f%% target", 100*ingRate, 100*targetIngredients)
	}
	if stepRate < targetSteps {
		t.Errorf("exact step counts %.1f%% are below the %.0f%% target", 100*stepRate, 100*targetSteps)
	}
}

// TestGoldenDump prints every parse in full; run it by name when working
// on the parser: go test ./internal/recipeimport -run GoldenDump -v
func TestGoldenDump(t *testing.T) {
	if os.Getenv("RECIPEIMPORT_DUMP") == "" {
		t.Skip("set RECIPEIMPORT_DUMP=1 to print every parse")
	}
	for _, c := range loadGolden(t) {
		if id := os.Getenv("RECIPEIMPORT_DUMP"); id != "1" && !strings.HasPrefix(c.id, id) {
			continue
		}
		p := Parse(c.caption)
		var b strings.Builder
		fmt.Fprintf(&b, "== %s  title=%q servings=%d conf=%.2f\n", c.id, p.Title, p.Servings, p.Confidence)
		for _, ing := range p.Ingredients {
			amount, unit := amountOf(ing.Quantity)
			fmt.Fprintf(&b, "   ING %q %s %s\n", ing.Name, amount, unit)
		}
		for i, s := range p.Steps {
			fmt.Fprintf(&b, "   STEP %d: %s\n", i+1, s)
		}
		for _, w := range p.Warnings {
			fmt.Fprintf(&b, "   WARN %s\n", w)
		}
		t.Log(b.String())
	}
}
