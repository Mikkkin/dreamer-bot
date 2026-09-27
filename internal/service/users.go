package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// touchInterval bounds how often an unchanged profile is written: every bot
// update and API request touches the user, and a write per request would be
// pure overhead.
const touchInterval = time.Hour

type userService struct{ *core }

func (s userService) Touch(ctx context.Context, u domain.User) error {
	if u.ID <= 0 {
		return &domain.ValidationError{Field: "user_id", Message: "некорректный пользователь"}
	}
	if !s.whitelisted(u.ID) {
		// The adapters drop foreign users before they get here; refusing to
		// store their profiles keeps strangers' data out of the database even
		// if an adapter gets this wrong.
		s.log.Debug("skip touch of a non-whitelisted user", slog.Int64("user_id", int64(u.ID)))
		return nil
	}
	now := s.now()
	stored, err := s.repos.GetUser(ctx, u.ID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
	case err != nil:
		return err
	default:
		u.HasChat = u.HasChat || stored.HasChat
		if sameProfile(stored, u) && now.Sub(stored.UpdatedAt) < touchInterval {
			return nil
		}
	}
	u.UpdatedAt = now
	return s.repos.UpsertUser(ctx, u)
}

func sameProfile(a, b domain.User) bool {
	return a.FirstName == b.FirstName && a.LastName == b.LastName &&
		a.Username == b.Username && a.HasChat == b.HasChat
}

func (s userService) Get(ctx context.Context, id domain.UserID) (domain.User, error) {
	return s.repos.GetUser(ctx, id)
}

func (s userService) List(ctx context.Context) ([]domain.User, error) {
	return s.knownWhitelisted(ctx)
}

func (s userService) Partners(ctx context.Context, of domain.UserID) ([]domain.User, error) {
	return s.partners(ctx, of)
}
