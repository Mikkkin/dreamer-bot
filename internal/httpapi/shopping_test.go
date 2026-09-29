package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type shoppingItems struct {
	Items []shoppingItemJSON `json:"items"`
}

func itemPath(it shoppingItemJSON) string { return "/api/shopping/" + strconv.FormatInt(it.ID, 10) }

func formatted(t *testing.T, amount, unit string) string {
	t.Helper()
	q, err := domain.ParseQuantity(amount, unit)
	if err != nil || q == nil {
		t.Fatalf("ParseQuantity(%q, %q): %v", amount, unit, err)
	}
	return q.Format()
}

func TestShoppingAddAndMerge(t *testing.T) {
	h := newHarness(t)
	rec := h.call(http.MethodPost, "/api/shopping", alice, `{"items":[
		{"name":"Молоко","amount":"500","unit":"мл"},
		{"name":"Хлеб","amount":null,"unit":null},
		{"name":"Соль","unit":"по вкусу"},
		{"name":"Лимоны","amount":"3"}
	]}`)
	expectStatus(t, rec, http.StatusCreated)
	added := decode[shoppingItems](t, rec).Items
	if len(added) != 4 {
		t.Fatalf("added: %+v", added)
	}
	raw := decode[struct {
		Items []map[string]json.RawMessage `json:"items"`
	}](t, rec).Items
	if len(raw) != 4 || len(raw[0]) != 8 {
		t.Fatalf("item shape: %s", rec.Body.String())
	}
	for i, want := range []string{
		mustJSON(t, map[string]any{"amount": "500", "formatted": formatted(t, "500", "мл"), "unit": "мл"}),
		"null",
		mustJSON(t, map[string]any{"amount": nil, "formatted": "по вкусу", "unit": "по вкусу"}),
		mustJSON(t, map[string]any{"amount": "3", "formatted": "3", "unit": nil}),
	} {
		var got any
		_ = json.Unmarshal(raw[i]["quantity"], &got)
		if mustJSON(t, got) != want {
			t.Errorf("item %d quantity = %s, want %s", i, raw[i]["quantity"], want)
		}
	}
	if string(raw[0]["recipe_id"]) != "null" || string(raw[0]["checked"]) != "false" || string(raw[0]["added_by"]) != `{"id":111,"name":"Алиса"}` {
		t.Errorf("item: %v", raw[0])
	}

	// Same name (any case) and unit merges into the unchecked item; an
	// unrelated unit stays a separate item.
	rec = h.call(http.MethodPost, "/api/shopping", bob, `{"items":[{"name":"молоко","amount":"250","unit":"мл"},{"name":"Молоко","amount":"1","unit":"упаковка"}]}`)
	expectStatus(t, rec, http.StatusCreated)
	merged := decode[shoppingItems](t, rec).Items
	if len(merged) != 2 || merged[0].ID != added[0].ID || merged[0].Quantity.Formatted != formatted(t, "750", "мл") || merged[1].ID == added[0].ID {
		t.Fatalf("merge: %+v", merged)
	}
	list := decode[shoppingItems](t, h.call(http.MethodGet, "/api/shopping", alice, nil)).Items
	if len(list) != 5 {
		t.Fatalf("list: %+v", list)
	}
}

