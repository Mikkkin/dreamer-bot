package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/bot"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSupervisorStopsOnRejectedToken(t *testing.T) {
	calls := 0
	s := newBotSupervisor(bot.Options{}, discard())
	s.newBot = func(context.Context, bot.Options) (*bot.Bot, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("dial tcp: i/o timeout") // transient: retried
		}
		return nil, bot.ErrTokenRejected
	}
	err := s.Run(context.Background(), nil)
	if !errors.Is(err, bot.ErrTokenRejected) || calls != 2 {
		t.Fatalf("want ErrTokenRejected after one retry, got %v after %d calls", err, calls)
	}
}

func TestSupervisorReturnsQuietlyOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := newBotSupervisor(bot.Options{}, discard())
	s.newBot = func(context.Context, bot.Options) (*bot.Bot, error) {
		cancel()
		return nil, errors.New("network down")
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown while retrying must not be an error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestSupervisorRemembersURLBeforeBotExists(t *testing.T) {
	s := newBotSupervisor(bot.Options{}, discard())
	s.SetWebAppURL(context.Background(), "https://a.trycloudflare.com/")
	if s.webAppURL != "https://a.trycloudflare.com/" {
		t.Fatalf("URL must be kept until the bot starts, got %q", s.webAppURL)
	}
}

type recordingNotifier struct{ kinds []string }

func (r *recordingNotifier) WishCreated(context.Context, service.Recipients, domain.Wish) {
	r.kinds = append(r.kinds, "wish_created")
}

func (r *recordingNotifier) WishFulfilled(context.Context, service.Recipients, domain.Wish) {
	r.kinds = append(r.kinds, "wish_fulfilled")
}

func (r *recordingNotifier) RecipeCreated(context.Context, service.Recipients, domain.Recipe) {
	r.kinds = append(r.kinds, "recipe_created")
}

func TestLazyNotifierDropsUntilSetThenForwards(t *testing.T) {
	n := &lazyNotifier{log: discard()}
	ctx := context.Background()
	n.WishCreated(ctx, service.Recipients{}, domain.Wish{}) // dropped, must not panic

	rec := &recordingNotifier{}
	n.set(rec)
	n.WishCreated(ctx, service.Recipients{}, domain.Wish{})
	n.WishFulfilled(ctx, service.Recipients{}, domain.Wish{})
	n.RecipeCreated(ctx, service.Recipients{}, domain.Recipe{})
	if len(rec.kinds) != 3 {
		t.Fatalf("want 3 forwarded notices, got %v", rec.kinds)
	}
}
