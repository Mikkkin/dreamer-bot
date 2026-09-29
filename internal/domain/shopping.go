package domain

import "time"

// ShoppingItemID identifies a shopping-list item.
type ShoppingItemID int64

// MaxShoppingItems caps the shared shopping list.
const MaxShoppingItems = 300

// ShoppingItem is one line of the couple's shared shopping list. Items added
// from a recipe remember it, so the list can show where a line came from.
type ShoppingItem struct {
	ID        ShoppingItemID
	Name      string
	Quantity  *Quantity
	Checked   bool
	RecipeID  *RecipeID
	AddedBy   UserID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ShoppingDraft is the input for a new shopping-list item.
type ShoppingDraft struct {
	Name     string
	Quantity *Quantity
	RecipeID *RecipeID
}

// NewShoppingItem validates a draft.
func NewShoppingItem(d ShoppingDraft, user UserID, now time.Time) (ShoppingItem, error) {
	ing, err := NormalizeIngredients([]Ingredient{{Name: d.Name, Quantity: d.Quantity}})
	if err != nil {
		return ShoppingItem{}, invalid("name", "у позиции должно быть название (до 80 символов) и верное количество")
	}
	now = now.UTC()
	return ShoppingItem{
		Name:      ing[0].Name,
		Quantity:  ing[0].Quantity,
		RecipeID:  d.RecipeID,
		AddedBy:   user,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ShoppingPatch is a partial update of a shopping-list item.
type ShoppingPatch struct {
	Name     Optional[string]
	Quantity Optional[*Quantity]
	Checked  Optional[bool]
}

// Apply validates the patch and only then mutates the item.
func (it *ShoppingItem) Apply(p ShoppingPatch, now time.Time) error {
	next := *it
	name, qty := next.Name, next.Quantity
	if p.Name.Set {
		name = p.Name.Value
	}
	if p.Quantity.Set {
		qty = p.Quantity.Value
	}
	ing, err := NormalizeIngredients([]Ingredient{{Name: name, Quantity: qty}})
	if err != nil {
		return invalid("name", "у позиции должно быть название (до 80 символов) и верное количество")
	}
	next.Name, next.Quantity = ing[0].Name, ing[0].Quantity
	if p.Checked.Set {
		next.Checked = p.Checked.Value
	}
	next.UpdatedAt = now.UTC()
	*it = next
	return nil
}

// MergeInto adds the quantity of a newly added item to an existing unchecked
// item with the same name and the same or a related unit: "молоко 500 мл" +
// "молоко 250 мл" become 750 мл, "молоко 200 мл" + "молоко 0,5 л" become
// 700 мл, while "яйца 2 шт" + "яйца 100 г" stay separate. It reports whether
// the items could be merged.
func (it *ShoppingItem) MergeInto(q *Quantity, now time.Time) bool {
	switch {
	case it.Checked:
		return false
	case it.Quantity == nil && q == nil:
		return true
	case it.Quantity == nil || q == nil:
		return false
	case it.Quantity.Hundredths == 0 || q.Hundredths == 0: // "по вкусу" twice
		return it.Quantity.Unit == q.Unit && it.Quantity.Hundredths == q.Hundredths
	}
	sum, ok := addQuantities(*it.Quantity, *q)
	if !ok {
		return false
	}
	it.Quantity = &sum
	it.UpdatedAt = now.UTC()
	return true
}
