package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
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

func (r *recordingNotifier) RecipeUpdated(context.Context, service.Recipients, domain.Recipe) {
	r.kinds = append(r.kinds, "recipe_updated")
}

func (r *recordingNotifier) RecipeCooked(context.Context, service.Recipients, domain.Recipe, domain.Cook) {
	r.kinds = append(r.kinds, "recipe_cooked")
}

func (r *recordingNotifier) RecipeRated(context.Context, service.Recipients, domain.Recipe, domain.Cook, domain.Rating) {
	r.kinds = append(r.kinds, "recipe_rated")
}

func (r *recordingNotifier) WishSaved(context.Context, service.Recipients, domain.Wish, domain.Saving) {
	r.kinds = append(r.kinds, "wish_saved")
}

func TestLazyNotifierDropsUntilSetThenForwards(t *testing.T) {
	var logs bytes.Buffer
	n := &lazyNotifier{log: slog.New(slog.NewTextHandler(&logs, nil))}
	ctx := context.Background()
	r := service.Recipients{}
	all := func() {
		n.WishCreated(ctx, r, domain.Wish{})
		n.WishFulfilled(ctx, r, domain.Wish{})
		n.RecipeCreated(ctx, r, domain.Recipe{})
		n.RecipeUpdated(ctx, r, domain.Recipe{})
		n.RecipeCooked(ctx, r, domain.Recipe{}, domain.Cook{})
		n.RecipeRated(ctx, r, domain.Recipe{}, domain.Cook{}, domain.Rating{})
		n.WishSaved(ctx, r, domain.Wish{}, domain.Saving{})
	}
	all() // dropped before the bot exists; must not panic
	for _, kind := range []string{"recipe_updated", "recipe_cooked", "recipe_rated", "wish_saved"} {
		if !strings.Contains(logs.String(), "kind="+kind) {
			t.Errorf("dropped %s notice must be logged: %s", kind, logs.String())
		}
	}

	rec := &recordingNotifier{}
	n.set(rec)
	all()
	want := []string{"wish_created", "wish_fulfilled", "recipe_created", "recipe_updated", "recipe_cooked", "recipe_rated", "wish_saved"}
	if !slices.Equal(rec.kinds, want) {
		t.Fatalf("forwarded %v, want %v", rec.kinds, want)
	}
}
