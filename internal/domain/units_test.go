package domain

import (
	"encoding/json"
	"os"
	"testing"
)

// unitVector is one line of testdata/units.json, which the Mini App's tests
// read too (web/src/lib/units.test.ts): both sides must render quantities
// the same way. An empty unit is a bare number; an empty amount is none.
type unitVector struct {
	Amount    string `json:"amount"`
	Unit      string `json:"unit"`
	Label     string `json:"label"`
	Formatted string `json:"formatted"`
}

func readJSON[T any](t *testing.T, path string) T {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func TestUnitVectors(t *testing.T) {
	vectors := readJSON[[]unitVector](t, "testdata/units.json")
	if len(vectors) < 40 {
		t.Fatalf("only %d vectors", len(vectors))
	}
	seen := map[Unit]map[Plural]bool{}
	for _, v := range vectors {
		q := Quantity{Unit: Unit(v.Unit)}
		if v.Amount != "" {
			h, err := ParseQuantityAmount(v.Amount)
			if err != nil {
				t.Fatalf("%+v: amount: %v", v, err)
			}
			q.Hundredths = h
		}
		if q.Amount() != v.Amount || q.Label() != v.Label || q.Format() != v.Formatted {
			t.Errorf("%s %s = %q / %q / %q, want %q / %q / %q", v.Amount, v.Unit,
				q.Amount(), q.Label(), q.Format(), v.Amount, v.Label, v.Formatted)
		}
		if seen[q.Unit] == nil {
			seen[q.Unit] = map[Plural]bool{}
		}
		seen[q.Unit][PluralFor(q.Hundredths)] = true

		// What is shown can be typed back: «½» + «чайной ложки» is ½ ч. л.
		back, err := ParseQuantity(q.FormatAmount(), v.Label)
		if err != nil || back == nil || *back != q {
			t.Errorf("ParseQuantity(%q, %q) = %v, %v; want %+v", q.FormatAmount(), v.Label, back, err, q)
		}
	}
	for _, u := range Units {
		if seen[u] == nil {
			t.Errorf("no vector for %q", u)
		}
	}
	for u := range declined {
		for _, p := range [...]Plural{PluralOne, PluralFew, PluralMany, PluralFraction} {
			if !seen[u][p] {
				t.Errorf("no vector for %q in form %d", u, p)
			}
		}
	}
	if !seen[""][PluralFraction] || !seen[""][PluralFew] {
		t.Error("bare numbers need whole and fractional vectors")
	}
}

// amountVector is one line of testdata/amounts.json: what a user may type
// as an amount and its canonical decimal, or null when it is rejected.
type amountVector struct {
	Input  string  `json:"input"`
	Amount *string `json:"amount"`
}

func TestParseQuantityAmountVectors(t *testing.T) {
	for _, v := range readJSON[[]amountVector](t, "testdata/amounts.json") {
		h, err := ParseQuantityAmount(v.Input)
		if v.Amount == nil {
			if verr, ok := AsValidation(err); !ok || verr.Field != "amount" {
				t.Errorf("ParseQuantityAmount(%q) = %d, %v; want a validation error on amount", v.Input, h, err)
			}
			continue
		}
		if err != nil || (Quantity{Hundredths: h}).Amount() != *v.Amount {
			t.Errorf("ParseQuantityAmount(%q) = %d, %v; want %s", v.Input, h, err, *v.Amount)
		}
	}
	_, err := ParseQuantityAmount("1/8")
	if v, ok := AsValidation(err); !ok || v.Message != "укажите дробь вида 1/2, 1/3, 1/4 или десятичную, например 0,5" {
		t.Errorf("an unsupported fraction must explain the format, got %v", err)
	}
}

func TestParseUnitSpellings(t *testing.T) {
	cases := map[Unit][]string{
		UnitTeaspoon:   {"ч. л.", "чайная ложка", "чайные ложки", "чайных ложек", "чайной ложки", "ч л", "ч.л.", "ч.л", "чл", "ч ложка", "Ч.Л.", "ЧАЙНЫЕ ЛОЖКИ"},
		UnitTablespoon: {"ст. л.", "столовая ложка", "столовые ложки", "столовых ложек", "столовой ложки", "ст л", "ст.л.", "ст ложка", "ст.ложки", " ст.  л. "},
		UnitGram:       {"г", "гр", "гр.", "грамм", "граммов"},
		UnitKilogram:   {"кг", "килограмм", "кг."},
		UnitMilliliter: {"мл", "миллилитров"},
		UnitLiter:      {"л", "литр", "литра"},
		UnitPiece:      {"шт", "шт.", "штук", "штуки"},
		UnitClove:      {"зубчик", "зубчика", "зубчиков", "зуб"},
		UnitBunch:      {"пучок", "пучка"},
		UnitCup:        {"стакан", "стакана", "стаканов"},
		UnitPinch:      {"щепотка", "щепотки", "щепоток"},
		UnitPack:       {"упаковка", "упаковки", "уп", "уп."},
		UnitToTaste:    {"по вкусу", "по-вкусу", "По вкусу"},
	}
	for want, spellings := range cases {
		for _, s := range spellings {
			if got, err := ParseUnit(s); err != nil || got != want {
				t.Errorf("ParseUnit(%q) = %q, %v; want %q", s, got, err, want)
			}
		}
	}
	if u, err := ParseUnit("  "); u != "" || err != nil {
		t.Errorf("a blank unit means none, got %q, %v", u, err)
	}
	for _, bad := range []string{"ведро", "ложка", "ст", "ч", "чайная", "кгг"} {
		if _, err := ParseUnit(bad); err == nil {
			t.Errorf("ParseUnit(%q) must fail", bad)
		}
	}
}

func TestPluralFor(t *testing.T) {
	cases := map[int64]Plural{
		0: PluralMany, 1: PluralOne, 2: PluralFew, 4: PluralFew, 5: PluralMany, 11: PluralMany, 12: PluralMany,
		14: PluralMany, 20: PluralMany, 21: PluralOne, 22: PluralFew, 25: PluralMany, 101: PluralOne,
		111: PluralMany, 112: PluralMany, 122: PluralFew, 1000: PluralMany, 1001: PluralOne,
	}
	for n, want := range cases {
		if got := PluralFor(n * hundredthsPerUnit); got != want {
			t.Errorf("PluralFor(%d) = %d, want %d", n, got, want)
		}
	}
	for _, h := range []int64{1, 33, 50, 150, 2150} {
		if PluralFor(h) != PluralFraction {
			t.Errorf("PluralFor(%d hundredths) must be the fraction form", h)
		}
	}
	// Without an amount the label is the dictionary form.
	if got := (Quantity{Unit: UnitTeaspoon}).Label(); got != "чайная ложка" {
		t.Errorf("label without an amount = %q", got)
	}
	if got := (Quantity{Hundredths: 300}).Label(); got != "" {
		t.Errorf("a bare number has no label, got %q", got)
	}
}

func TestNormalizeIngredientsCanonicalizesUnits(t *testing.T) {
	in := []Ingredient{
		{Name: "Сахар", Quantity: &Quantity{Hundredths: 200, Unit: "чайные ложки"}},
		{Name: "Соль", Quantity: &Quantity{Unit: "по-вкусу"}},
		{Name: "Перец", Quantity: &Quantity{}},
	}
	out, err := NormalizeIngredients(in)
	if err != nil {
		t.Fatal(err)
	}
	if *out[0].Quantity != (Quantity{Hundredths: 200, Unit: UnitTeaspoon}) || out[1].Quantity.Unit != UnitToTaste || out[2].Quantity != nil {
		t.Fatalf("normalized = %+v %+v %+v", out[0].Quantity, out[1].Quantity, out[2].Quantity)
	}
	if out[0].Quantity == in[0].Quantity || in[0].Quantity.Unit != "чайные ложки" {
		t.Error("the input quantities must be neither shared nor changed")
	}
	if _, err := NormalizeIngredients([]Ingredient{{Name: "Мука", Quantity: &Quantity{Hundredths: 100, Unit: "ведро"}}}); err == nil {
		t.Error("an unknown unit must fail")
	}
}

func TestRecipeServings(t *testing.T) {
	four, zero, tooMany := 4, 0, MaxServings+1
	n, _ := NewNutrition("150", "12,5", "6", "10.4", 800, 0)
	r, err := NewRecipe(RecipeDraft{Title: "Паста", Servings: &four, Nutrition: &n}, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Servings == nil || *r.Servings != 4 || r.Nutrition.Servings != 4 || n.Servings != 0 {
		t.Fatalf("servings = %v, nutrition %+v; the draft's nutrition must stay untouched (%+v)", r.Servings, r.Nutrition, n)
	}
	if per, ok := r.Nutrition.PerServing(); !ok || per.Kcal != 3000 {
		t.Errorf("per serving uses the recipe's servings: %+v, %v", per, ok)
	}
	four = 5
	if *r.Servings != 4 {
		t.Error("the recipe must not share the draft's servings")
	}
	for _, bad := range []*int{&zero, &tooMany} {
		_, err := NewRecipe(RecipeDraft{Title: "x", Servings: bad}, 1, now)
		if v, ok := AsValidation(err); !ok || v.Field != "servings" {
			t.Errorf("servings %d: %v", *bad, err)
		}
	}

	// Older clients send the servings inside the КБЖУ only.
	old, _ := NewNutrition("100", "1", "1", "1", 0, 3)
	r, err = NewRecipe(RecipeDraft{Title: "Суп", Nutrition: &old}, 1, now)
	if err != nil || r.Servings == nil || *r.Servings != 3 {
		t.Fatalf("servings from the КБЖУ = %v, %v", r.Servings, err)
	}
	plain, _ := NewRecipe(RecipeDraft{Title: "Чай"}, 1, now)
	if plain.Servings != nil || plain.Nutrition != nil {
		t.Errorf("unknown servings stay nil: %+v", plain)
	}

	// A КБЖУ patch without servings keeps them; with servings it sets them.
	per100, _ := NewNutrition("50", "2", "2", "2", 0, 0)
	if err := r.Apply(RecipePatch{Nutrition: Some(&per100)}, now); err != nil || *r.Servings != 3 || r.Nutrition.Servings != 3 {
		t.Fatalf("after a КБЖУ patch: %v %+v %v", r.Servings, r.Nutrition, err)
	}
	withSix, _ := NewNutrition("50", "2", "2", "2", 0, 6)
	if err := r.Apply(RecipePatch{Nutrition: Some(&withSix)}, now); err != nil || *r.Servings != 6 || r.Nutrition.Servings != 6 {
		t.Fatalf("КБЖУ servings must set the recipe's: %v %+v %v", r.Servings, r.Nutrition, err)
	}
	two := 2
	if err := r.Apply(RecipePatch{Servings: Some(&two), Nutrition: Some(&withSix)}, now); err != nil || *r.Servings != 2 || r.Nutrition.Servings != 2 {
		t.Fatalf("the top-level servings win: %v %+v %v", r.Servings, r.Nutrition, err)
	}
	if err := r.Apply(RecipePatch{Servings: Some[*int](nil)}, now); err != nil || r.Servings != nil || r.Nutrition.Servings != 0 {
		t.Fatalf("null clears the servings: %v %+v %v", r.Servings, r.Nutrition, err)
	}
	if err := r.Apply(RecipePatch{Title: Some("Борщ"), Servings: Some(&tooMany)}, now); err == nil || r.Title != "Суп" {
		t.Errorf("an invalid patch must change nothing: %v %q", err, r.Title)
	}
	if err := r.Apply(RecipePatch{Servings: Some(&two), Nutrition: Some[*Nutrition](nil)}, now); err != nil || *r.Servings != 2 || r.Nutrition != nil {
		t.Errorf("servings live without КБЖУ: %v %+v %v", r.Servings, r.Nutrition, err)
	}
}
