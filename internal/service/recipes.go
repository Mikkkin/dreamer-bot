package service

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type recipeService struct{ *core }

func (s recipeService) Create(ctx context.Context, actor domain.UserID, d domain.RecipeDraft) (domain.Recipe, error) {
	r, err := domain.NewRecipe(d, actor, s.now())
	if err != nil {
		return domain.Recipe{}, err
	}
	r, err = s.repos.InsertRecipe(ctx, r)
	if err != nil {
		return domain.Recipe{}, err
	}
	s.log.Info("recipe created", slog.Int64("recipe_id", int64(r.ID)), slog.Int64("user_id", int64(actor)))
	s.notify(ctx, "recipe_created", actor, func(ctx context.Context, rc Recipients) {
		s.notifier.RecipeCreated(ctx, rc, r)
	})
	return r, nil
}

func (s recipeService) Get(ctx context.Context, id domain.RecipeID) (domain.Recipe, error) {
	return s.repos.GetRecipe(ctx, id)
}

func (s recipeService) List(ctx context.Context, f domain.RecipeFilter) ([]domain.Recipe, error) {
	f.Query = strings.TrimSpace(f.Query)
	return s.repos.ListRecipes(ctx, f)
}

func (s recipeService) Random(ctx context.Context) (domain.Recipe, error) {
	return s.repos.RandomRecipe(ctx)
}

func (s recipeService) Update(ctx context.Context, actor domain.UserID, id domain.RecipeID, p domain.RecipePatch) (domain.Recipe, error) {
	now := s.now()
	r, err := s.repos.ModifyRecipe(ctx, id, func(r *domain.Recipe) error {
		return r.Apply(p, now)
	})
	if err != nil {
		return domain.Recipe{}, err
	}
	s.log.Info("recipe updated", slog.Int64("recipe_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return r, nil
}

func (s recipeService) Delete(ctx context.Context, actor domain.UserID, id domain.RecipeID) error {
	keys, err := s.repos.DeleteRecipe(ctx, id)
	if err != nil {
		return err
	}
	s.deleteFiles(keys...)
	s.log.Info("recipe deleted", slog.Int64("recipe_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return nil
}

func (s recipeService) AddImage(ctx context.Context, actor domain.UserID, id domain.RecipeID, src io.Reader) (domain.Image, error) {
	r, err := s.repos.GetRecipe(ctx, id)
	if err != nil {
		return domain.Image{}, err
	}
	if !r.CanAddImage() {
		return domain.Image{}, domain.ErrLimitExceeded
	}
	return s.storeImage(ctx, RecipeImages(id), domain.MaxImagesPerRecipe, src)
}

func (s recipeService) RemoveImage(ctx context.Context, actor domain.UserID, id domain.RecipeID, img domain.ImageID) error {
	return s.removeImage(ctx, RecipeImages(id), img)
}
