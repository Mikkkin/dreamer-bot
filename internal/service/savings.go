package service

import (
	"context"
	"log/slog"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// AddSaving validates the contribution against the stored wish (its
// savings currency) and stores it. A wish that is still «Хотим» moves to
// «Копим» in the same transaction.
func (s wishService) AddSaving(ctx context.Context, actor domain.UserID, id domain.WishID, amount domain.Money, note string) (domain.Saving, error) {
	now := s.now()
	w, saving, err := s.repos.InsertSaving(ctx, id, func(w *domain.Wish) (domain.Saving, error) {
		saving, err := domain.NewSaving(*w, amount, actor, note, now)
		if err != nil {
			return domain.Saving{}, err
		}
		if w.Status == domain.StatusWant {
			if _, err := w.SetStatus(domain.StatusProgress, now); err != nil {
				return domain.Saving{}, err
			}
		}
		return saving, nil
	})
	if err != nil {
		return domain.Saving{}, err
	}
	s.log.Info("saving added",
		slog.Int64("wish_id", int64(id)),
		slog.Int64("saving_id", int64(saving.ID)),
		slog.Int64("user_id", int64(actor)))
	s.notify(ctx, "wish_saved", actor, func(ctx context.Context, r Recipients) {
		s.notifier.WishSaved(ctx, r, w, saving)
	})
	return saving, nil
}

func (s wishService) ListSavings(ctx context.Context, id domain.WishID) ([]domain.Saving, error) {
	return s.repos.ListSavings(ctx, id)
}

func (s wishService) RemoveSaving(ctx context.Context, actor domain.UserID, id domain.WishID, saving domain.SavingID) error {
	if err := s.repos.DeleteSaving(ctx, id, saving); err != nil {
		return err
	}
	s.log.Info("saving removed",
		slog.Int64("wish_id", int64(id)),
		slog.Int64("saving_id", int64(saving)),
		slog.Int64("user_id", int64(actor)))
	return nil
}
