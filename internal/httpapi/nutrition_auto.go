package httpapi

import (
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/nutrition"
)

// nutritionAutoJSON is the КБЖУ estimated from the ingredients with the
// built-in food table (internal/nutrition). It sits next to the manual
// nutrition, which wins in the UI when it is set.
type nutritionAutoJSON struct {
	Per100g    macrosJSON          `json:"per_100g"`
	WeightG    int                 `json:"weight_g"`
	PerDish    macrosJSON          `json:"per_dish"`
	PerServing *macrosJSON         `json:"per_serving"`
	Coverage   coverageJSON        `json:"coverage"`
	Items      []nutritionItemJSON `json:"items"`
}

// coverageJSON tells which ingredients the estimate covers. Total counts
// every ingredient except the skipped «по вкусу» ones, so counted +
// missing + no_amount = total. no_amount also holds the lines whose amount
// is in a measure without a known weight for that food («2 шт» of flour).
type coverageJSON struct {
	Counted  int      `json:"counted"`
	Total    int      `json:"total"`
	Missing  []string `json:"missing"`
	NoAmount []string `json:"no_amount"`
	Skipped  []string `json:"skipped"`
}

// nutritionItemJSON is one counted ingredient: its name in the recipe, the
// food it matched, its weight and its КБЖУ.
type nutritionItemJSON struct {
	Name  string `json:"name"`
	Food  string `json:"food"`
	Grams int    `json:"grams"`
	macrosJSON
}

// nutritionAuto estimates the recipe's КБЖУ, or returns nil when no
// ingredient could be counted (or there is no table).
func nutritionAuto(table *nutrition.Table, r domain.Recipe) *nutritionAutoJSON {
	if table == nil {
		return nil
	}
	servings := 0
	if r.Servings != nil {
		servings = *r.Servings
	}
	res := table.Compute(r.Ingredients, servings)
	if res.Coverage.Counted == 0 {
		return nil
	}
	out := &nutritionAutoJSON{
		Per100g: valuesOf(res.Per100),
		WeightG: res.WeightGrams,
		PerDish: valuesOf(res.Total),
		Coverage: coverageJSON{
			Counted:  res.Coverage.Counted,
			Total:    res.Coverage.Total,
			Missing:  nonNil(res.Coverage.Missing),
			NoAmount: nonNil(res.Coverage.NoAmount),
			Skipped:  nonNil(res.Coverage.Skipped),
		},
		Items: make([]nutritionItemJSON, len(res.Items)),
	}
	if res.PerServing != nil {
		m := valuesOf(*res.PerServing)
		out.PerServing = &m
	}
	for i, it := range res.Items {
		out.Items[i] = nutritionItemJSON{Name: it.Name, Food: it.Food, Grams: it.Grams, macrosJSON: valuesOf(it.Values)}
	}
	return out
}

// valuesOf renders estimated values (tenths) like the manual КБЖУ.
func valuesOf(v nutrition.Values) macrosJSON {
	return macrosOf(domain.Macros{Kcal: v.Kcal, Protein: v.Protein, Fat: v.Fat, Carbs: v.Carbs})
}

// nonNil keeps empty lists as [] in JSON.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
