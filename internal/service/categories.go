package service

import (
	"context"
	"log/slog"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type categoryService struct{ *core }

func (s categoryService) List(ctx context.Context) ([]domain.Category, error) {
	return s.repos.ListCategories(ctx)
}

func (s categoryService) Get(ctx context.Context, id domain.CategoryID) (domain.Category, error) {
	return s.repos.GetCategory(ctx, id)
}

func (s categoryService) Create(ctx context.Context, actor domain.UserID, name, emoji string) (domain.Category, error) {
	c, err := domain.NewCategory(name, emoji, s.now())
	if err != nil {
		return domain.Category{}, err
	}
	c, err = s.repos.InsertCategory(ctx, c)
	if err != nil {
		return domain.Category{}, err
	}
	s.log.Info("category created", slog.Int64("category_id", int64(c.ID)), slog.Int64("user_id", int64(actor)))
	return c, nil
}

func (s categoryService) Update(ctx context.Context, actor domain.UserID, id domain.CategoryID, p domain.CategoryPatch) (domain.Category, error) {
	c, err := s.repos.ModifyCategory(ctx, id, func(c *domain.Category) error { return c.Apply(p) })
	if err != nil {
		return domain.Category{}, err
	}
	s.log.Info("category updated", slog.Int64("category_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return c, nil
}

func (s categoryService) Delete(ctx context.Context, actor domain.UserID, id domain.CategoryID) error {
	if err := s.repos.DeleteCategory(ctx, id); err != nil {
		return err
	}
	s.log.Info("category deleted", slog.Int64("category_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return nil
}