func TestShoppingAddValidation(t *testing.T) {
	h := newHarness(t)
	many := make([]map[string]any, maxItemsPerRequest+1)
	for i := range many {
		many[i] = map[string]any{"name": "Позиция " + strconv.Itoa(i)}
	}
	cases := []struct {
		name  string
		body  any
		field string
	}{
		{"no items", `{}`, "items"},
		{"null items", `{"items":null}`, "items"},
		{"empty items", `{"items":[]}`, "items"},
		{"too many items", map[string]any{"items": many}, "items"},
		{"unknown field", `{"items":[{"name":"Хлеб"}],"store":"vkusvill"}`, "store"},
		{"recipe_id is not accepted", `{"items":[{"name":"Хлеб","recipe_id":1}]}`, "recipe_id"},
		{"amount as a number", `{"items":[{"name":"Хлеб","amount":1}]}`, "items.0.amount"},
		{"bad amount", `{"items":[{"name":"Хлеб","amount":"1..5"}]}`, "items"},
		{"negative amount", `{"items":[{"name":"Хлеб","amount":"-2","unit":"шт"}]}`, "items"},
		{"huge amount", `{"items":[{"name":"Хлеб","amount":"100000.01","unit":"г"}]}`, "items"},
		{"unknown unit", `{"items":[{"name":"Хлеб","amount":"1","unit":"буханка"}]}`, "items"},
		{"nameless", `{"items":[{"name":" "}]}`, "items"},
		{"long name", `{"items":[{"name":"` + strings.Repeat("я", domain.MaxItemNameLen+1) + `"}]}`, "items"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectError(t, h.call(http.MethodPost, "/api/shopping", alice, c.body), http.StatusBadRequest, "validation", c.field)
		})
	}
	if len(h.db.shopping) != 0 {
		t.Fatalf("invalid input added items: %+v", h.db.shopping)
	}
	e := expectError(t, h.call(http.MethodPost, "/api/shopping", alice, `{"items":[{"name":"Хлеб"},{"name":"Мука","amount":"abc","unit":"г"}]}`), 400, "validation", "items")
	if !strings.HasPrefix(e.Error.Message, "Позиция «Мука»: ") {
		t.Errorf("message = %q", e.Error.Message)
	}
	expectStatus(t, h.call(http.MethodPost, "/api/shopping", alice, map[string]any{"items": many[:maxItemsPerRequest]}), http.StatusCreated)
}

func TestShoppingListLimit(t *testing.T) {
	h := newHarness(t)
	for id := range domain.MaxShoppingItems {
		h.db.shopping[domain.ShoppingItemID(id+1000)] = domain.ShoppingItem{ID: domain.ShoppingItemID(id + 1000), Name: "x" + strconv.Itoa(id)}
	}
	expectError(t, h.call(http.MethodPost, "/api/shopping", alice, `{"items":[{"name":"Хлеб"}]}`), http.StatusUnprocessableEntity, "limit", "")
}

