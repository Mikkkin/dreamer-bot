// Package bot is the Telegram chat face of the wishlist: quick add of wishes
// and recipes from text and photos, lists and cards, statistics and partner
// notifications. It is a thin adapter over the service use cases.
//
// Security model: only private chats with whitelisted users are served;
// everything else is dropped without a reply. The bot token never reaches
// logs or errors: library errors and file downloads are redacted, the
// library's default handler (which logs whole updates) is replaced, and
// debug mode is never enabled.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// Options configure the bot.
type Options struct {
	Token           string
	Whitelist       auth.Whitelist
	WebAppURL       string // may be "" (not known yet)
	DefaultCurrency domain.Currency
	MaxImageBytes   int64
	Log             *slog.Logger
}

const (
	// pollTimeout is the long-polling wait; the HTTP client timeout must be
	// longer, or every idle poll would end in a client error.
	pollTimeout       = 50 * time.Second
	httpClientTimeout = pollTimeout + 20*time.Second
	// workers process updates concurrently. Updates of one user are
	// serialised by the app, so an album (up to 10 updates at once) may park
	// several workers on one user; 8 leaves room for the other user.
	workers          = 8
	getMeTimeout     = 15 * time.Second
	janitorEvery     = time.Minute
	defaultImageMax  = 10 << 20
	menuButtonText   = "✨ Мечты"
	maxLibraryErrLen = 512
)

// ErrTokenRejected means Telegram answered 401 to getMe: the token is wrong
// or was revoked. Retrying cannot help, unlike a network failure.
var ErrTokenRejected = errors.New("bot: telegram rejected the bot token (401)")

// Bot is the Telegram chat bot. Create it with New, connect it to the
// services with Attach and start it with Run.
type Bot struct {
	client   *tg.Bot // nil in tests
	api      telegramAPI
	opts     Options
	log      *slog.Logger
	redact   redactor
	web      *webApp
	notifier *notifier
	username string
	now      func() time.Time

	app        atomic.Pointer[app]
	attachOnce sync.Once
	running    atomic.Bool

	menuMu     sync.Mutex
	configured bool // Run has set up the bot account; guarded by menuMu
}

// New builds the Telegram client and checks the token with getMe. It does
// not start polling.
func New(ctx context.Context, o Options) (*Bot, error) {
	return newWithClientOptions(ctx, o)
}

// newWithClientOptions is New with extra library options appended; tests use
// it to point the real client at a fake Bot API server.
func newWithClientOptions(ctx context.Context, o Options, extra ...tg.Option) (*Bot, error) {
	if err := o.normalize(); err != nil {
		return nil, err
	}
	b := newBot(o)
	opts := []tg.Option{
		tg.WithSkipGetMe(), // called below with the caller's context
		// Middlewares run outermost first. They are resolved per update, so
		// the method values may read fields set after tg.New returns.
		tg.WithMiddlewares(recoverer(b.log), b.accessMiddleware, b.setupMiddleware),
		tg.WithDefaultHandler(b.unhandled),
		tg.WithErrorsHandler(b.onLibraryError),
		tg.WithAllowedUpdates(tg.AllowedUpdates{
			models.AllowedUpdateMessage,
			models.AllowedUpdateCallbackQuery,
			models.AllowedUpdateMyChatMember,
		}),
		tg.WithHTTPClient(pollTimeout, &http.Client{Timeout: httpClientTimeout}),
		tg.WithWorkers(workers),
		// Handlers run inside the workers, so Start returns only after the
		// in-flight handlers are done: a graceful drain on shutdown.
		tg.WithNotAsyncHandlers(),
	}
	client, err := tg.New(o.Token, append(opts, extra...)...)
	if err != nil {
		return nil, fmt.Errorf("bot: create client: %w", b.redact.err(err))
	}
	ctx, cancel := context.WithTimeout(ctx, getMeTimeout)
	defer cancel()
	me, err := client.GetMe(ctx)
	if errors.Is(err, tg.ErrorUnauthorized) {
		return nil, ErrTokenRejected
	}
	if err != nil {
		return nil, fmt.Errorf("bot: getMe: %w", b.redact.err(err))
	}
	b.client = client
	b.bind(client, me.Username)
	b.log.Info("bot: connected", "username", me.Username, "setup_mode", o.Whitelist.Empty())
	return b, nil
}

