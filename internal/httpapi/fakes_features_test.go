package httpapi

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// Fakes of the iteration-2 use cases: savings, cooking history, recipe tags
// and the shopping list. Like the other fakes they apply the domain rules,
// so the adapter can be tested end to end without storage.

// checkTags mimics the service: the cuisine must be an existing cuisine tag
// and every course an existing course tag.
func (db *fakeDB) checkTags(r domain.Recipe) error {
	if id := r.CuisineID; id != nil {
		if t, ok := db.tags[*id]; !ok || t.Kind != domain.TagCuisine {
			return &domain.ValidationError{Field: "cuisine_id", Message: "такой кухни нет"}
		}
	}
	for _, id := range r.CourseIDs {
		if t, ok := db.tags[id]; !ok || t.Kind != domain.TagCourse {
			return &domain.ValidationError{Field: "course_ids", Message: "такого типа блюда нет"}
		}
	}
	return nil
}

// ------------------------------------------------------------- savings --

func (db *fakeDB) refreshSaved(id domain.WishID) {
	w := db.wishes[id]
	w.Saved = nil
	for _, s := range db.savings[id] {
		if w.Saved == nil {
			w.Saved = &domain.Money{Currency: s.Amount.Currency}
		}
		w.Saved.Minor += s.Amount.Minor
	}
	db.wishes[id] = w
}

func (f fakeWishes) AddSaving(_ context.Context, actor domain.UserID, id domain.WishID, amount domain.Money, note string) (domain.Saving, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	f.db.calls.savingAmount = &amount
	w, ok := f.db.wishes[id]
	if !ok {
		return domain.Saving{}, domain.ErrNotFound
	}
	s, err := domain.NewSaving(w, amount, actor, note, f.db.now)
	if err != nil {
		return domain.Saving{}, err
	}
	s.ID = domain.SavingID(f.db.id())
	f.db.savings[id] = append([]domain.Saving{s}, f.db.savings[id]...)
	if w.Status == domain.StatusWant {
		w.Status = domain.StatusProgress
		f.db.wishes[id] = w
	}
	f.db.refreshSaved(id)
	return s, nil
}

func (f fakeWishes) ListSavings(_ context.Context, id domain.WishID) ([]domain.Saving, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if _, ok := f.db.wishes[id]; !ok {
		return nil, domain.ErrNotFound
	}
	return slices.Clone(f.db.savings[id]), nil
}

func (f fakeWishes) RemoveSaving(_ context.Context, _ domain.UserID, id domain.WishID, saving domain.SavingID) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	list := f.db.savings[id]
	i := slices.IndexFunc(list, func(s domain.Saving) bool { return s.ID == saving })
	if i < 0 {
		return domain.ErrNotFound
	}
	f.db.savings[id] = slices.Delete(list, i, i+1)
	f.db.refreshSaved(id)
	return nil
}

// ------------------------------------------------------------- cooking --

func (db *fakeDB) refreshCooking(id domain.RecipeID) {
	r := db.recipes[id]
	var sum domain.CookingSummary
	for _, c := range db.cooks[id] {
		sum.Count++
		if sum.LastCookedAt == nil || c.CookedAt.After(*sum.LastCookedAt) {
			at := c.CookedAt
			sum.LastCookedAt = &at
		}
		for _, rt := range c.Ratings {
			sum.RatingSum += rt.Stars
			sum.RatingCount++
		}
	}
	r.Cooking = sum
	db.recipes[id] = r
}

