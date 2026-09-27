package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type wishService struct{ *core }

func (s wishService) Create(ctx context.Context, actor domain.UserID, d domain.WishDraft) (domain.Wish, error) {
	w, err := domain.NewWish(d, actor, s.now())
	if err != nil {
		return domain.Wish{}, err
	}
	if err := s.checkCategory(ctx, w.CategoryID); err != nil {
		return domain.Wish{}, err
	}
	w, err = s.repos.InsertWish(ctx, w)
	if err != nil {
		return domain.Wish{}, err
	}
	s.log.Info("wish created", slog.Int64("wish_id", int64(w.ID)), slog.Int64("user_id", int64(actor)))
	s.notify(ctx, "wish_created", actor, func(ctx context.Context, r Recipients) {
		s.notifier.WishCreated(ctx, r, w)
	})
	return w, nil
}

func (s wishService) Get(ctx context.Context, id domain.WishID) (domain.Wish, error) {
	return s.repos.GetWish(ctx, id)
}

func (s wishService) List(ctx context.Context, f domain.WishFilter) ([]domain.Wish, error) {
	if f.Status != nil {
		if _, err := domain.ParseStatus(string(*f.Status)); err != nil {
			return nil, err
		}
	}
	f.Query = strings.TrimSpace(f.Query)
	return s.repos.ListWishes(ctx, f)
}

func (s wishService) Update(ctx context.Context, actor domain.UserID, id domain.WishID, p domain.WishPatch) (domain.Wish, error) {
	if p.CategoryID.Set {
		if err := s.checkCategory(ctx, p.CategoryID.Value); err != nil {
			return domain.Wish{}, err
		}
	}
	now := s.now()
	w, err := s.repos.ModifyWish(ctx, id, func(w *domain.Wish) error {
		return w.Apply(p, now)
	})
	if err != nil {
		return domain.Wish{}, err
	}
	s.log.Info("wish updated", slog.Int64("wish_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return w, nil
}

func (s wishService) SetStatus(ctx context.Context, actor domain.UserID, id domain.WishID, st domain.Status) (domain.Wish, error) {
	now := s.now()
	var fulfilled bool
	w, err := s.repos.ModifyWish(ctx, id, func(w *domain.Wish) error {
		var err error
		fulfilled, err = w.SetStatus(st, now)
		return err
	})
	if err != nil {
		return domain.Wish{}, err
	}
	s.log.Info("wish status set",
		slog.Int64("wish_id", int64(id)),
		slog.String("status", string(w.Status)),
		slog.Int64("user_id", int64(actor)))
	if fulfilled {
		s.notify(ctx, "wish_fulfilled", actor, func(ctx context.Context, r Recipients) {
			s.notifier.WishFulfilled(ctx, r, w)
		})
	}
	return w, nil
}

func (s wishService) Delete(ctx context.Context, actor domain.UserID, id domain.WishID) error {
	keys, err := s.repos.DeleteWish(ctx, id)
	if err != nil {
		return err
	}
	s.deleteFiles(keys...)
	s.log.Info("wish deleted", slog.Int64("wish_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return nil
}

func (s wishService) AddImage(ctx context.Context, actor domain.UserID, id domain.WishID, src io.Reader) (domain.Image, error) {
	w, err := s.repos.GetWish(ctx, id)
	if err != nil {
		return domain.Image{}, err
	}
	if !w.CanAddImage() {
		return domain.Image{}, domain.ErrLimitExceeded
	}
	return s.storeImage(ctx, WishImages(id), domain.MaxImagesPerWish, src)
}

func (s wishService) RemoveImage(ctx context.Context, actor domain.UserID, id domain.WishID, img domain.ImageID) error {
	return s.removeImage(ctx, WishImages(id), img)
}

// checkCategory rejects a reference to a category that does not exist.
func (s wishService) checkCategory(ctx context.Context, id *domain.CategoryID) error {
	if id == nil {
		return nil
	}
	_, err := s.repos.GetCategory(ctx, *id)
	if errors.Is(err, domain.ErrNotFound) {
		return &domain.ValidationError{Field: "category_id", Message: "такой категории нет"}
	}
	return err
}
