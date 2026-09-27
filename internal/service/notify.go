package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// noopNotifier is used when no Notifier is configured.
type noopNotifier struct{}

func (noopNotifier) WishCreated(context.Context, Recipients, domain.Wish)     {}
func (noopNotifier) WishFulfilled(context.Context, Recipients, domain.Wish)   {}
func (noopNotifier) RecipeCreated(context.Context, Recipients, domain.Recipe) {}

// notify resolves the recipients of an event caused by actor and hands them
// to send. Notifications are best effort: every failure is logged and never
// reaches the caller, and send is skipped when nobody can be notified.
func (c *core) notify(ctx context.Context, event string, actor domain.UserID, send func(context.Context, Recipients)) {
	to, err := c.partners(ctx, actor)
	if err != nil {
		c.log.Warn("resolve notification recipients", slog.String("event", event), slog.Any("error", err))
		return
	}
	if len(to) == 0 {
		return
	}
	profile, err := c.repos.GetUser(ctx, actor)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			c.log.Warn("load notification actor", slog.String("event", event), slog.Any("error", err))
		}
		profile = domain.User{ID: actor}
	}

	defer func() {
		if p := recover(); p != nil {
			c.log.Error("notifier panicked", slog.String("event", event), slog.Any("panic", p))
		}
	}()
	// The notifier delivers asynchronously, possibly after the request that
	// caused the event has finished, so it must not inherit its cancellation.
	send(context.WithoutCancel(ctx), Recipients{Actor: profile, To: to})
}

// partners returns the whitelisted users other than of who have an open
// chat with the bot, in whitelist order.
func (c *core) partners(ctx context.Context, of domain.UserID) ([]domain.User, error) {
	users, err := c.knownWhitelisted(ctx)
	if err != nil {
		return nil, err
	}
	out := users[:0]
	for _, u := range users {
		if u.ID != of && u.HasChat {
			out = append(out, u)
		}
	}
	return out, nil
}

// knownWhitelisted returns the stored profiles of whitelisted users in
// whitelist order. Profiles of users removed from the whitelist are skipped.
func (c *core) knownWhitelisted(ctx context.Context) ([]domain.User, error) {
	stored, err := c.repos.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[domain.UserID]domain.User, len(stored))
	for _, u := range stored {
		byID[u.ID] = u
	}
	out := make([]domain.User, 0, len(c.whitelist))
	for _, id := range c.whitelist {
		if u, ok := byID[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}
