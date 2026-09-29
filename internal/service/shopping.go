package service

import (
	"context"
	"log/slog"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type shoppingService struct{ *core }

func (s shoppingService) List(ctx context.Context) ([]domain.ShoppingItem, error) {
	return s.repos.ListShoppingItems(ctx)
}

// Add validates every draft and stores them in one transaction: each one
// merges into an unchecked item with the same name and unit when the
// quantities add up (domain.ShoppingItem.MergeInto), otherwise it becomes a
// new item as long as the list stays within domain.MaxShoppingItems.
func (s shoppingService) Add(ctx context.Context, actor domain.UserID, drafts []domain.ShoppingDraft) ([]domain.ShoppingItem, error) {
	switch {
	case len(drafts) == 0:
		return nil, &domain.ValidationError{Field: "items", Message: "добавьте хотя бы одну позицию"}
	case len(drafts) > domain.MaxShoppingItems:
		return nil, domain.ErrLimitExceeded
	}
	now := s.now()
	items := make([]domain.ShoppingItem, 0, len(drafts))
	for _, d := range drafts {
		it, err := domain.NewShoppingItem(d, actor, now)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	out, err := s.repos.AddShoppingItems(ctx, items, domain.MaxShoppingItems,
		func(stored *domain.ShoppingItem, added domain.ShoppingItem) bool {
			return stored.MergeInto(added.Quantity, now)
		})
	if err != nil {
		return nil, err
	}
	s.log.Info("shopping items added", slog.Int("drafts", len(drafts)), slog.Int("items", len(out)), slog.Int64("user_id", int64(actor)))
	return out, nil
}

// AddFromRecipe adds the ingredients at positions (nil = all) of the recipe.
// Every position must point at an ingredient; repeated positions count once.
func (s shoppingService) AddFromRecipe(ctx context.Context, actor domain.UserID, id domain.RecipeID, positions []int) ([]domain.ShoppingItem, error) {
	r, err := s.repos.GetRecipe(ctx, id)
	if err != nil {
		return nil, err
	}
	selected, err := selectIngredients(r.Ingredients, positions)
	if err != nil {
		return nil, err
	}
	drafts := make([]domain.ShoppingDraft, 0, len(selected))
	for _, ing := range selected {
		d := domain.ShoppingDraft{Name: ing.Name, RecipeID: &id}
		if ing.Quantity != nil {
			q := *ing.Quantity
			d.Quantity = &q
		}
		drafts = append(drafts, d)
	}
	return s.Add(ctx, actor, drafts)
}

func selectIngredients(all []domain.Ingredient, positions []int) ([]domain.Ingredient, error) {
	if len(all) == 0 {
		return nil, &domain.ValidationError{Field: "positions", Message: "в рецепте нет ингредиентов"}
	}
	if positions == nil {
		return all, nil
	}
	if len(positions) == 0 {
		return nil, &domain.ValidationError{Field: "positions", Message: "выберите ингредиенты"}
	}
	out := make([]domain.Ingredient, 0, len(positions))
	seen := make(map[int]bool, len(positions))
	for _, p := range positions {
		if p < 0 || p >= len(all) {
			return nil, &domain.ValidationError{Field: "positions", Message: "такого ингредиента нет в рецепте"}
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, all[p])
		}
	}
	return out, nil
}

func (s shoppingService) Update(ctx context.Context, actor domain.UserID, id domain.ShoppingItemID, p domain.ShoppingPatch) (domain.ShoppingItem, error) {
	now := s.now()
	it, err := s.repos.ModifyShoppingItem(ctx, id, func(it *domain.ShoppingItem) error {
		return it.Apply(p, now)
	})
	if err != nil {
		return domain.ShoppingItem{}, err
	}
	s.log.Info("shopping item updated", slog.Int64("item_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return it, nil
}

func (s shoppingService) Delete(ctx context.Context, actor domain.UserID, id domain.ShoppingItemID) error {
	if err := s.repos.DeleteShoppingItem(ctx, id); err != nil {
		return err
	}
	s.log.Info("shopping item deleted", slog.Int64("item_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return nil
}

func (s shoppingService) ClearChecked(ctx context.Context, actor domain.UserID) (int, error) {
	n, err := s.repos.DeleteCheckedShoppingItems(ctx)
	if err != nil {
		return 0, err
	}
	s.log.Info("checked shopping items cleared", slog.Int("removed", n), slog.Int64("user_id", int64(actor)))
	return n, nil
}
