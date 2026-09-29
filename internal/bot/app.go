package bot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// handlerTimeout bounds the work done for one update, including photo
// downloads when a draft is saved.
const handlerTimeout = 90 * time.Second

// app handles the updates of whitelisted users once the services are
// attached. Updates of one user are processed one at a time, so the draft
// state machine never sees interleaved messages (e.g. album photos, which
// arrive as separate updates at the same moment).
type app struct {
	api      messenger
	files    *downloader
	svc      *service.Services
	drafts   *draftStore
	savings  *savingPrompts
	recent   *recentActions
	locks    *userLocks
	toucher  *toucher
	web      *webApp
	username string
	currency domain.Currency
	loc      *time.Location
	now      func() time.Time
	log      *slog.Logger
	timeout  time.Duration
}

func (a *app) handle(ctx context.Context, _ *tg.Bot, u *models.Update) {
	// Detach from the polling context: on shutdown Run waits for in-flight
	// handlers, and a cancelled context would make all their calls fail.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
	defer cancel()

	o := originOf(u)
	if o.user == nil {
		return
	}
	unlock, err := a.locks.lock(ctx, domain.UserID(o.user.ID))
	if err != nil {
		a.log.Warn("bot: gave up waiting for the previous update", "user_id", o.user.ID)
		return
	}
	defer unlock()

	switch {
	case u.CallbackQuery != nil:
		a.onCallback(ctx, u.CallbackQuery)
	case u.Message != nil:
		a.onMessage(ctx, u.Message)
	}
}

func (a *app) onMessage(ctx context.Context, m *models.Message) {
	if cmd, ok := parseCommand(m.Text, a.username); ok {
		a.onCommand(ctx, m, cmd)
		return
	}
	if fileID := imageFileID(m); fileID != "" {
		a.onPhoto(ctx, m, fileID)
		return
	}
	if strings.TrimSpace(m.Text) == "" {
		a.say(ctx, m.Chat.ID, "Я понимаю текст, ссылки и фото 🙂 Пришлите что-нибудь из этого — и я предложу сохранить.")
		return
	}
	user := domain.UserID(m.From.ID)
	if p, ok := a.savings.get(user); ok && a.fillSaving(ctx, m, p) {
		return
	}
	if d, ok := a.drafts.get(user); ok && d.awaiting != fieldNone {
		a.fillField(ctx, m, d)
		return
	}
	a.startDraft(ctx, m, parseInput(m.Text, m.Entities), "")
}

// imageFileID returns the file ID of the largest photo size, or of an image
// sent as a file (uncompressed), or "".
func imageFileID(m *models.Message) string {
	if len(m.Photo) > 0 {
		best := m.Photo[0]
		for _, p := range m.Photo[1:] {
			if p.Width*p.Height > best.Width*best.Height {
				best = p
			}
		}
		return best.FileID
	}
	if d := m.Document; d != nil {
		switch d.MimeType {
		case "image/jpeg", "image/png", "image/webp":
			return d.FileID
		}
	}
	return ""
}

func (a *app) onCallback(ctx context.Context, cq *models.CallbackQuery) {
	r := &cbReply{api: a.api, log: a.log, id: cq.ID}
	// Every callback query is answered exactly once, even on failures, so
	// the button never keeps spinning.
	defer r.answer(ctx, "")

	c, err := parseCallback(cq.Data)
	if err != nil {
		r.answer(ctx, "Кнопка устарела")
		return
	}
	user := domain.UserID(cq.From.ID)
	msg := cq.Message.Message
	if isDraftOp(c.op) {
		a.onDraftCallback(ctx, r, user, msg, c)
		return
	}
	if c.op == opRecipeRate {
		// A rating counts even when the notice itself is no longer
		// accessible; the message is then just not updated.
		a.rateCook(ctx, r, msg, user, domain.RecipeID(c.id), domain.CookID(c.cook), c.stars)
		return
	}
	if msg == nil {
		if c.op != opNoop {
			r.answer(ctx, "Сообщение устарело — откройте список заново")
		}
		return
	}
	switch c.op {
	case opWishList:
		a.showWishList(ctx, r, msg, c.status, c.page)
	case opRecipeList:
		a.showRecipeList(ctx, r, msg, c.page)
	case opWishOpen:
		a.openWish(ctx, r, msg.Chat.ID, domain.WishID(c.id))
	case opWishStatus:
		a.setWishStatus(ctx, r, msg, user, domain.WishID(c.id), c.status)
	case opWishAskDelete:
		a.askDelete(ctx, r, msg, c.id, opWishDelete, opWishKeep)
	case opWishDelete:
		a.deleteWish(ctx, r, msg, user, domain.WishID(c.id))
	case opWishKeep:
		a.keepWish(ctx, r, msg, domain.WishID(c.id))
	case opWishSave:
		a.askSaving(ctx, r, msg, user, domain.WishID(c.id))
	case opRecipeOpen:
		a.openRecipe(ctx, r, msg.Chat.ID, domain.RecipeID(c.id))
	case opRecipeAskDelete:
		a.askDelete(ctx, r, msg, c.id, opRecipeDelete, opRecipeKeep)
	case opRecipeDelete:
		a.deleteRecipe(ctx, r, msg, user, domain.RecipeID(c.id))
	case opRecipeKeep:
		a.restoreRecipeButtons(ctx, r, msg, domain.RecipeID(c.id), "Оставили 👌")
	case opRecipeBack:
		a.restoreRecipeButtons(ctx, r, msg, domain.RecipeID(c.id), "")
	case opRecipeCookAsk:
		a.askCook(ctx, r, msg, domain.RecipeID(c.id))
	case opRecipeCook:
		a.cookRecipe(ctx, r, msg, user, domain.RecipeID(c.id), c.stars)
	case opRecipeShop:
		a.addRecipeToShopping(ctx, r, msg, user, domain.RecipeID(c.id))
	case opCookAgain:
		a.cookAgain(ctx, r, msg, domain.RecipeID(c.id))
	case opShopList:
		a.showShopping(ctx, r, msg, c.page, "")
	case opShopCheck, opShopUncheck:
		a.markBought(ctx, r, msg, user, domain.ShoppingItemID(c.id), c.op == opShopCheck, c.page)
	case opShopClear:
		a.clearBought(ctx, r, msg, user)
	}
}

