package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type tagIDs struct{ asian, italian, dinner, breakfast int64 }

func seedTags(t *testing.T, h *harness) tagIDs {
	t.Helper()
	create := func(kind, name, emoji string) int64 {
		rec := h.call(http.MethodPost, "/api/recipe-tags", alice, map[string]string{"kind": kind, "name": name, "emoji": emoji})
		expectStatus(t, rec, http.StatusCreated)
		return decode[recipeTagJSON](t, rec).ID
	}
	return tagIDs{
		asian:     create("cuisine", "Азиатская", "🍜"),
		italian:   create("cuisine", "Итальянская", "🍝"),
		dinner:    create("course", "Ужин", "🌙"),
		breakfast: create("course", "Завтрак", "🍳"),
	}
}

func createRecipe(t *testing.T, h *harness, body any) recipeJSON {
	t.Helper()
	rec := h.call(http.MethodPost, "/api/recipes", alice, body)
	expectStatus(t, rec, http.StatusCreated)
	return decode[recipeJSON](t, rec)
}

func recipePath(r recipeJSON) string { return "/api/recipes/" + strconv.FormatInt(r.ID, 10) }

func TestRecipeFullInputShape(t *testing.T) {
	h := newHarness(t)
	tags := seedTags(t, h)
	rec := h.call(http.MethodPost, "/api/recipes", alice, `{
		"title": "Паста карбонара",
		"cuisine_id": `+strconv.FormatInt(tags.italian, 10)+`,
		"course_ids": [`+strconv.FormatInt(tags.dinner, 10)+`, `+strconv.FormatInt(tags.breakfast, 10)+`, `+strconv.FormatInt(tags.dinner, 10)+`],
		"ingredients": [
			{"name": " Спагетти ", "amount": "320", "unit": "г"},
			{"name": "Соль", "amount": null, "unit": "по вкусу"},
			{"name": "Яйца", "amount": "4", "unit": "шт"},
			{"name": "Сливки", "amount": "0,5", "unit": "стакан"},
			{"name": "Сахар", "amount": "1 1/2", "unit": "чайные ложки"},
			{"name": "Лимон", "amount": "1", "unit": null},
			{"name": "Перец"}
		],
		"nutrition": {"kcal": "150", "protein": "12,5", "fat": "6", "carbs": "10.4", "weight_g": 800, "servings": 4}
	}`)
	expectStatus(t, rec, http.StatusCreated)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	// formatted joins the amount and the declined unit with a no-break
	// space; unit is always the code, whatever spelling was sent.
	ingredient := func(name, amount, unit, label, formatted string) string {
		out := map[string]any{"name": name, "amount": nil, "unit": nil, "unit_label": nil, "formatted": formatted}
		if amount != "" {
			out["amount"] = amount
		}
		if unit != "" {
			out["unit"], out["unit_label"] = unit, label
		}
		return `{"name":` + mustJSON(t, out["name"]) + `,"amount":` + mustJSON(t, out["amount"]) +
			`,"unit":` + mustJSON(t, out["unit"]) + `,"unit_label":` + mustJSON(t, out["unit_label"]) +
			`,"formatted":` + mustJSON(t, out["formatted"]) + `}`
	}
	for key, want := range map[string]string{
		"cuisine_id": strconv.FormatInt(tags.italian, 10),
		"course_ids": "[" + strconv.FormatInt(tags.dinner, 10) + "," + strconv.FormatInt(tags.breakfast, 10) + "]",
		"ingredients": "[" + strings.Join([]string{
			ingredient("Спагетти", "320", "г", "г", "320\u00a0г"),
			ingredient("Соль", "", "по вкусу", "по вкусу", "по вкусу"),
			ingredient("Яйца", "4", "шт", "шт", "4\u00a0шт"),
			ingredient("Сливки", "0.5", "стакан", "стакана", "½\u00a0стакана"),
			ingredient("Сахар", "1.5", "ч. л.", "чайной ложки", "1½\u00a0чайной ложки"),
			ingredient("Лимон", "1", "", "", "1"),
			ingredient("Перец", "", "", "", ""),
		}, ",") + "]",
		// Older clients send the servings inside the КБЖУ only.
		"servings": "4",
		"nutrition": `{"per_100g":{"kcal":"150","protein":"12.5","fat":"6","carbs":"10.4"},"weight_g":800,"servings":4,` +
			`"per_dish":{"kcal":"1200","protein":"100","fat":"48","carbs":"83.2"},` +
			`"per_serving":{"kcal":"300","protein":"25","fat":"12","carbs":"20.8"}}`,
	} {
		if got := string(raw[key]); got != want {
			t.Errorf("%s =\n%s\nwant\n%s", key, got, want)
		}
	}
}