func TestShoppingPatchDeleteClear(t *testing.T) {
	h := newHarness(t)
	items := decode[shoppingItems](t, h.call(http.MethodPost, "/api/shopping", alice,
		`{"items":[{"name":"Молоко","amount":"1","unit":"л"},{"name":"Хлеб"},{"name":"Сыр","amount":"200","unit":"г"}]}`)).Items
	milk, bread, cheese := items[0], items[1], items[2]

	t.Run("checked", func(t *testing.T) {
		rec := h.call(http.MethodPatch, itemPath(milk), bob, `{"checked":true}`)
		expectStatus(t, rec, http.StatusOK)
		got := decode[shoppingItemJSON](t, rec)
		if !got.Checked || got.Name != "Молоко" || got.Quantity == nil || got.Quantity.Formatted != formatted(t, "1", "л") {
			t.Fatalf("checked patch changed other fields: %+v", got)
		}
		if p := h.db.calls.shoppingPatch; p.Name.Set || p.Quantity.Set || !p.Checked.Set {
			t.Fatalf("patch passed to the service: %+v", p)
		}
		list := decode[shoppingItems](t, h.call(http.MethodGet, "/api/shopping", alice, nil)).Items
		if list[len(list)-1].ID != milk.ID {
			t.Fatalf("checked items go last: %+v", list)
		}
	})
	t.Run("name and quantity", func(t *testing.T) {
		got := decode[shoppingItemJSON](t, h.call(http.MethodPatch, itemPath(bread), alice, `{"name":"Хлеб бородинский","amount":"2","unit":"шт"}`))
		if got.Name != "Хлеб бородинский" || got.Quantity == nil || *got.Quantity.Amount != "2" || *got.Quantity.Unit != "шт" || got.Checked {
			t.Fatalf("patch: %+v", got)
		}
		rec := h.call(http.MethodPatch, itemPath(bread), alice, `{"amount":null,"unit":null}`)
		expectStatus(t, rec, http.StatusOK)
		if !strings.Contains(rec.Body.String(), `"quantity":null`) {
			t.Fatalf("null amount and unit must clear the quantity: %s", rec.Body.String())
		}
		got = decode[shoppingItemJSON](t, h.call(http.MethodPatch, itemPath(bread), alice, `{"amount":null,"unit":"по вкусу"}`))
		if got.Quantity == nil || got.Quantity.Amount != nil || got.Quantity.Formatted != "по вкусу" {
			t.Fatalf("to taste: %+v", got.Quantity)
		}
	})
	t.Run("rejections leave the item unchanged", func(t *testing.T) {
		for _, c := range []struct{ body, field string }{
			{`{"amount":"300"}`, "unit"},
			{`{"unit":"кг"}`, "amount"},
			{`{"amount":"abc","unit":"г"}`, "amount"},
			{`{"amount":"1","unit":"ведро"}`, "unit"},
			{`{"amount":300,"unit":"г"}`, "amount"},
			{`{"name":null}`, "name"},
			{`{"name":""}`, "name"},
			{`{"checked":"yes"}`, "checked"},
			{`{"recipe_id":4}`, "recipe_id"},
			{`{"id":1}`, "id"},
			{`null`, ""},
		} {
			expectError(t, h.call(http.MethodPatch, itemPath(cheese), alice, c.body), http.StatusBadRequest, "validation", c.field)
		}
		list := decode[shoppingItems](t, h.call(http.MethodGet, "/api/shopping", alice, nil)).Items
		i := slices.IndexFunc(list, func(it shoppingItemJSON) bool { return it.ID == cheese.ID })
		if i < 0 || list[i].Name != "Сыр" || list[i].Quantity.Formatted != formatted(t, "200", "г") || list[i].Checked {
			t.Fatalf("item changed by a rejected patch: %+v", list)
		}
	})
	t.Run("missing item", func(t *testing.T) {
		expectError(t, h.call(http.MethodPatch, "/api/shopping/999", alice, `{"checked":true}`), 404, "not_found", "")
		expectError(t, h.call(http.MethodDelete, "/api/shopping/999", alice, nil), 404, "not_found", "")
		expectError(t, h.call(http.MethodPatch, "/api/shopping/abc", alice, `{"checked":true}`), 404, "not_found", "")
	})
	t.Run("delete and clear checked", func(t *testing.T) {
		expectStatus(t, h.call(http.MethodPatch, itemPath(cheese), alice, `{"checked":true}`), http.StatusOK)
		rec := h.call(http.MethodPost, "/api/shopping/clear-checked", alice, nil)
		expectStatus(t, rec, http.StatusOK)
		if got := strings.TrimSpace(rec.Body.String()); got != `{"removed":2}` {
			t.Fatalf("clear-checked = %s", got)
		}
		expectStatus(t, h.call(http.MethodDelete, itemPath(bread), alice, nil), http.StatusNoContent)
		rec = h.call(http.MethodGet, "/api/shopping", alice, nil)
		if got := strings.TrimSpace(rec.Body.String()); got != `{"items":[]}` {
			t.Fatalf("empty list must be [] not null: %s", got)
		}
		if got := strings.TrimSpace(h.call(http.MethodPost, "/api/shopping/clear-checked", alice, nil).Body.String()); got != `{"removed":0}` {
			t.Fatalf("clear-checked on an empty list = %s", got)
		}
	})
}

