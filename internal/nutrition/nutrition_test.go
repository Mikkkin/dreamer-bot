package nutrition

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func food(t *testing.T, id string) Food {
	t.Helper()
	for _, f := range Default().foods {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no food %q", id)
	return Food{}
}

func qty(amount float64, unit domain.Unit) domain.Quantity {
	return domain.Quantity{Hundredths: int64(math.Round(amount * 100)), Unit: unit}
}

func TestDefaultParsesOnce(t *testing.T) {
	a, b := Default(), Default()
	if a != b {
		t.Fatal("Default returned two tables")
	}
	if n := len(a.foods); n < 300 {
		t.Fatalf("Default has %d foods, want the full table", n)
	}
}

func TestGrams(t *testing.T) {
	tb := Default()
	cases := []struct {
		food string
		q    domain.Quantity
		want int
		ok   bool
	}{
		// Weight and volume convert directly; ml by the food's density.
		{"flour_wheat", qty(250, domain.UnitGram), 250, true},
		{"potato", qty(1.5, domain.UnitKilogram), 1500, true},
		{"milk_3_2", qty(200, domain.UnitMilliliter), 206, true}, // 1,03 г/мл
		{"oil_sunflower", qty(0.5, domain.UnitLiter), 460, true}, // 0,92 г/мл
		{"beef", qty(100, domain.UnitMilliliter), 100, true},     // no density: as water
		{"flour_wheat", qty(1, domain.UnitCup), 130, true},       // per-food measures
		{"sugar", qty(2, domain.UnitTablespoon), 30, true},
		{"salt", qty(0.5, domain.UnitTeaspoon), 3, true},
		{"salt", qty(1, domain.UnitPinch), 1, true}, // 0,5 г rounds up
		{"egg", qty(3, domain.UnitPiece), 165, true},
		{"egg", qty(2, ""), 110, true}, // a bare number counts pieces
		{"garlic", qty(3, domain.UnitClove), 15, true},
		{"dill", qty(0.5, domain.UnitBunch), 15, true},
		{"butter", qty(1, domain.UnitPack), 180, true},
		{"vanilla_sugar", qty(1, domain.UnitPack), 8, true}, // pack falls back to the sachet
		{"corn_canned", qty(1, domain.UnitPack), 210, true}, // … or the can
		{"honey", qty(1, domain.UnitTablespoon), 32, true},  // heaped
		// Not convertible: reported, never zero.
		{"flour_wheat", qty(2, domain.UnitPiece), 0, false},
		{"beef", qty(1, domain.UnitTablespoon), 0, false},
		{"egg", qty(1, domain.UnitCup), 0, false},
		{"potato", qty(500, ""), 0, false}, // «Картофель 500» is grams, not 500 pieces
		{"salt", domain.Quantity{Unit: domain.UnitToTaste}, 0, false},
		{"salt", domain.Quantity{Unit: domain.UnitTeaspoon}, 0, false}, // no amount
		{"salt", qty(1, "горсть"), 0, false},
		// Spellings that skipped domain validation still convert.
		{"sugar", qty(2, "столовые ложки"), 30, true},
		{"sugar", qty(1, "ч.л."), 5, true},
		{"flour_wheat", qty(200, "гр"), 200, true},
	}
	for _, c := range cases {
		got, ok := tb.Grams(food(t, c.food), c.q)
		if got != c.want || ok != c.ok {
			t.Errorf("Grams(%s, %d %q) = %d, %v; want %d, %v", c.food, c.q.Hundredths, c.q.Unit, got, ok, c.want, c.ok)
		}
	}
}

func ing(name string, amount float64, unit domain.Unit) domain.Ingredient {
	q := qty(amount, unit)
	return domain.Ingredient{Name: name, Quantity: &q}
}

// A carbonara without its guanciale (not in the table): golden numbers
// computed from foods.json by hand.
func TestComputeCarbonara(t *testing.T) {
	ings := []domain.Ingredient{
		ing("Спагетти", 320, domain.UnitGram),
		ing("Гуанчале", 150, domain.UnitGram),
		ing("Яичные желтки", 4, domain.UnitPiece),
		ing("Яйцо", 1, domain.UnitPiece),
		ing("Пармезан", 50, domain.UnitGram),
		ing("Оливковое масло", 1, domain.UnitTablespoon),
		ing("Чеснок", 2, domain.UnitClove),
		{Name: "Перец чёрный молотый", Quantity: &domain.Quantity{Unit: domain.UnitToTaste}},
		{Name: "Соль"},
		ing("Сливочное масло", 1, domain.UnitPiece), // butter has no piece weight
	}
	got := Default().Compute(ings, 4)

	want := Result{
		Items: []Item{
			{"Спагетти", "Макароны", 320, Values{11872, 416, 48, 2390}},
			{"Яичные желтки", "Желток", 72, Values{2318, 114, 191, 26}},
			{"Яйцо", "Яйцо куриное", 55, Values{787, 69, 52, 4}},
			{"Пармезан", "Пармезан", 50, Values{1960, 179, 125, 16}},
			{"Оливковое масло", "Масло оливковое", 14, Values{1193, 0, 135, 0}}, // 13,5 г
			{"Чеснок", "Чеснок", 10, Values{149, 6, 1, 33}},
		},
		Total:       Values{18279, 785, 552, 2469},
		WeightGrams: 521, // 520,5 г
		Per100:      Values{3512, 151, 106, 474},
		PerServing:  &Values{4570, 196, 138, 617},
		Coverage: Coverage{
			Counted:   6,
			Total:     9,
			Missing:   []string{"Гуанчале"},
			NoAmount:  []string{"Соль", "Сливочное масло"},
			Skipped:   []string{"Перец чёрный молотый"},
			NoMeasure: []string{"Сливочное масло"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.MarshalIndent(got, "", "  ")
		wj, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Compute =\n%s\nwant\n%s", gj, wj)
	}
}

func TestComputeEdges(t *testing.T) {
	tb := Default()

	empty := tb.Compute(nil, 0)
	if empty.PerServing != nil || empty.Coverage.Total != 0 || empty.WeightGrams != 0 || empty.Total != (Values{}) {
		t.Errorf("Compute(nil) = %+v", empty)
	}
	if empty.Items == nil || empty.Coverage.Missing == nil || empty.Coverage.Skipped == nil {
		t.Error("Compute(nil) returns nil slices; want empty ones for JSON")
	}

	// «по вкусу» inside the name, with no amount, is skipped too; known
	// servings with nothing counted still give a (zero) portion.
	r := tb.Compute([]domain.Ingredient{
		{Name: "Соль по вкусу"},
		{Name: "Перец", Quantity: &domain.Quantity{Unit: "по-вкусу"}},
		{Name: "Специи"},
	}, 2)
	if r.Coverage.Total != 1 || len(r.Coverage.Skipped) != 2 || len(r.Coverage.Missing) != 1 || r.Coverage.Counted != 0 {
		t.Errorf("coverage = %+v", r.Coverage)
	}
	if r.PerServing == nil || *r.PerServing != (Values{}) || r.Per100 != (Values{}) {
		t.Errorf("PerServing = %v, Per100 = %v", r.PerServing, r.Per100)
	}

	// Unknown servings: no portion.
	r = tb.Compute([]domain.Ingredient{ing("Сахар", 100, domain.UnitGram)}, 0)
	if r.PerServing != nil {
		t.Errorf("PerServing = %v with unknown servings", *r.PerServing)
	}
	if r.Per100 != food(t, "sugar").Per100 || r.Total != food(t, "sugar").Per100 {
		t.Errorf("100 г сахара: Total %v, Per100 %v; want %v", r.Total, r.Per100, food(t, "sugar").Per100)
	}

	// The invariant the API shows: counted + missing + no amount = total.
	r = tb.Compute([]domain.Ingredient{
		ing("Мука", 200, domain.UnitGram), ing("Мука", 2, domain.UnitPiece), {Name: "Мука"},
		ing("Перец", 1, domain.UnitTeaspoon), {Name: "Соль", Quantity: &domain.Quantity{Unit: domain.UnitToTaste}},
	}, 1)
	c := r.Coverage
	if c.Counted+len(c.Missing)+len(c.NoAmount) != c.Total || c.Total != 4 || c.Counted != 1 {
		t.Errorf("coverage = %+v", c)
	}
}

// USDA energy uses food-specific Atwater factors, and its carbohydrate
// («by difference») includes fibre, so kcal sits below 4P+9F+4C for spices,
// cocoa and citrus. The sane bounds are therefore:
//   - label values (Russian labels use 4/9/4): within max(8 kcal, 12 %);
//   - USDA values: inside the envelope of the specific factors USDA uses
//     (protein 1.8–4.27, fat 8.37–9.02, carbohydrate 1.3–4.16), ±3 kcal;
//   - energy above the macros only where alcohol or acetic acid explains it.
func TestDataEnergyMatchesMacros(t *testing.T) {
	var raw struct {
		Foods []struct {
			ID     string              `json:"id"`
			Source string              `json:"source"`
			Per100 map[string]float64  `json:"per100"`
			Grams  map[string]*float64 `json:"grams"`
		} `json:"foods"`
	}
	if err := json.Unmarshal(foodsJSON, &raw); err != nil {
		t.Fatal(err)
	}
	// Energy that P/F/C cannot explain: alcohol (7 kcal/g) and acetic acid.
	nonMacro := map[string]bool{
		"beer": true, "vodka": true, "wine_red": true, "wine_white": true, "vanilla_extract": true,
		"vinegar": true, "vinegar_apple": true, "vinegar_wine": true, "vinegar_balsamic": true,
	}
	off := 0
	for _, f := range raw.Foods {
		p, fat, c, kcal := f.Per100["protein"], f.Per100["fat"], f.Per100["carbs"], f.Per100["kcal"]
		est := 4*p + 9*fat + 4*c
		if p < 0 || fat < 0 || c < 0 || kcal < 0 || p+fat+c > 100.5 || kcal > 902 {
			t.Errorf("%s: impossible per100 %v", f.ID, f.Per100)
		}
		for u, g := range f.Grams {
			if g == nil || *g <= 0 {
				t.Errorf("%s: grams[%q] = %v; a unit that does not apply must be absent", f.ID, u, g)
			}
		}
		if math.Abs(kcal-est) > math.Max(15, 0.15*math.Max(kcal, est)) {
			off++
		}
		switch {
		case nonMacro[f.ID]:
			if kcal <= est {
				t.Errorf("%s: listed as alcohol/acid energy but kcal %.0f ≤ macros %.1f", f.ID, kcal, est)
			}
		case f.Source == "label-typical":
			if d := math.Abs(kcal - est); d > math.Max(8, 0.12*math.Max(kcal, est)) {
				t.Errorf("%s: label kcal %.0f vs 4P+9F+4C %.1f", f.ID, kcal, est)
			}
		default:
			lo := 1.8*p + 8.37*fat + 1.3*c - 3
			hi := 4.27*p + 9.02*fat + 4.16*c + 3
			if kcal < lo || kcal > hi {
				t.Errorf("%s: kcal %.0f outside [%.1f, %.1f] for P %.1f F %.1f C %.1f", f.ID, kcal, lo, hi, p, fat, c)
			}
		}
	}
	t.Logf("%d of %d foods differ from plain 4/9/4 by more than 15%% (fibre, specific factors, alcohol)", off, len(raw.Foods))
}
