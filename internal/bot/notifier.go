package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

const (
	// notifyCreatedDelay lets the author finish the new item (photos are
	// uploaded right after it is created) before the partner hears of it.
	notifyCreatedDelay = 15 * time.Second
	// notifyTimeout bounds the delivery of one notification.
	notifyTimeout = 30 * time.Second
	// maxPhotoUpload is the Bot API limit for sendPhoto.
	maxPhotoUpload int64 = 10 << 20
)

// notifier implements service.Notifier. Every notice is delivered in its own
// goroutine, detached from the request that triggered it; drain waits for
// all of them on shutdown.
type notifier struct {
	api     messenger
	web     *webApp
	log     *slog.Logger
	svc     atomic.Pointer[service.Services]
	delay   time.Duration
	timeout time.Duration

	mu     sync.Mutex
	closed bool
	quit   chan struct{}
	wg     sync.WaitGroup
}

var _ service.Notifier = (*notifier)(nil)

func newNotifier(api messenger, web *webApp, log *slog.Logger) *notifier {
	return &notifier{
		api:     api,
		web:     web,
		log:     log,
		delay:   notifyCreatedDelay,
		timeout: notifyTimeout,
		quit:    make(chan struct{}),
	}
}

// attach gives the notifier access to the services, used to reload items
// before announcing them and to read cover photos.
func (n *notifier) attach(s *service.Services) { n.svc.Store(s) }

// WishCreated announces a new wish after a short delay, with its final title
// and cover. A wish deleted in the meantime is not announced.
func (n *notifier) WishCreated(ctx context.Context, r service.Recipients, w domain.Wish) {
	n.spawn(ctx, func(ctx context.Context) {
		n.pause()
		ctx, cancel := context.WithTimeout(ctx, n.timeout)
		defer cancel()
		w, ok := n.reloadWish(ctx, w)
		if !ok {
			return
		}
		actor := r.Actor.DisplayName()
		n.deliver(ctx, r.To, notice{
			cover:   coverOf(w.Images),
			caption: renderWishCreated(actor, w, maxCaptionLen),
			text:    renderWishCreated(actor, w, maxMessageLen),
			open:    n.web.wishLink(w.ID),
		})
	})
}

// WishFulfilled announces a fulfilled wish right away.
func (n *notifier) WishFulfilled(ctx context.Context, r service.Recipients, w domain.Wish) {
	n.spawn(ctx, func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, n.timeout)
		defer cancel()
		actor := r.Actor.DisplayName()
		n.deliver(ctx, r.To, notice{
			cover:   coverOf(w.Images),
			caption: renderWishFulfilled(actor, w, maxCaptionLen),
			text:    renderWishFulfilled(actor, w, maxMessageLen),
			open:    n.web.wishLink(w.ID),
		})
	})
}

// RecipeCreated announces a new recipe after a short delay.
func (n *notifier) RecipeCreated(ctx context.Context, r service.Recipients, rec domain.Recipe) {
	n.spawn(ctx, func(ctx context.Context) {
		n.pause()
		ctx, cancel := context.WithTimeout(ctx, n.timeout)
		defer cancel()
		rec, ok := n.reloadRecipe(ctx, rec)
		if !ok {
			return
		}
		actor := r.Actor.DisplayName()
		n.deliver(ctx, r.To, notice{
			cover:   coverOf(rec.Images),
			caption: renderRecipeCreated(actor, rec, maxCaptionLen),
			text:    renderRecipeCreated(actor, rec, maxMessageLen),
			open:    n.web.recipeLink(rec.ID),
		})
	})
}

// spawn runs fn in a tracked goroutine with a context that survives the
// caller's request. After drain has started, notices are dropped.
func (n *notifier) spawn(ctx context.Context, fn func(context.Context)) {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		n.log.Warn("bot: shutting down, notification dropped")
		return
	}
	n.wg.Add(1)
	n.mu.Unlock()

	detached := context.WithoutCancel(ctx)
	go func() {
		defer n.wg.Done()
		defer func() {
			if p := recover(); p != nil {
				n.log.Error("bot: notification panic", "panic", fmt.Sprint(p))
			}
		}()
		fn(detached)
	}()
}