func (f fakeRecipes) Cook(_ context.Context, actor domain.UserID, id domain.RecipeID, rating *service.RatingInput) (domain.Cook, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	f.db.calls.cooked, f.db.calls.cookRating = true, rating
	if _, ok := f.db.recipes[id]; !ok {
		return domain.Cook{}, domain.ErrNotFound
	}
	c := domain.Cook{ID: domain.CookID(f.db.id()), RecipeID: id, CookedBy: actor, CookedAt: f.db.now}
	if rating != nil {
		r, err := domain.NewRating(actor, rating.Stars, rating.Comment, f.db.now)
		if err != nil {
			return domain.Cook{}, err
		}
		c.Ratings = []domain.Rating{r}
	}
	f.db.cooks[id] = append([]domain.Cook{c}, f.db.cooks[id]...)
	f.db.refreshCooking(id)
	return c, nil
}

func (f fakeRecipes) Rate(_ context.Context, actor domain.UserID, id domain.RecipeID, cook domain.CookID, in service.RatingInput) (domain.Cook, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	cooks := f.db.cooks[id]
	i := slices.IndexFunc(cooks, func(c domain.Cook) bool { return c.ID == cook })
	if i < 0 {
		return domain.Cook{}, domain.ErrNotFound
	}
	r, err := domain.NewRating(actor, in.Stars, in.Comment, f.db.now)
	if err != nil {
		return domain.Cook{}, err
	}
	c := cooks[i]
	c.Ratings = slices.DeleteFunc(slices.Clone(c.Ratings), func(x domain.Rating) bool { return x.UserID == actor })
	c.Ratings = append(c.Ratings, r)
	cooks[i] = c
	f.db.refreshCooking(id)
	return c, nil
}

func (f fakeRecipes) ListCooks(_ context.Context, id domain.RecipeID) ([]domain.Cook, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if _, ok := f.db.recipes[id]; !ok {
		return nil, domain.ErrNotFound
	}
	return slices.Clone(f.db.cooks[id]), nil
}

func (f fakeRecipes) RemoveCook(_ context.Context, _ domain.UserID, id domain.RecipeID, cook domain.CookID) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	cooks := f.db.cooks[id]
	i := slices.IndexFunc(cooks, func(c domain.Cook) bool { return c.ID == cook })
	if i < 0 {
		return domain.ErrNotFound
	}
	f.db.cooks[id] = slices.Delete(cooks, i, i+1)
	f.db.refreshCooking(id)
	return nil
}

// --------------------------------------------------------- recipe tags --

type fakeRecipeTags struct{ db *fakeDB }

func (f fakeRecipeTags) List(context.Context) ([]domain.RecipeTag, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	out := make([]domain.RecipeTag, 0, len(f.db.tags))
	for _, t := range f.db.tags {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b domain.RecipeTag) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Position, b.Position))
	})
	return out, nil
}

func (f fakeRecipeTags) Create(_ context.Context, _ domain.UserID, kind domain.TagKind, name, emoji string) (domain.RecipeTag, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	t, err := domain.NewRecipeTag(kind, name, emoji, f.db.now)
	if err != nil {
		return domain.RecipeTag{}, err
	}
	for _, other := range f.db.tags {
		if other.Kind == t.Kind && strings.EqualFold(other.Name, t.Name) {
			return domain.RecipeTag{}, domain.ErrConflict
		}
		if other.Kind == t.Kind {
			t.Position++
		}
	}
	t.ID = domain.RecipeTagID(f.db.id())
	f.db.tags[t.ID] = t
	return t, nil
}

func (f fakeRecipeTags) Update(_ context.Context, _ domain.UserID, id domain.RecipeTagID, p domain.RecipeTagPatch) (domain.RecipeTag, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	t, ok := f.db.tags[id]
	if !ok {
		return domain.RecipeTag{}, domain.ErrNotFound
	}
	if err := t.Apply(p); err != nil {
		return domain.RecipeTag{}, err
	}
	f.db.tags[id] = t
	return t, nil
}

func (f fakeRecipeTags) Delete(_ context.Context, _ domain.UserID, id domain.RecipeTagID) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if _, ok := f.db.tags[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.db.tags, id)
	for rid, r := range f.db.recipes {
		if r.CuisineID != nil && *r.CuisineID == id {
			r.CuisineID = nil
		}
		r.CourseIDs = slices.DeleteFunc(slices.Clone(r.CourseIDs), func(c domain.RecipeTagID) bool { return c == id })
		f.db.recipes[rid] = r
	}
	return nil
}