func TestRecipeToShopping(t *testing.T) {
	h := newHarness(t)
	r := createRecipe(t, h, map[string]any{"title": "Блины", "ingredients": []map[string]any{
		{"name": "Молоко", "amount": "500", "unit": "мл"},
		{"name": "Яйца", "amount": "2", "unit": "шт"},
		{"name": "Соль", "unit": "по вкусу"},
	}})
	path := recipePath(r) + "/shopping"

	rec := h.call(http.MethodPost, path, alice, `{"positions":[0,2]}`)
	expectStatus(t, rec, http.StatusCreated)
	items := decode[shoppingItems](t, rec).Items
	if !slices.Equal(h.db.calls.positions, []int{0, 2}) || len(items) != 2 || items[0].Name != "Молоко" || items[1].Name != "Соль" {
		t.Fatalf("positions: %v -> %+v", h.db.calls.positions, items)
	}
	if items[0].RecipeID == nil || *items[0].RecipeID != r.ID {
		t.Fatalf("items must remember the recipe: %+v", items[0])
	}

	for _, body := range []string{`{}`, `{"positions":null}`} {
		h.db.calls = fakeCalls{}
		rec = h.call(http.MethodPost, path, alice, body)
		expectStatus(t, rec, http.StatusCreated)
		if !h.db.calls.fromRecipe || h.db.calls.positions != nil {
			t.Fatalf("%s must add all ingredients (nil positions), got %v", body, h.db.calls.positions)
		}
	}
	list := decode[shoppingItems](t, h.call(http.MethodGet, "/api/shopping", alice, nil)).Items
	if len(list) != 3 || list[0].Quantity.Formatted != formatted(t, "1500", "мл") {
		t.Fatalf("ingredients must merge into the list: %+v", list)
	}

	tooMany := make([]int, domain.MaxIngredientsPerRecipe+1)
	for i := range tooMany {
		tooMany[i] = i
	}
	h.db.calls = fakeCalls{}
	for _, c := range []struct {
		body  any
		field string
	}{
		{`{"positions":[]}`, "positions"},
		{`{"positions":[-1]}`, "positions"},
		{`{"positions":[50]}`, "positions"},
		{map[string]any{"positions": tooMany}, "positions"},
		{`{"positions":[0.5]}`, "positions.0"},
		{`{"positions":"all"}`, "positions"},
		{`{"positions":[0],"all":true}`, "all"},
	} {
		expectError(t, h.call(http.MethodPost, path, alice, c.body), http.StatusBadRequest, "validation", c.field)
	}
	if h.db.calls.fromRecipe {
		t.Fatal("invalid positions must be rejected before the use case runs")
	}
	// A repeated index is passed through; the service counts it once.
	rec = h.call(http.MethodPost, path, alice, `{"positions":[1,1]}`)
	expectStatus(t, rec, http.StatusCreated)
	if !slices.Equal(h.db.calls.positions, []int{1, 1}) || len(decode[shoppingItems](t, rec).Items) != 1 {
		t.Fatalf("repeated position: %v -> %s", h.db.calls.positions, rec.Body.String())
	}
	// In range for the edge, but not in this recipe: the service decides.
	expectError(t, h.call(http.MethodPost, path, alice, `{"positions":[7]}`), http.StatusBadRequest, "validation", "positions")
	expectError(t, h.call(http.MethodPost, "/api/recipes/999/shopping", alice, `{}`), http.StatusNotFound, "not_found", "")
	expectError(t, h.call(http.MethodPost, path, alice, nil), http.StatusUnsupportedMediaType, "unsupported_media", "")
}

func TestStores(t *testing.T) {
	h := newHarness(t, withVkusvill(&fakeVkusvill{}))
	rec := h.call(http.MethodGet, "/api/stores", alice, nil)
	expectStatus(t, rec, http.StatusOK)
	var raw struct {
		Stores []map[string]any `json:"stores"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Stores) < 3 {
		t.Fatalf("stores: %s", rec.Body.String())
	}
	first := raw.Stores[0]
	if first["id"] != "vkusvill" || first["name"] != "ВкусВилл" || first["emoji"] == "" || first["cart"] != true || first["opens_app"] != false || len(first) != 6 {
		t.Fatalf("store shape: %v", first)
	}
	carts := 0
	for _, s := range raw.Stores {
		tmpl, _ := s["search_url_template"].(string)
		if !strings.HasPrefix(tmpl, "https://") || strings.Count(tmpl, "{q}") != 1 {
			t.Errorf("template %q", tmpl)
		}
		if _, ok := s["opens_app"].(bool); !ok {
			t.Errorf("opens_app must be a boolean: %v", s)
		}
		if s["cart"] == true {
			carts++
		}
	}
	if carts != 1 {
		t.Errorf("exactly one store (ВкусВилл) has a cart integration, got %d", carts)
	}
}

func TestStoresWithoutVkusvillOfferNoCart(t *testing.T) {
	h := newHarness(t)
	got := decode[struct {
		Stores []storeJSON `json:"stores"`
	}](t, h.call(http.MethodGet, "/api/stores", alice, nil))
	if len(got.Stores) == 0 || got.Stores[0].ID != "vkusvill" {
		t.Fatalf("stores: %+v", got.Stores)
	}
	for _, s := range got.Stores {
		if s.Cart {
			t.Errorf("%s offers a cart while ВкусВилл is switched off", s.ID)
		}
	}
}