// pause waits for the announcement delay, cut short by shutdown.
func (n *notifier) pause() {
	if n.delay <= 0 {
		return
	}
	t := time.NewTimer(n.delay)
	defer t.Stop()
	select {
	case <-t.C:
	case <-n.quit:
	}
}

// drain stops accepting notices and waits for those in flight.
func (n *notifier) drain() {
	n.mu.Lock()
	if !n.closed {
		n.closed = true
		close(n.quit)
	}
	n.mu.Unlock()
	n.wg.Wait()
}

func (n *notifier) reloadWish(ctx context.Context, w domain.Wish) (domain.Wish, bool) {
	s := n.svc.Load()
	if s == nil {
		return w, true
	}
	fresh, err := s.Wishes.Get(ctx, w.ID)
	switch {
	case err == nil:
		return fresh, true
	case errors.Is(err, domain.ErrNotFound):
		n.log.Debug("bot: wish deleted before notification", "wish_id", w.ID)
		return w, false
	}
	n.log.Warn("bot: reload wish for notification failed", "wish_id", w.ID, "err", err)
	return w, true
}

func (n *notifier) reloadRecipe(ctx context.Context, r domain.Recipe) (domain.Recipe, bool) {
	s := n.svc.Load()
	if s == nil {
		return r, true
	}
	fresh, err := s.Recipes.Get(ctx, r.ID)
	switch {
	case err == nil:
		return fresh, true
	case errors.Is(err, domain.ErrNotFound):
		n.log.Debug("bot: recipe deleted before notification", "recipe_id", r.ID)
		return r, false
	}
	n.log.Warn("bot: reload recipe for notification failed", "recipe_id", r.ID, "err", err)
	return r, true
}

type notice struct {
	cover   *domain.ImageID
	caption string
	text    string
	open    string // deep link into the Mini App, "" when unknown
}

// deliver sends the notice to every recipient. Failures (e.g. a partner who
// blocked the bot) are logged and never propagated.
func (n *notifier) deliver(ctx context.Context, to []domain.User, msg notice) {
	kb := openKeyboard("Открыть ✨", msg.open)
	var photo []byte
	if msg.cover != nil {
		photo = n.loadCover(ctx, *msg.cover)
	}
	for _, u := range to {
		chatID := int64(u.ID)
		if photo != nil {
			_, err := n.api.SendPhoto(ctx, &tg.SendPhotoParams{
				ChatID:      chatID,
				Photo:       &models.InputFileUpload{Filename: "cover.jpg", Data: bytes.NewReader(photo)},
				Caption:     msg.caption,
				ParseMode:   models.ParseModeHTML,
				ReplyMarkup: markup(kb),
			})
			if err == nil {
				continue
			}
			if errors.Is(err, tg.ErrorForbidden) {
				n.log.Warn("bot: partner unreachable", "user_id", u.ID, "err", err)
				continue
			}
			n.log.Warn("bot: notification photo failed, sending text", "user_id", u.ID, "err", err)
		}
		if _, err := sendHTML(ctx, n.api, chatID, msg.text, kb); err != nil {
			n.log.Warn("bot: notification failed", "user_id", u.ID, "err", err)
		}
	}
}

// loadCover reads the cover photo into memory so it can be uploaded to
// several recipients; nil means "send text only".
func (n *notifier) loadCover(ctx context.Context, id domain.ImageID) []byte {
	s := n.svc.Load()
	if s == nil {
		return nil
	}
	f, err := s.Images.Open(ctx, id, service.VariantFull)
	if err != nil {
		n.log.Warn("bot: open cover for notification failed", "image_id", id, "err", err)
		return nil
	}
	defer f.Content.Close()
	data, err := io.ReadAll(io.LimitReader(f.Content, maxPhotoUpload+1))
	if err != nil || int64(len(data)) > maxPhotoUpload {
		n.log.Warn("bot: cover unusable for notification", "image_id", id, "err", err)
		return nil
	}
	return data
}