func TestRecipeNutritionPartialValues(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"per 100 g only", `{"kcal":"99.9","protein":"0","fat":"0","carbs":"1"}`,
			`{"per_100g":{"kcal":"99.9","protein":"0","fat":"0","carbs":"1"},"weight_g":null,"servings":null,"per_dish":null,"per_serving":null}`},
		{"weight without servings", `{"kcal":"200","protein":"10","fat":"5","carbs":"30","weight_g":250,"servings":null}`,
			`{"per_100g":{"kcal":"200","protein":"10","fat":"5","carbs":"30"},"weight_g":250,"servings":null,` +
				`"per_dish":{"kcal":"500","protein":"25","fat":"12.5","carbs":"75"},"per_serving":null}`},
		{"servings without weight", `{"kcal":"200","protein":"10","fat":"5","carbs":"30","servings":3}`,
			`{"per_100g":{"kcal":"200","protein":"10","fat":"5","carbs":"30"},"weight_g":null,"servings":3,"per_dish":null,"per_serving":null}`},
		{"rounding to 0.1", `{"kcal":"33.3","protein":"1.1","fat":"0.1","carbs":"0.5","weight_g":333,"servings":7}`,
			`{"per_100g":{"kcal":"33.3","protein":"1.1","fat":"0.1","carbs":"0.5"},"weight_g":333,"servings":7,` +
				`"per_dish":{"kcal":"110.9","protein":"3.7","fat":"0.3","carbs":"1.7"},` +
				`"per_serving":{"kcal":"15.8","protein":"0.5","fat":"0","carbs":"0.2"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := h.call(http.MethodPost, "/api/recipes", alice, `{"title":"x","nutrition":`+c.input+`}`)
			expectStatus(t, rec, http.StatusCreated)
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(rec.Body.Bytes(), &raw)
			if got := string(raw["nutrition"]); got != c.want {
				t.Fatalf("nutrition =\n%s\nwant\n%s", got, c.want)
			}
		})
	}
	// A blank value is unknown, not zero.
	for _, blank := range []string{`"fat":""`, `"fat":null`} {
		rec := h.call(http.MethodPost, "/api/recipes", alice, `{"title":"x","nutrition":{"kcal":"100","protein":"1",`+blank+`,"carbs":"1"}}`)
		expectStatus(t, rec, http.StatusBadRequest)
		if !strings.Contains(rec.Body.String(), `"field":"fat"`) {
			t.Errorf("blank fat (%s): %s", blank, rec.Body.String())
		}
	}
}

func TestRecipeInputValidation(t *testing.T) {
	h := newHarness(t)
	tags := seedTags(t, h)
	manyIngredients := make([]map[string]any, 51)
	for i := range manyIngredients {
		manyIngredients[i] = map[string]any{"name": "Ингредиент " + strconv.Itoa(i)}
	}
	cases := []struct {
		name  string
		body  any
		field string
	}{
		{"unknown nutrition field", `{"title":"x","nutrition":{"kcal":"1","sugar":"2"}}`, "sugar"},
		{"unknown ingredient field", `{"title":"x","ingredients":[{"name":"Мука","grams":5}]}`, "grams"},
		{"kcal as a number", `{"title":"x","nutrition":{"kcal":150}}`, "nutrition.kcal"},
		{"kcal two decimals", `{"title":"x","nutrition":{"kcal":"12.55"}}`, "kcal"},
		{"kcal garbage", `{"title":"x","nutrition":{"kcal":"много"}}`, "kcal"},
		{"kcal negative", `{"title":"x","nutrition":{"kcal":"-1"}}`, "kcal"},
		{"kcal too high", `{"title":"x","nutrition":{"kcal":"901"}}`, "kcal"},
		{"protein too high", `{"title":"x","nutrition":{"protein":"100.1"}}`, "protein"},
		{"fractional weight", `{"title":"x","nutrition":{"kcal":"1","weight_g":800.5}}`, "nutrition.weight_g"},
		{"weight too high", `{"title":"x","nutrition":{"kcal":"1","weight_g":20001}}`, "weight_g"},
		{"negative servings", `{"title":"x","nutrition":{"kcal":"1","servings":-1}}`, "servings"},
		{"zero servings", `{"title":"x","servings":0}`, "servings"},
		{"too many servings", `{"title":"x","servings":51}`, "servings"},
		{"fractional servings", `{"title":"x","servings":2.5}`, "servings"},
		{"unsupported fraction", `{"title":"x","ingredients":[{"name":"Мука","amount":"1/8","unit":"стакан"}]}`, "ingredients"},
		{"amount as a number", `{"title":"x","ingredients":[{"name":"Мука","amount":200,"unit":"г"}]}`, "ingredients.0.amount"},
		{"amount three decimals", `{"title":"x","ingredients":[{"name":"Мука","amount":"1.234","unit":"г"}]}`, "ingredients"},
		{"amount zero", `{"title":"x","ingredients":[{"name":"Мука","amount":"0","unit":"г"}]}`, "ingredients"},
		{"unknown unit", `{"title":"x","ingredients":[{"name":"Мука","amount":"2","unit":"ведро"}]}`, "ingredients"},
		{"to taste with an amount", `{"title":"x","ingredients":[{"name":"Соль","amount":"1","unit":"по вкусу"}]}`, "ingredients"},
		{"unit without amount", `{"title":"x","ingredients":[{"name":"Мука","unit":"г"}]}`, "ingredients"},
		{"nameless ingredient", `{"title":"x","ingredients":[{"name":"  "}]}`, "ingredients"},
		{"too many ingredients", map[string]any{"title": "x", "ingredients": manyIngredients}, "ingredients"},
		{"too many courses", `{"title":"x","course_ids":[1,2,3,4,5,6,7,8,9]}`, "course_ids"},
		{"zero course id", `{"title":"x","course_ids":[0]}`, "course_ids"},
		{"string course id", `{"title":"x","course_ids":["1"]}`, "course_ids.0"},
		{"zero cuisine id", `{"title":"x","cuisine_id":0}`, "cuisine_id"},
		{"negative cuisine id", `{"title":"x","cuisine_id":-3}`, "cuisine_id"},
		{"unknown cuisine", `{"title":"x","cuisine_id":987}`, "cuisine_id"},
		{"course as cuisine", `{"title":"x","cuisine_id":` + strconv.FormatInt(tags.dinner, 10) + `}`, "cuisine_id"},
		{"cuisine as course", `{"title":"x","course_ids":[` + strconv.FormatInt(tags.asian, 10) + `]}`, "course_ids"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectError(t, h.call(http.MethodPost, "/api/recipes", alice, c.body), http.StatusBadRequest, "validation", c.field)
		})
	}
	if len(h.db.recipes) != 0 {
		t.Fatalf("invalid input created recipes: %+v", h.db.recipes)
	}
}

func TestIngredientErrorsNameTheLine(t *testing.T) {
	h := newHarness(t)
	e := expectError(t, h.call(http.MethodPost, "/api/recipes", alice,
		`{"title":"x","ingredients":[{"name":"Мука","amount":"200","unit":"г"},{"name":"Сахар","amount":"abc","unit":"г"}]}`),
		http.StatusBadRequest, "validation", "ingredients")
	if !strings.HasPrefix(e.Error.Message, "Ингредиент «Сахар»: ") {
		t.Errorf("message = %q", e.Error.Message)
	}
	e = expectError(t, h.call(http.MethodPost, "/api/recipes", alice,
		`{"title":"x","ingredients":[{"name":"","amount":"-1"}]}`), http.StatusBadRequest, "validation", "ingredients")
	if !strings.HasPrefix(e.Error.Message, "Ингредиент №1: ") {
		t.Errorf("message = %q", e.Error.Message)
	}
}

func TestRecipePatchNewFields(t *testing.T) {
	h := newHarness(t)
	tags := seedTags(t, h)
	r := createRecipe(t, h, map[string]any{
		"title":       "Рамен",
		"cuisine_id":  tags.asian,
		"course_ids":  []int64{tags.dinner},
		"ingredients": []map[string]any{{"name": "Лапша", "amount": "200", "unit": "г"}},
		"nutrition":   map[string]any{"kcal": "120", "protein": "5", "fat": "3", "carbs": "20", "weight_g": 600, "servings": 2},
	})
	path := recipePath(r)

	t.Run("absent fields are untouched", func(t *testing.T) {
		got := decode[recipeJSON](t, h.call(http.MethodPatch, path, alice, `{"title":"Рамен с яйцом"}`))
		if got.CuisineID == nil || len(got.CourseIDs) != 1 || len(got.Ingredients) != 1 || got.Nutrition == nil {
			t.Fatalf("patch touched other fields: %+v", got)
		}
	})
	t.Run("set values", func(t *testing.T) {
		got := decode[recipeJSON](t, h.call(http.MethodPatch, path, alice, map[string]any{
			"cuisine_id":  tags.italian,
			"course_ids":  []int64{tags.breakfast, tags.dinner},
			"ingredients": []map[string]any{{"name": "Яйцо", "amount": "2", "unit": "шт"}, {"name": "Соль", "unit": "по вкусу"}},
			"nutrition":   map[string]any{"kcal": "100", "protein": "1", "fat": "1", "carbs": "1", "weight_g": 500},
		}))
		// КБЖУ without servings keeps the recipe's servings.
		if *got.CuisineID != tags.italian || len(got.CourseIDs) != 2 || got.CourseIDs[0] != tags.breakfast ||
			len(got.Ingredients) != 2 || got.Ingredients[1].Formatted != "по вкусу" ||
			got.Servings == nil || *got.Servings != 2 || got.Nutrition.Servings == nil || *got.Nutrition.Servings != 2 ||
			got.Nutrition.PerDish == nil || got.Nutrition.PerDish.Kcal != "500" ||
			got.Nutrition.PerServing == nil || got.Nutrition.PerServing.Kcal != "250" {
			t.Fatalf("unexpected recipe %+v, nutrition %+v", got, got.Nutrition)
		}
	})
	t.Run("top-level servings", func(t *testing.T) {
		got := decode[recipeJSON](t, h.call(http.MethodPatch, path, alice, `{"servings":5}`))
		if got.Servings == nil || *got.Servings != 5 || *got.Nutrition.Servings != 5 || got.Nutrition.PerServing.Kcal != "100" {
			t.Fatalf("servings = %v, nutrition %+v", got.Servings, got.Nutrition)
		}
		// Older clients send them inside the КБЖУ; the top-level value wins.
		got = decode[recipeJSON](t, h.call(http.MethodPatch, path, alice,
			`{"nutrition":{"kcal":"100","protein":"1","fat":"1","carbs":"1","weight_g":500,"servings":4}}`))
		if *got.Servings != 4 || *got.Nutrition.Servings != 4 {
			t.Fatalf("compat servings = %v, nutrition %+v", *got.Servings, got.Nutrition)
		}
		got = decode[recipeJSON](t, h.call(http.MethodPatch, path, alice,
			`{"servings":3,"nutrition":{"kcal":"100","protein":"1","fat":"1","carbs":"1","weight_g":500,"servings":4}}`))
		if *got.Servings != 3 || *got.Nutrition.Servings != 3 {
			t.Fatalf("top-level servings = %v, nutrition %+v", *got.Servings, got.Nutrition)
		}
		got = decode[recipeJSON](t, h.call(http.MethodPatch, path, alice, `{"servings":null}`))
		if got.Servings != nil || got.Nutrition.Servings != nil || got.Nutrition.PerServing != nil || got.Nutrition.PerDish == nil {
			t.Fatalf("null servings = %v, nutrition %+v", got.Servings, got.Nutrition)
		}
		expectStatus(t, h.call(http.MethodPatch, path, alice, `{"servings":2}`), http.StatusOK)
	})
	t.Run("null clears", func(t *testing.T) {
		rec := h.call(http.MethodPatch, path, alice, `{"cuisine_id":null,"nutrition":null,"servings":null,"course_ids":null,"ingredients":[]}`)
		expectStatus(t, rec, http.StatusOK)
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		for key, want := range map[string]string{"cuisine_id": "null", "nutrition": "null", "servings": "null", "course_ids": "[]", "ingredients": "[]"} {
			if string(raw[key]) != want {
				t.Errorf("%s = %s, want %s", key, raw[key], want)
			}
		}
	})
	t.Run("rejections leave the recipe unchanged", func(t *testing.T) {
		expectStatus(t, h.call(http.MethodPatch, path, alice, map[string]any{"cuisine_id": tags.asian}), http.StatusOK)
		for _, c := range []struct{ body, field string }{
			{`{"cuisine_id":0}`, "cuisine_id"},
			{`{"cuisine_id":"1"}`, "cuisine_id"},
			{`{"course_ids":[1,2,3,4,5,6,7,8,9]}`, "course_ids"},
			{`{"course_ids":{"a":1}}`, "course_ids"},
			{`{"ingredients":[{"name":"x","amount":"1,,5"}]}`, "ingredients"},
			{`{"ingredients":[{"name":"x","extra":1}]}`, "ingredients"},
			{`{"nutrition":{"kcal":"1","fibre":"2"}}`, "nutrition"},
			{`{"nutrition":{"kcal":"1.25"}}`, "kcal"},
			{`{"nutrition":"150"}`, "nutrition"},
			{`{"servings":0}`, "servings"},
			{`{"servings":51}`, "servings"},
			{`{"servings":"4"}`, "servings"},
			{`{"servings":1.5}`, "servings"},
			{`{"cooking":{"count":5}}`, "cooking"},
			{`{"author":{"id":1}}`, "author"},
		} {
			expectError(t, h.call(http.MethodPatch, path, alice, c.body), http.StatusBadRequest, "validation", c.field)
		}
		got := decode[recipeJSON](t, h.call(http.MethodGet, path, alice, nil))
		if got.CuisineID == nil || *got.CuisineID != tags.asian || got.Title != "Рамен с яйцом" || got.Cooking.Count != 0 {
			t.Fatalf("recipe changed by a rejected patch: %+v", got)
		}
	})
}

func TestRecipeListFilters(t *testing.T) {
	h := newHarness(t)
	tags := seedTags(t, h)
	ramen := createRecipe(t, h, map[string]any{"title": "Рамен", "cuisine_id": tags.asian, "course_ids": []int64{tags.dinner}})
	createRecipe(t, h, map[string]any{"title": "Сырники", "course_ids": []int64{tags.breakfast}})
	createRecipe(t, h, map[string]any{"title": "Паста", "cuisine_id": tags.italian, "course_ids": []int64{tags.dinner}})

	list := func(query string) []string {
		t.Helper()
		rec := h.call(http.MethodGet, "/api/recipes"+query, alice, nil)
		expectStatus(t, rec, http.StatusOK)
		var titles []string
		for _, r := range decode[struct {
			Recipes []recipeJSON `json:"recipes"`
		}](t, rec).Recipes {
			titles = append(titles, r.Title)
		}
		return titles
	}
	id := func(v int64) string { return strconv.FormatInt(v, 10) }
	for query, want := range map[string]string{
		"":                           "Паста,Сырники,Рамен",
		"?cuisine=" + id(tags.asian): "Рамен",
		"?course=" + id(tags.dinner): "Паста,Рамен",
		"?course=" + id(tags.dinner) + "&cuisine=" + id(tags.italian): "Паста",
		"?course=" + id(tags.breakfast) + "&q=%D0%A1%D1%8B%D1%80":     "Сырники",
	} {
		if got := strings.Join(list(query), ","); got != want {
			t.Errorf("GET /api/recipes%s = %s, want %s", query, got, want)
		}
	}
	for query, field := range map[string]string{
		"?cuisine=abc": "cuisine", "?cuisine=0": "cuisine", "?cuisine=-1": "cuisine", "?cuisine=01": "cuisine",
		"?course=x": "course", "?course=1.5": "course",
	} {
		expectError(t, h.call(http.MethodGet, "/api/recipes"+query, alice, nil), http.StatusBadRequest, "validation", field)
	}
	if got := decode[recipeJSON](t, h.call(http.MethodGet, recipePath(ramen), alice, nil)); got.CuisineID == nil {
		t.Fatalf("get: %+v", got)
	}
}

func TestRecipeTagLifecycle(t *testing.T) {
	h := newHarness(t)
	tags := seedTags(t, h)
	r := createRecipe(t, h, map[string]any{"title": "Рамен", "cuisine_id": tags.asian, "course_ids": []int64{tags.dinner, tags.breakfast}})

	list := decode[struct {
		Tags []recipeTagJSON `json:"tags"`
	}](t, h.call(http.MethodGet, "/api/recipe-tags", alice, nil)).Tags
	if len(list) != 4 {
		t.Fatalf("list: %+v", list)
	}
	rec := h.call(http.MethodGet, "/api/recipe-tags", alice, nil)
	if !strings.Contains(rec.Body.String(), `{"id":`+strconv.FormatInt(tags.asian, 10)+`,"kind":"cuisine","name":"Азиатская","emoji":"🍜","position":0}`) {
		t.Fatalf("tag shape: %s", rec.Body.String())
	}

	expectError(t, h.call(http.MethodPost, "/api/recipe-tags", alice, map[string]string{"kind": "colour", "name": "Красное", "emoji": "🔴"}), 400, "validation", "kind")
	expectError(t, h.call(http.MethodPost, "/api/recipe-tags", alice, map[string]string{"name": "Без вида", "emoji": "🔴"}), 400, "validation", "kind")
	expectError(t, h.call(http.MethodPost, "/api/recipe-tags", alice, map[string]string{"kind": "cuisine", "name": "азиатская", "emoji": "🥢"}), 409, "conflict", "")
	expectError(t, h.call(http.MethodPost, "/api/recipe-tags", alice, map[string]string{"kind": "cuisine", "name": "Грузинская", "emoji": "abc"}), 400, "validation", "emoji")
	expectError(t, h.call(http.MethodPost, "/api/recipe-tags", alice, `{"kind":"cuisine","name":"Грузинская","emoji":"🍢","position":3}`), 400, "validation", "position")

	path := "/api/recipe-tags/" + strconv.FormatInt(tags.asian, 10)
	rec = h.call(http.MethodPatch, path, alice, map[string]string{"emoji": "🥢"})
	expectStatus(t, rec, http.StatusOK)
	if got := decode[recipeTagJSON](t, rec); got.Name != "Азиатская" || got.Emoji != "🥢" || got.Kind != "cuisine" {
		t.Fatalf("partial patch: %+v", got)
	}
	expectError(t, h.call(http.MethodPatch, path, alice, `{"kind":"course"}`), 400, "validation", "kind")
	expectError(t, h.call(http.MethodPatch, "/api/recipe-tags/999", alice, map[string]string{"name": "X"}), 404, "not_found", "")

	expectStatus(t, h.call(http.MethodDelete, path, alice, nil), http.StatusNoContent)
	expectError(t, h.call(http.MethodDelete, path, alice, nil), 404, "not_found", "")
	got := decode[recipeJSON](t, h.call(http.MethodGet, recipePath(r), alice, nil))
	if got.CuisineID != nil || len(got.CourseIDs) != 2 {
		t.Fatalf("recipe must stay and lose only the deleted tag: %+v", got)
	}
	expectError(t, h.call(http.MethodDelete, "/api/recipe-tags/abc", alice, nil), 404, "not_found", "")
}
