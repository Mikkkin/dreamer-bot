package domain

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func TestParseQuantity(t *testing.T) {
	cases := []struct {
		amount, unit, wantAmount, wantFormat string
	}{
		{"1,5", "кг", "1.5", "1½\u00a0кг"},
		{"200", "г", "200", "200 г"},
		{"0.25", "л", "0.25", "¼\u00a0л"},
		{"3", "", "3", "3"},
		{"", "по вкусу", "", "по вкусу"},
	}
	for _, c := range cases {
		q, err := ParseQuantity(c.amount, c.unit)
		if err != nil || q == nil {
			t.Fatalf("ParseQuantity(%q, %q): %v", c.amount, c.unit, err)
		}
		if q.Amount() != c.wantAmount || q.Format() != c.wantFormat {
			t.Errorf("ParseQuantity(%q, %q) = %q / %q", c.amount, c.unit, q.Amount(), q.Format())
		}
	}
	if q, err := ParseQuantity("", ""); q != nil || err != nil {
		t.Errorf("empty quantity must be nil, got %v %v", q, err)
	}
	for _, bad := range [][2]string{{"-1", "г"}, {"0", "г"}, {"1.234", "г"}, {"abc", "шт"}, {"2", "ведро"}, {"1", "по вкусу"}, {"", "г"}, {"9999999", "г"}} {
		if _, err := ParseQuantity(bad[0], bad[1]); err == nil {
			t.Errorf("ParseQuantity(%q, %q) must fail", bad[0], bad[1])
		}
	}
}

func TestNutritionMath(t *testing.T) {
	n, err := NewNutrition("150", "12,5", "6", "10.4", 800, 4)
	if err != nil {
		t.Fatal(err)
	}
	dish, ok := n.PerDish()
	if !ok || dish.Kcal != 12000 || dish.Protein != 1000 || dish.Carbs != 832 {
		t.Fatalf("PerDish = %+v, %v", dish, ok)
	}
	serving, ok := n.PerServing()
	if !ok || serving.Kcal != 3000 || serving.Protein != 250 {
		t.Fatalf("PerServing = %+v, %v", serving, ok)
	}
	if FormatTenths(n.ProteinPer100) != "12,5" || DecimalTenths(n.ProteinPer100) != "12.5" || FormatTenths(60) != "6" {
		t.Error("tenths formatting")
	}
	noWeight, _ := NewNutrition("100", "1", "1", "1", 0, 0)
	if _, ok := noWeight.PerDish(); ok {
		t.Error("per-dish values need a weight")
	}
	for _, bad := range [][4]string{{"901", "1", "1", "1"}, {"100", "101", "1", "1"}, {"-1", "1", "1", "1"}, {"1.25", "1", "1", "1"}} {
		if _, err := NewNutrition(bad[0], bad[1], bad[2], bad[3], 100, 1); err == nil {
			t.Errorf("NewNutrition(%v) must fail", bad)
		}
	}
	if _, err := NewNutrition("100", "1", "1", "1", MaxDishWeightGrams+1, 1); err == nil {
		t.Error("too heavy dish must fail")
	}
}

func TestRecipeTagsAndIngredients(t *testing.T) {
	if _, err := NewRecipeTag("colour", "Красное", "🔴", now); err == nil {
		t.Error("unknown tag kind must fail")
	}
	tag, err := NewRecipeTag(TagCuisine, " Грузинская ", "🍢", now)
	if err != nil || tag.Name != "Грузинская" {
		t.Fatalf("NewRecipeTag = %+v, %v", tag, err)
	}
	if err := tag.Apply(RecipeTagPatch{Emoji: Some("🥙")}); err != nil || tag.Emoji != "🥙" || tag.Name != "Грузинская" {
		t.Fatalf("partial tag patch = %+v, %v", tag, err)
	}
	r, err := NewRecipe(RecipeDraft{Title: "Хачапури", CourseIDs: []RecipeTagID{3, 3, 5}}, 1, now)
	if err != nil || len(r.CourseIDs) != 2 {
		t.Fatalf("courses must be de-duplicated: %+v %v", r.CourseIDs, err)
	}
	many := make([]RecipeTagID, MaxCoursesPerRecipe+1)
	for i := range many {
		many[i] = RecipeTagID(i + 1)
	}
	if _, err := NewRecipe(RecipeDraft{Title: "x", CourseIDs: many}, 1, now); err == nil {
		t.Error("too many courses must fail")
	}
	q, _ := ParseQuantity("200", "г")
	ings, err := NormalizeIngredients([]Ingredient{{Name: " Мука ", Quantity: q}, {Name: "Соль", Quantity: &Quantity{Unit: UnitToTaste}}})
	if err != nil || ings[0].Name != "Мука" || len(ings) != 2 {
		t.Fatalf("NormalizeIngredients = %+v, %v", ings, err)
	}
	if _, err := NormalizeIngredients([]Ingredient{{Name: ""}}); err == nil {
		t.Error("nameless ingredient must fail")
	}
	if _, err := NormalizeIngredients([]Ingredient{{Name: "Соль", Quantity: &Quantity{Hundredths: 100, Unit: UnitToTaste}}}); err == nil {
		t.Error("'по вкусу' with an amount must fail")
	}
}

