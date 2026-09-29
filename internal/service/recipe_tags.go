package service

import (
	"context"
	"log/slog"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type recipeTagService struct{ *core }

func (s recipeTagService) List(ctx context.Context) ([]domain.RecipeTag, error) {
	return s.repos.ListRecipeTags(ctx)
}

func (s recipeTagService) Create(ctx context.Context, actor domain.UserID, kind domain.TagKind, name, emoji string) (domain.RecipeTag, error) {
	t, err := domain.NewRecipeTag(kind, name, emoji, s.now())
	if err != nil {
		return domain.RecipeTag{}, err
	}
	t, err = s.repos.InsertRecipeTag(ctx, t)
	if err != nil {
		return domain.RecipeTag{}, err
	}
	s.log.Info("recipe tag created",
		slog.Int64("tag_id", int64(t.ID)),
		slog.String("kind", string(t.Kind)),
		slog.Int64("user_id", int64(actor)))
	return t, nil
}

func (s recipeTagService) Update(ctx context.Context, actor domain.UserID, id domain.RecipeTagID, p domain.RecipeTagPatch) (domain.RecipeTag, error) {
	t, err := s.repos.ModifyRecipeTag(ctx, id, func(t *domain.RecipeTag) error { return t.Apply(p) })
	if err != nil {
		return domain.RecipeTag{}, err
	}
	s.log.Info("recipe tag updated", slog.Int64("tag_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return t, nil
}

func (s recipeTagService) Delete(ctx context.Context, actor domain.UserID, id domain.RecipeTagID) error {
	if err := s.repos.DeleteRecipeTag(ctx, id); err != nil {
		return err
	}
	s.log.Info("recipe tag deleted", slog.Int64("tag_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return nil
}
