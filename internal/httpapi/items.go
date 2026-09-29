package httpapi

import (
	"strconv"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// itemInput is one ingredient or shopping-list line as the client sends it:
// a name plus an optional decimal amount ("1.5" or "1,5") and unit.
type itemInput struct {
	Name   string  `json:"name"`
	Amount *string `json:"amount"`
	Unit   *string `json:"unit"`
}

// itemList describes how a list of lines is reported in errors.
type itemList struct {
	field    string // JSON field of the list, e.g. "ingredients"
	noun     string // one line in a message, e.g. "Ингредиент"
	limit    int
	tooMany  string
	required bool // an empty list is rejected
	empty    string
}

var (
	ingredientList = itemList{
		field:   "ingredients",
		noun:    "Ингредиент",
		limit:   domain.MaxIngredientsPerRecipe,
		tooMany: "Не больше " + strconv.Itoa(domain.MaxIngredientsPerRecipe) + " ингредиентов.",
	}
	shoppingList = itemList{
		field:    "items",
		noun:     "Позиция",
		limit:    maxItemsPerRequest,
		tooMany:  "Не больше " + strconv.Itoa(maxItemsPerRequest) + " позиций за один раз.",
		required: true,
		empty:    "Добавьте хотя бы одну позицию.",
	}
)

// parse bounds the list and parses every quantity with the domain parser.
// Names are validated by the domain when the lines are stored.
func (l itemList) parse(in []itemInput) ([]domain.Ingredient, error) {
	switch {
	case len(in) > l.limit:
		return nil, badRequest(l.field, l.tooMany)
	case len(in) == 0 && l.required:
		return nil, badRequest(l.field, l.empty)
	}
	out := make([]domain.Ingredient, len(in))
	for i, it := range in {
		// The name is checked here too, so its error names the line.
		if _, err := domain.NormalizeItemName(it.Name); err != nil {
			return nil, l.lineError(i, it.Name, err)
		}
		q, err := parseQuantity(it.Amount, it.Unit)
		if err != nil {
			return nil, l.lineError(i, it.Name, err)
		}
		out[i] = domain.Ingredient{Name: it.Name, Quantity: q}
	}
	return out, nil
}

// lineError names the offending line: «Мука», or its number when the name
// itself is unusable.
func (l itemList) lineError(i int, name string, err error) error {
	v, ok := domain.AsValidation(err)
	if !ok {
		return err
	}
	label := "№" + strconv.Itoa(i+1)
	if n, err := domain.NormalizeItemName(name); err == nil {
		label = "«" + n + "»"
	}
	return badRequest(l.field, l.noun+" "+label+": "+v.Message+".")
}

// parseQuantity reads an optional amount and unit; both absent (or null)
// means no quantity.
func parseQuantity(amount, unit *string) (*domain.Quantity, error) {
	return domain.ParseQuantity(deref(amount), deref(unit))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
