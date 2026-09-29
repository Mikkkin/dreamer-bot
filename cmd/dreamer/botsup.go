package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/bot"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// botSupervisor owns the Telegram bot's lifecycle. The HTTP server does not
// wait for it: while Telegram is unreachable the Mini App keeps working and
// the supervisor keeps retrying in the background.
type botSupervisor struct {
	opts     bot.Options
	notifier *lazyNotifier
	log      *slog.Logger
	newBot   func(context.Context, bot.Options) (*bot.Bot, error)

	mu        sync.Mutex
	bot       *bot.Bot
	webAppURL string // latest discovered URL, applied once the bot exists
}

func newBotSupervisor(opts bot.Options, log *slog.Logger) *botSupervisor {
	return &botSupervisor{opts: opts, notifier: &lazyNotifier{log: log}, log: log, newBot: bot.New}
}

// Notifier is handed to the services before the bot exists.
func (s *botSupervisor) Notifier() service.Notifier { return s.notifier }

// SetWebAppURL records the URL and forwards it to the bot when it is running.
func (s *botSupervisor) SetWebAppURL(ctx context.Context, url string) {
	s.mu.Lock()
	s.webAppURL = url
	b := s.bot
	s.mu.Unlock()
	if b != nil {
		b.SetWebAppURL(ctx, url)
	}
}

// Run starts the bot (retrying transient failures with backoff), attaches it
// to the services and keeps it polling until ctx is done. It returns an error
// only when retrying cannot help, i.e. Telegram rejected the token.
func (s *botSupervisor) Run(ctx context.Context, services *service.Services) error {
	b, err := s.start(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	b.Attach(services)
	s.notifier.set(b.Notifier())

	s.mu.Lock()
	s.bot = b
	url := s.webAppURL
	s.mu.Unlock()
	if url != "" {
		b.SetWebAppURL(ctx, url)
	}
	keepPolling(ctx, b, s.log)
	return nil
}

func (s *botSupervisor) start(ctx context.Context) (*bot.Bot, error) {
	delay := botRetryMin
	for attempt := 1; ; attempt++ {
		b, err := s.newBot(ctx, s.opts)
		if err == nil {
			return b, nil
		}
		if errors.Is(err, bot.ErrTokenRejected) {
			return nil, fmt.Errorf("check BOT_TOKEN: %w", err)
		}
		s.log.Warn("telegram is unreachable; retrying", "attempt", attempt, "retry_in", delay.String(), "err", err)
		if !sleep(ctx, delay) {
			return nil, ctx.Err()
		}
		delay = min(2*delay, botRetryMax)
	}
}

// lazyNotifier forwards to the bot's notifier once the bot is running and
// drops notices before that (they are best-effort by contract).
type lazyNotifier struct {
	target atomic.Pointer[service.Notifier]
	log    *slog.Logger
}

var _ service.Notifier = (*lazyNotifier)(nil)

func (n *lazyNotifier) set(target service.Notifier) { n.target.Store(&target) }

func (n *lazyNotifier) get(kind string) service.Notifier {
	if t := n.target.Load(); t != nil {
		return *t
	}
	n.log.Warn("telegram bot not connected yet; notification dropped", "kind", kind)
	return nil
}

func (n *lazyNotifier) WishCreated(ctx context.Context, r service.Recipients, w domain.Wish) {
	if t := n.get("wish_created"); t != nil {
		t.WishCreated(ctx, r, w)
	}
}

func (n *lazyNotifier) WishFulfilled(ctx context.Context, r service.Recipients, w domain.Wish) {
	if t := n.get("wish_fulfilled"); t != nil {
		t.WishFulfilled(ctx, r, w)
	}
}

func (n *lazyNotifier) RecipeCreated(ctx context.Context, r service.Recipients, rec domain.Recipe) {
	if t := n.get("recipe_created"); t != nil {
		t.RecipeCreated(ctx, r, rec)
	}
}

func (n *lazyNotifier) RecipeUpdated(ctx context.Context, r service.Recipients, rec domain.Recipe) {
	if t := n.get("recipe_updated"); t != nil {
		t.RecipeUpdated(ctx, r, rec)
	}
}

func (n *lazyNotifier) RecipeCooked(ctx context.Context, r service.Recipients, rec domain.Recipe, cook domain.Cook) {
	if t := n.get("recipe_cooked"); t != nil {
		t.RecipeCooked(ctx, r, rec, cook)
	}
}

func (n *lazyNotifier) RecipeRated(ctx context.Context, r service.Recipients, rec domain.Recipe, cook domain.Cook, rating domain.Rating) {
	if t := n.get("recipe_rated"); t != nil {
		t.RecipeRated(ctx, r, rec, cook, rating)
	}
}

func (n *lazyNotifier) WishSaved(ctx context.Context, r service.Recipients, w domain.Wish, s domain.Saving) {
	if t := n.get("wish_saved"); t != nil {
		t.WishSaved(ctx, r, w, s)
	}
}

// keepPolling runs the bot until ctx is done. A polling failure is logged
// and retried with backoff; it never takes the HTTP server down.
func keepPolling(ctx context.Context, b *bot.Bot, log *slog.Logger) {
	delay := botRetryMin
	for {
		started := time.Now()
		err := runBotOnce(ctx, b)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("polling returned before shutdown")
		}
		if time.Since(started) >= botHealthyRun {
			delay = botRetryMin
		}
		log.Error("telegram bot stopped; restarting", "err", err, "retry_in", delay.String())
		if !sleep(ctx, delay) {
			return
		}
		delay = min(2*delay, botRetryMax)
	}
}

// runBotOnce turns a panic on the polling goroutine into an error, so it
// cannot crash the process. Handlers run on the bot's own goroutines and
// are protected by the bot's recover middleware.
func runBotOnce(ctx context.Context, b *bot.Bot) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("bot panic: %v", p)
		}
	}()
	return b.Run(ctx)
}