// ------------------------------------------------------------ shopping --

type fakeShopping struct{ db *fakeDB }

func (f fakeShopping) List(context.Context) ([]domain.ShoppingItem, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	return f.db.shoppingList(), nil
}

// shoppingList orders unchecked items first, then checked, oldest first.
func (db *fakeDB) shoppingList() []domain.ShoppingItem {
	out := make([]domain.ShoppingItem, 0, len(db.shopping))
	for _, it := range db.shopping {
		out = append(out, it)
	}
	slices.SortFunc(out, func(a, b domain.ShoppingItem) int {
		if a.Checked != b.Checked {
			if a.Checked {
				return 1
			}
			return -1
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

func (f fakeShopping) Add(_ context.Context, actor domain.UserID, drafts []domain.ShoppingDraft) ([]domain.ShoppingItem, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	return f.db.addShopping(actor, drafts)
}

func (db *fakeDB) addShopping(actor domain.UserID, drafts []domain.ShoppingDraft) ([]domain.ShoppingItem, error) {
	out := make([]domain.ShoppingItem, 0, len(drafts))
	for _, d := range drafts {
		it, err := domain.NewShoppingItem(d, actor, db.now)
		if err != nil {
			return nil, err
		}
		merged := false
		for _, existing := range db.shoppingList() {
			if strings.EqualFold(existing.Name, it.Name) && existing.MergeInto(it.Quantity, db.now) {
				db.shopping[existing.ID] = existing
				out = append(out, existing)
				merged = true
				break
			}
		}
		if merged {
			continue
		}
		if len(db.shopping) >= domain.MaxShoppingItems {
			return nil, domain.ErrLimitExceeded
		}
		it.ID = domain.ShoppingItemID(db.id())
		db.shopping[it.ID] = it
		out = append(out, it)
	}
	return out, nil
}

func (f fakeShopping) AddFromRecipe(_ context.Context, actor domain.UserID, id domain.RecipeID, positions []int) ([]domain.ShoppingItem, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	f.db.calls.fromRecipe, f.db.calls.positions = true, positions
	r, ok := f.db.recipes[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if positions == nil {
		for i := range r.Ingredients {
			positions = append(positions, i)
		}
	}
	drafts := make([]domain.ShoppingDraft, 0, len(positions))
	seen := map[int]bool{}
	for _, p := range positions {
		if p < 0 || p >= len(r.Ingredients) {
			return nil, &domain.ValidationError{Field: "positions", Message: "такого ингредиента нет"}
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		ing := r.Ingredients[p]
		drafts = append(drafts, domain.ShoppingDraft{Name: ing.Name, Quantity: ing.Quantity, RecipeID: &id})
	}
	return f.db.addShopping(actor, drafts)
}

func (f fakeShopping) Update(_ context.Context, _ domain.UserID, id domain.ShoppingItemID, p domain.ShoppingPatch) (domain.ShoppingItem, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	f.db.calls.shoppingPatch = &p
	it, ok := f.db.shopping[id]
	if !ok {
		return domain.ShoppingItem{}, domain.ErrNotFound
	}
	if err := it.Apply(p, f.db.now); err != nil {
		return domain.ShoppingItem{}, err
	}
	f.db.shopping[id] = it
	return it, nil
}

func (f fakeShopping) Delete(_ context.Context, _ domain.UserID, id domain.ShoppingItemID) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if _, ok := f.db.shopping[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.db.shopping, id)
	return nil
}

func (f fakeShopping) ClearChecked(context.Context, domain.UserID) (int, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	n := 0
	for id, it := range f.db.shopping {
		if it.Checked {
			delete(f.db.shopping, id)
			n++
		}
	}
	return n, nil
}