func isDraftOp(op cbOp) bool {
	switch op {
	case opDraftKind, opDraftCategories, opDraftCategory, opDraftCuisines, opDraftCuisine,
		opDraftCourses, opDraftCourse, opDraftBack, opDraftField, opDraftHot, opDraftSave, opDraftCancel:
		return true
	}
	return false
}

// cbReply answers a callback query at most once.
type cbReply struct {
	api  messenger
	log  *slog.Logger
	id   string
	done bool
}

// answer shows text as a toast ("" just stops the spinner).
func (r *cbReply) answer(ctx context.Context, text string) { r.send(ctx, text, false) }

// alert shows text in a dialog the user has to dismiss.
func (r *cbReply) alert(ctx context.Context, text string) { r.send(ctx, text, true) }

func (r *cbReply) send(ctx context.Context, text string, alert bool) {
	if r.done {
		return
	}
	r.done = true
	text, _ = excerpt(text, 200) // Bot API limit for callback answers
	if _, err := r.api.AnswerCallbackQuery(ctx, &tg.AnswerCallbackQueryParams{
		CallbackQueryID: r.id,
		Text:            text,
		ShowAlert:       alert,
	}); err != nil {
		r.log.Debug("bot: answer callback failed", "err", err)
	}
}

// say sends a short plain-text reply.
func (a *app) say(ctx context.Context, chatID int64, text string) {
	if _, err := sendHTML(ctx, a.api, chatID, newHTML(maxMessageLen).Text(text).String(), nil); err != nil {
		a.log.Warn("bot: send message failed", "err", err)
	}
}

// userError maps an error to a Russian message for the user. It is the only
// place where errors are translated; unexpected ones are logged here.
func (a *app) userError(err error, action string) string {
	if v, ok := domain.AsValidation(err); ok {
		return capitalize(v.Message)
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return "Не нашёл — кажется, это уже удалили."
	case errors.Is(err, domain.ErrLimitExceeded):
		return "Достигнут лимит — больше добавить нельзя."
	case errors.Is(err, domain.ErrImageTooLarge):
		return "Фото слишком большое."
	case errors.Is(err, domain.ErrImageUnsupported):
		return "Этот формат фото не подходит — нужен JPEG, PNG или WebP."
	case errors.Is(err, domain.ErrConflict):
		return "Такое уже есть."
	}
	a.log.Error("bot: "+action+" failed", "err", err)
	return "Что-то пошло не так 😕 Попробуйте ещё раз чуть позже."
}

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// recentActions remembers button actions that must not run twice, so a
// double tap on one card neither records a cooking twice nor doubles the
// amounts on the shopping list. Keys expire after ttl.
type recentActions struct {
	mu   sync.Mutex
	ttl  time.Duration
	now  func() time.Time
	seen map[string]time.Time
}

// recentActionTTL is long enough to absorb double taps and retries of one
// press, short enough not to block a deliberate repeat.
const recentActionTTL = 30 * time.Second

func newRecentActions(ttl time.Duration, now func() time.Time) *recentActions {
	return &recentActions{ttl: ttl, now: now, seen: make(map[string]time.Time)}
}

// first records key and reports whether it was not seen within ttl.
func (r *recentActions) first(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for k, at := range r.seen {
		if now.Sub(at) >= r.ttl {
			delete(r.seen, k)
		}
	}
	if _, ok := r.seen[key]; ok {
		return false
	}
	r.seen[key] = now
	return true
}

// forget drops key after its action failed, so it can be retried at once.
func (r *recentActions) forget(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.seen, key)
}

// userLocks serialises the updates of each user.
type userLocks struct {
	mu    sync.Mutex
	locks map[domain.UserID]chan struct{}
}

func newUserLocks() *userLocks {
	return &userLocks{locks: make(map[domain.UserID]chan struct{})}
}

// lock waits for the user's previous update to finish or ctx to end.
func (l *userLocks) lock(ctx context.Context, user domain.UserID) (func(), error) {
	l.mu.Lock()
	ch, ok := l.locks[user]
	if !ok {
		ch = make(chan struct{}, 1)
		l.locks[user] = ch
	}
	l.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
