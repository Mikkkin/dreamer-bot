package httpapi

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/stores"
)

// maxItemsPerRequest bounds one POST /api/shopping; the whole list is
// capped by the service (domain.MaxShoppingItems).
const maxItemsPerRequest = 50

func (s *server) listShopping(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	items, err := s.svc.Shopping.List(r.Context())
	if err != nil {
		return err
	}
	return s.writeShoppingItems(w, r, http.StatusOK, items)
}

func (s *server) addShopping(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	var in struct {
		Items []itemInput `json:"items"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	lines, err := shoppingList.parse(in.Items)
	if err != nil {
		return err
	}
	drafts := make([]domain.ShoppingDraft, len(lines))
	for i, l := range lines {
		drafts[i] = domain.ShoppingDraft{Name: l.Name, Quantity: l.Quantity}
	}
	items, err := s.svc.Shopping.Add(r.Context(), u.ID, drafts)
	if err != nil {
		return err
	}
	return s.writeShoppingItems(w, r, http.StatusCreated, items)
}

// addRecipeToShopping adds the recipe's ingredients at the given positions
// (indexes into Recipe.ingredients); {} or {"positions":null} adds them all.
func (s *server) addRecipeToShopping(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Positions *[]int `json:"positions"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	var positions []int
	if in.Positions != nil {
		if positions, err = checkPositions(*in.Positions); err != nil {
			return err
		}
	}
	items, err := s.svc.Shopping.AddFromRecipe(r.Context(), u.ID, id, positions)
	if err != nil {
		return err
	}
	return s.writeShoppingItems(w, r, http.StatusCreated, items)
}

// checkPositions accepts a non-empty list of ingredient indexes within the
// per-recipe limit; the service checks that they exist in this recipe and
// counts a repeated index once.
func checkPositions(positions []int) ([]int, error) {
	switch {
	case len(positions) == 0:
		return nil, badRequest("positions", "Выберите хотя бы один ингредиент.")
	case len(positions) > domain.MaxIngredientsPerRecipe:
		return nil, badRequest("positions", "Не больше "+strconv.Itoa(domain.MaxIngredientsPerRecipe)+" ингредиентов.")
	}
	for _, p := range positions {
		if p < 0 || p >= domain.MaxIngredientsPerRecipe {
			return nil, badRequest("positions", "Неверный список ингредиентов.")
		}
	}
	return slices.Clone(positions), nil
}

// patchShopping changes the name, the quantity or the checkmark. The amount
// and the unit form one quantity, so they are sent together: a request with
// only one of them is rejected rather than guessing the other.
func (s *server) patchShopping(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.ShoppingItemID](r, "id")
	if err != nil {
		return err
	}
	fields, err := decodePatch(w, r, "name", "amount", "unit", "checked")
	if err != nil {
		return err
	}
	patch, err := shoppingPatch(fields)
	if err != nil {
		return err
	}
	item, err := s.svc.Shopping.Update(r.Context(), u.ID, id, patch)
	if err != nil {
		return err
	}
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, p.shoppingItem(item))
	return nil
}

func shoppingPatch(fields patchFields) (domain.ShoppingPatch, error) {
	var (
		p   domain.ShoppingPatch
		err error
	)
	if p.Name, err = optionalField[string](fields, "name"); err != nil {
		return p, err
	}
	if p.Checked, err = optionalField[bool](fields, "checked"); err != nil {
		return p, err
	}
	rawAmount, hasAmount := fields["amount"]
	rawUnit, hasUnit := fields["unit"]
	switch {
	case !hasAmount && !hasUnit:
		return p, nil
	case !hasUnit:
		return p, badRequest("unit", "Укажите единицу вместе с количеством (или null).")
	case !hasAmount:
		return p, badRequest("amount", "Укажите количество вместе с единицей (или null).")
	}
	amount, err := patchNullable[string](rawAmount, "amount")
	if err != nil {
		return p, err
	}
	unit, err := patchNullable[string](rawUnit, "unit")
	if err != nil {
		return p, err
	}
	q, err := parseQuantity(amount.Value, unit.Value)
	if err != nil {
		return p, err
	}
	p.Quantity = domain.Some(q)
	return p, nil
}

// optionalField decodes a non-nullable field when it is present.
func optionalField[T any](fields patchFields, key string) (domain.Optional[T], error) {
	raw, ok := fields[key]
	if !ok {
		return domain.Optional[T]{}, nil
	}
	return patchValue[T](raw, key)
}

func (s *server) deleteShopping(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.ShoppingItemID](r, "id")
	if err != nil {
		return err
	}
	if err := s.svc.Shopping.Delete(r.Context(), u.ID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// clearChecked removes every checked item. It takes no body.
func (s *server) clearChecked(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	removed, err := s.svc.Shopping.ClearChecked(r.Context(), u.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, struct {
		Removed int `json:"removed"`
	}{removed})
	return nil
}

func (s *server) writeShoppingItems(w http.ResponseWriter, r *http.Request, status int, items []domain.ShoppingItem) error {
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, status, struct {
		Items []shoppingItemJSON `json:"items"`
	}{p.shoppingItems(items)})
	return nil
}

// listStores returns the fixed store catalog. The templates are opened by
// the client; the server never fetches them. The ВкусВилл cart is offered
// only while its integration is switched on (VKUSVILL_ENABLED).
func (s *server) listStores(w http.ResponseWriter, _ *http.Request, _ auth.WebAppUser) error {
	all := stores.All()
	out := make([]storeJSON, len(all))
	for i, st := range all {
		st.Cart = st.Cart && s.vkusvill != nil
		out[i] = storeOf(st)
	}
	writeJSON(w, http.StatusOK, struct {
		Stores []storeJSON `json:"stores"`
	}{out})
	return nil
}