// newBot holds the parts of a Bot that do not need the Telegram client, so
// that the client's middlewares can refer to the Bot before it exists.
func newBot(o Options) *Bot {
	b := &Bot{
		opts:   o,
		log:    o.Log,
		redact: newRedactor(o.Token),
		web:    &webApp{},
		now:    time.Now,
	}
	b.web.set(o.WebAppURL)
	return b
}

// bind connects the Bot to the Bot API (the real client or a test fake).
func (b *Bot) bind(api telegramAPI, username string) {
	b.api, b.username = api, username
	b.notifier = newNotifier(api, b.web, b.log)
}

func (o *Options) normalize() error {
	if strings.TrimSpace(o.Token) == "" {
		return errors.New("bot: token is required")
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.DefaultCurrency == "" {
		o.DefaultCurrency = domain.Currencies[0]
	}
	if _, err := domain.ParseCurrency(string(o.DefaultCurrency)); err != nil {
		return fmt.Errorf("bot: default currency: %w", err)
	}
	if o.MaxImageBytes <= 0 {
		o.MaxImageBytes = defaultImageMax
	}
	return validateWebAppURL(o.WebAppURL)
}

// Notifier returns the partner notifier. It can be handed to the services
// before Attach; until then it announces items without reloading them.
func (b *Bot) Notifier() service.Notifier { return b.notifier }

// Attach connects the bot to the use cases and registers the handlers. Call
// it before Run.
func (b *Bot) Attach(s *service.Services) {
	if s == nil {
		b.log.Error("bot: Attach called without services")
		return
	}
	b.app.Store(&app{
		api:      b.api,
		files:    newDownloader(b.api, b.opts.MaxImageBytes, b.redact),
		svc:      s,
		drafts:   newDraftStore(draftTTL, b.now),
		locks:    newUserLocks(),
		toucher:  newToucher(s.Users, b.now, b.log),
		web:      b.web,
		username: b.username,
		currency: b.opts.DefaultCurrency,
		loc:      time.Local,
		now:      b.now,
		log:      b.log,
		timeout:  handlerTimeout,
	})
	b.notifier.attach(s)
	b.attachOnce.Do(func() {
		if b.client != nil {
			b.client.RegisterHandlerMatchFunc(func(*models.Update) bool { return true }, b.dispatch, b.touchMiddleware)
		}
	})
}

// Run configures the bot account (webhook off, commands, menu button) and
// long-polls until ctx is done. It then waits for in-flight handlers and
// notifications before returning. Polling errors are retried with backoff
// by the library and never end Run.
func (b *Bot) Run(ctx context.Context) error {
	a := b.app.Load()
	if a == nil || b.client == nil {
		return errors.New("bot: Attach must be called before Run")
	}
	if !b.running.CompareAndSwap(false, true) {
		return errors.New("bot: already running")
	}
	defer b.running.Store(false)

	b.configure(ctx)

	var janitor sync.WaitGroup
	janitor.Go(func() { a.drafts.janitor(ctx, janitorEvery) })
	b.log.Info("bot: polling started")
	b.client.Start(ctx)
	janitor.Wait()
	b.notifier.drain()
	b.log.Info("bot: stopped")
	return nil
}

// configure prepares the bot account. Every step is best effort: a failure
// is logged and polling starts anyway (the library keeps retrying).
func (b *Bot) configure(ctx context.Context) {
	if _, err := b.api.DeleteWebhook(ctx, &tg.DeleteWebhookParams{DropPendingUpdates: false}); err != nil {
		b.log.Warn("bot: deleteWebhook failed", "err", err)
	}
	if _, err := b.api.SetMyCommands(ctx, &tg.SetMyCommandsParams{
		Commands: botCommands(b.opts.Whitelist.Empty()),
	}); err != nil {
		b.log.Warn("bot: setMyCommands failed", "err", err)
	}
	b.menuMu.Lock()
	defer b.menuMu.Unlock()
	b.configured = true
	b.applyMenu(ctx, b.web.url())
}

// SetWebAppURL changes the Mini App URL at runtime (e.g. a new quick tunnel
// hostname) and re-sets the menu buttons. It is safe for concurrent use.
func (b *Bot) SetWebAppURL(ctx context.Context, url string) {
	if err := validateWebAppURL(url); err != nil {
		b.log.Warn("bot: ignoring invalid web app URL", "err", err)
		return
	}
	b.menuMu.Lock()
	defer b.menuMu.Unlock()
	b.web.set(url)
	b.log.Info("bot: web app URL changed")
	if b.configured {
		b.applyMenu(ctx, url)
	}
}

// applyMenu points the chat menu button at the Mini App, for the default
// scope and for every whitelisted chat (a per-chat button overrides the
// default one). Without a URL it restores the commands menu, so a stale
// tunnel hostname from a previous run is not left behind.
func (b *Bot) applyMenu(ctx context.Context, url string) {
	if b.opts.Whitelist.Empty() {
		return
	}
	var menu models.InputMenuButton = models.MenuButtonCommands{Type: models.MenuButtonTypeCommands}
	if url != "" {
		menu = models.MenuButtonWebApp{
			Type:   models.MenuButtonTypeWebApp,
			Text:   menuButtonText,
			WebApp: models.WebAppInfo{URL: url},
		}
	}
	if _, err := b.api.SetChatMenuButton(ctx, &tg.SetChatMenuButtonParams{MenuButton: menu}); err != nil {
		b.log.Warn("bot: set default menu button failed", "err", err)
	}
	for _, id := range b.opts.Whitelist.IDs() {
		if _, err := b.api.SetChatMenuButton(ctx, &tg.SetChatMenuButtonParams{ChatID: int64(id), MenuButton: menu}); err != nil {
			// Expected until the user has opened a chat with the bot.
			b.log.Debug("bot: set chat menu button failed", "user_id", id, "err", err)
		}
	}
}

func (b *Bot) accessMiddleware(next tg.HandlerFunc) tg.HandlerFunc {
	return accessFilter(b.opts.Whitelist, b.api, b.log)(next)
}

func (b *Bot) setupMiddleware(next tg.HandlerFunc) tg.HandlerFunc {
	return setupGate(b.opts.Whitelist, b.api, b.username, b.log)(next)
}

func (b *Bot) touchMiddleware(next tg.HandlerFunc) tg.HandlerFunc {
	return func(ctx context.Context, tb *tg.Bot, u *models.Update) {
		if a := b.app.Load(); a != nil {
			a.toucher.middleware(next)(ctx, tb, u)
			return
		}
		next(ctx, tb, u)
	}
}

func (b *Bot) dispatch(ctx context.Context, tb *tg.Bot, u *models.Update) {
	if a := b.app.Load(); a != nil {
		a.handle(ctx, tb, u)
	}
}

// unhandled replaces the library default handler, which logs whole updates
// including message text.
func (b *Bot) unhandled(_ context.Context, _ *tg.Bot, u *models.Update) {
	b.log.Debug("bot: update not handled", "update_id", u.ID, "kind", originOf(u).kind)
}

// onLibraryError logs errors reported by the polling loop. They are
// redacted, and errors that embed raw updates or response bodies (user
// content) are reduced to their description.
func (b *Bot) onLibraryError(err error) {
	b.log.Warn("bot: telegram", "err", sanitizeLibraryError(b.redact.string(err.Error())))
}

func sanitizeLibraryError(msg string) string {
	for _, marker := range []string{"error decode update", "error decode response body"} {
		if i := strings.Index(msg, marker); i >= 0 {
			return msg[:i] + marker + " (content omitted)"
		}
	}
	short, _ := clip(msg, maxLibraryErrLen)
	return short
}