func TestRatingsAndSummary(t *testing.T) {
	if _, err := NewRating(1, 0, "", now); err == nil {
		t.Error("0 stars must fail")
	}
	if _, err := NewRating(1, 6, "", now); err == nil {
		t.Error("6 stars must fail")
	}
	if _, err := NewRating(1, 5, strings.Repeat("я", MaxRatingCommentLen+1), now); err == nil {
		t.Error("long comment must fail")
	}
	s := CookingSummary{Count: 2, RatingSum: 9, RatingCount: 2}
	if avg, ok := s.AverageTenths(); !ok || avg != 45 {
		t.Errorf("average = %d, %v", avg, ok)
	}
	if _, ok := (CookingSummary{Count: 1}).AverageTenths(); ok {
		t.Error("no ratings means no average")
	}
}

func TestSavings(t *testing.T) {
	price := Money{Minor: 4_500_000, Currency: "RUB"}
	w := Wish{ID: 7, Title: "Диван", Price: &price}
	if _, err := NewSaving(w, Money{Minor: 100, Currency: "EUR"}, 1, "", now); err == nil {
		t.Error("saving in another currency than the price must fail")
	}
	s, err := NewSaving(w, Money{Minor: 1_200_000, Currency: "RUB"}, 1, "аванс", now)
	if err != nil || s.WishID != 7 {
		t.Fatalf("NewSaving = %+v, %v", s, err)
	}
	saved := Money{Minor: 1_200_000, Currency: "RUB"}
	w.Saved = &saved
	if p, ok := w.SavedPercent(); !ok || p != 26 {
		t.Errorf("SavedPercent = %d, %v", p, ok)
	}
	eur := Money{Minor: 100, Currency: "EUR"}
	if err := w.Apply(WishPatch{Price: Some(&eur)}, now); err == nil {
		t.Error("changing the price currency with savings in RUB must fail")
	}
	if err := w.Apply(WishPatch{Price: Some[*Money](nil)}, now); err != nil {
		t.Errorf("removing the price must stay possible: %v", err)
	}
	noPrice := Wish{ID: 8, Saved: &saved}
	if c, ok := noPrice.SavingsCurrency(); !ok || c != "RUB" {
		t.Error("without a price the savings currency comes from what is saved")
	}
}

func TestShoppingMerge(t *testing.T) {
	g500, _ := ParseQuantity("500", "мл")
	g250, _ := ParseQuantity("250", "мл")
	l1, _ := ParseQuantity("1", "л")
	it, err := NewShoppingItem(ShoppingDraft{Name: "Молоко", Quantity: g500}, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if !it.MergeInto(g250, now) || it.Quantity.Format() != "750 мл" {
		t.Fatalf("same unit must merge, got %v", it.Quantity)
	}
	if !it.MergeInto(l1, now) || it.Quantity.Format() != "1¾\u00a0л" {
		t.Fatalf("750 мл + 1 л = %v, want 1¾ л", it.Quantity)
	}
	if !it.MergeInto(g250, now) || it.Quantity.Format() != "2 л" {
		t.Fatalf("an exact sum of at least 1 л must be shown in л, got %v", it.Quantity)
	}
	pcs, _ := ParseQuantity("2", "шт")
	if it.MergeInto(pcs, now) {
		t.Error("unrelated units must not merge")
	}
	kg, _ := ParseQuantity("1", "кг")
	flour, _ := NewShoppingItem(ShoppingDraft{Name: "Мука", Quantity: kg}, 1, now)
	g333, _ := ParseQuantity("333", "г")
	if !flour.MergeInto(g333, now) || flour.Quantity.Format() != "1333 г" {
		t.Fatalf("кг + г = %v", flour.Quantity)
	}
	huge, _ := ParseQuantity("100000", "кг")
	if flour.MergeInto(huge, now) {
		t.Error("an overflowing sum must not merge")
	}
	it.Checked = true
	if it.MergeInto(g250, now) {
		t.Error("a bought item must not absorb new quantities")
	}
	if _, err := NewShoppingItem(ShoppingDraft{Name: "  "}, 1, now); err == nil {
		t.Error("nameless item must fail")
	}
}

func TestSavingRulesForDoneWishAndField(t *testing.T) {
	done := Wish{ID: 1, Status: StatusDone}
	if _, err := NewSaving(done, Money{Minor: 100, Currency: "EUR"}, 1, "", now); err == nil {
		t.Error("saving for a fulfilled wish must fail")
	}
	_, err := NewSaving(Wish{ID: 2, Status: StatusWant}, Money{Minor: 0, Currency: "EUR"}, 1, "", now)
	if v, ok := AsValidation(err); !ok || v.Field != "amount" {
		t.Errorf("a zero saving must be reported on field amount, got %v", err)
	}
}
