package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/recipeimport"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

const (
	// importTimeout bounds one import. A model watching a reel takes up to
	// a minute and a half; the HTTP API allows the same two minutes.
	importTimeout = 2 * time.Minute
	// importHintAfter is when the status message admits that the import
	// may be watching the video: reading a caption takes a few seconds.
	importHintAfter = 6 * time.Second
	// importFinishTimeout bounds the answer once the import is over (photo
	// upload included), also when the bot is shutting down.
	importFinishTimeout = 20 * time.Second
	// captionWaitTTL is how long after Instagram did not give a post the
	// next text message is taken for its caption.
	captionWaitTTL = 10 * time.Minute
	// minRecipeIngredients ingredients with an amount make a plain text
	// look like a recipe.
	minRecipeIngredients = 2
	maxImportWarnings    = 5
	importWarningLen     = 200
	// reactionSeen is put on a message the bot starts working on.
	reactionSeen = "👀"
)

// Texts of the import flow.
const (
	statusReadingCaption = "⏳ Читаю подпись…"
	statusReadingText    = "⏳ Разбираю текст…"
	statusWatchingVideo  = "🎬 Если рецепт в видео — смотрю ролик, до минуты…"
	importStopping       = "⚠️ Бот перезапускается — попробуйте ещё раз через минуту."
	importReviewHint     = "Проверьте ингредиенты и шаги в приложении — партнёр увидит рецепт через пару минут."
	instagramDown        = "Instagram не отдал пост — перешлите сюда текст подписи"
	captionWaitHint      = "Жду текст 10 минут · /cancel — не ждать"
	notARecipeInPost     = "Не нашёл в подписи рецепт 🤔 Если он там есть — пришлите текст рецепта сообщением."
	notARecipeInText     = "Не нашёл в тексте рецепт — нужны ингредиенты с количеством, например «Мука — 200 г»."
	recipeInVideo        = "Рецепт, похоже, только в видео — пришлите текст рецепта сообщением."
	tooManyImports       = "Слишком много импортов подряд — подождите минуту и пришлите ссылку ещё раз."
	importUnavailable    = "Сейчас не получается разобрать рецепт 😕 Попробуйте чуть позже."
	draftOffer           = "Ссылку можно сохранить как обычно 👇"
)

// sourceHeading starts the original text that an unsure import keeps at
// the end of the recipe body (see internal/recipeimport); its numbered
// lines are not steps of the recipe.
const sourceHeading = "Исходный текст:"

// instagramLink finds an Instagram post or reel link in a message, in its
// text or behind a text link, and returns its canonical form.
func instagramLink(text string, entities []models.MessageEntity) (string, bool) {
	if ref, ok := recipeimport.ParseURL(text); ok {
		return ref.URL(), true
	}
	for _, e := range entities {
		if e.Type != models.MessageEntityTypeTextLink {
			continue
		}
		if ref, ok := recipeimport.ParseURL(e.URL); ok {
			return ref.URL(), true
		}
	}
	return "", false
}

// looksLikeRecipe reports whether the rule-based recipe parser finds at
// least minRecipeIngredients ingredients with an amount in text.
func looksLikeRecipe(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	n := 0
	for _, ing := range recipeimport.Parse(text).Ingredients {
		if ing.Quantity != nil && ing.Quantity.Hundredths > 0 {
			if n++; n >= minRecipeIngredients {
				return true
			}
		}
	}
	return false
}

// importRequest is one import started from the chat.
type importRequest struct {
	user   domain.UserID
	chatID int64
	input  service.ImportInput
	// link is the post a text import belongs to (its caption, sent after
	// Instagram did not give the post), "" otherwise.
	link string
	// photos are draft photos to add to the imported recipe.
	photos []string
	// origin is the user's message; when it holds no recipe, a normal
	// draft is started from it. nil for an import from a draft card.
	origin *models.Message
	// status is the message telling how the import goes, 0 when there is
	// none (it could not be sent or was deleted).
	status int
}

// importLink imports the Instagram post linked in m: 👀 on the message,
// one status message edited as the import goes, then the recipe card.
func (a *app) importLink(ctx context.Context, m *models.Message, link string) {
	user := domain.UserID(m.From.ID)
	a.captions.drop(user)
	a.react(ctx, m, reactionSeen)
	a.startImport(ctx, importRequest{
		user:   user,
		chatID: m.Chat.ID,
		input:  service.ImportInput{URL: link},
		origin: m,
	}, statusReadingCaption)
}

// importCaption imports the text sent after Instagram did not give the post
// it belongs to; the recipe gets the post's link.
func (a *app) importCaption(ctx context.Context, m *models.Message, w captionWait) {
	a.react(ctx, m, reactionSeen)
	a.startImport(ctx, importRequest{
		user:   domain.UserID(m.From.ID),
		chatID: m.Chat.ID,
		input:  service.ImportInput{Text: m.Text, Link: w.link},
		link:   w.link,
		origin: m,
	}, statusReadingText)
}

// importDraft imports the text of a draft that reads like a recipe. The
// draft is used up: its card becomes the status message and its photos go
// to the new recipe.
func (a *app) importDraft(ctx context.Context, r *cbReply, user domain.UserID, d draft) {
	if d.source == "" {
		r.answer(ctx, "Кнопка устарела")
		return
	}
	a.drafts.remove(user, d.id)
	deleteQuietly(ctx, a.api, a.log, d.chatID, d.promptID)
	r.answer(ctx, "Разбираю рецепт…")
	a.startImport(ctx, importRequest{
		user:   user,
		chatID: d.chatID,
		input:  service.ImportInput{Text: d.source},
		photos: d.photos,
		status: d.cardID,
	}, statusReadingText)
}

// startImport shows the first status and runs the import in the background,
// so the user can go on chatting while a model watches a reel.
func (a *app) startImport(ctx context.Context, req importRequest, status string) {
	a.setStatus(ctx, &req, plainHTML(status), nil)
	if !a.jobs.spawn(func(jobCtx context.Context) { a.runImport(jobCtx, req) }) {
		a.setStatus(ctx, &req, plainHTML(importStopping), nil)
	}
}

// runImport calls the use case and answers in the chat. The status message
// admits after a while that the video may be watched; that edit always
// lands before the answer.
func (a *app) runImport(jobCtx context.Context, req importRequest) {
	importCtx, cancelImport := context.WithTimeout(jobCtx, importTimeout)
	defer cancelImport()

	var (
		mu       sync.Mutex
		finished bool
	)
	if req.input.URL != "" && a.importHint > 0 {
		hint := time.AfterFunc(a.importHint, func() {
			mu.Lock()
			defer mu.Unlock()
			if !finished {
				a.setStatus(importCtx, &req, plainHTML(statusWatchingVideo), nil)
			}
		})
		defer hint.Stop()
	}
	rec, report, err := a.svc.Recipes.Import(importCtx, req.user, req.input)
	mu.Lock()
	finished = true
	mu.Unlock()

	stopping := jobCtx.Err() != nil
	timedOut := !stopping && importCtx.Err() != nil
	// The answer is due even when the bot is stopping or the import ran
	// out of time.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(importCtx), importFinishTimeout)
	defer cancel()
	switch {
	case err == nil && report.Duplicate:
		a.log.Info("bot: import found an existing recipe", "user_id", req.user, "recipe_id", rec.ID)
		a.setStatus(ctx, &req, plainHTML("📌 Уже есть: «"+rec.Title+"»"), openKeyboard("Открыть ✨", a.web.recipeLink(rec.ID)))
	case err == nil:
		a.importDone(ctx, &req, rec, report)
	case stopping:
		a.setStatus(ctx, &req, plainHTML(importStopping), nil)
	default:
		a.importFailed(ctx, &req, err, timedOut)
	}
}

// importDone shows the new recipe: its cover with a short summary, the
// parser's warnings and the buttons to check or delete it.
func (a *app) importDone(ctx context.Context, req *importRequest, rec domain.Recipe, report service.ImportReport) {
	warnings := report.Warnings
	if len(req.photos) > 0 {
		add := func(ctx context.Context, src io.Reader) error {
			_, err := a.svc.Recipes.AddImage(ctx, req.user, rec.ID, src)
			return err
		}
		if failed := a.attachPhotos(ctx, req.photos, domain.MaxImagesPerRecipe-len(rec.Images), add); failed > 0 {
			warnings = append(slices.Clone(warnings), "Не получилось добавить фото: "+strconv.Itoa(failed))
		}
		if fresh, err := a.svc.Recipes.Get(ctx, rec.ID); err == nil {
			rec = fresh
		} else {
			a.log.Warn("bot: reload imported recipe failed", "recipe_id", rec.ID, "err", err)
		}
	}
	c := importCard(rec, warnings, a.web.recipeLink(rec.ID))
	if c.cover != nil && a.sendCover(ctx, req.chatID, *c.cover, c.caption, c.kb) {
		deleteQuietly(ctx, a.api, a.log, req.chatID, req.status)
		return
	}
	a.setStatus(ctx, req, c.text, c.kb)
}

// importFailed explains why nothing was imported. A post Instagram did not
// give can be imported from its caption: the next text message is taken
// for it. A message without a recipe becomes a normal draft, so a link to
// something else can still be saved as a wish.
func (a *app) importFailed(ctx context.Context, req *importRequest, err error, timedOut bool) {
	fromPost := req.input.URL != ""
	offer := false
	h := newHTML(maxMessageLen)
	switch {
	case errors.Is(err, service.ErrTooManyImports):
		h.Text(tooManyImports)
	case errors.Is(err, service.ErrRecipeInVideo):
		h.Text(recipeInVideo)
		offer = true
	case errors.Is(err, service.ErrNotARecipe):
		if fromPost {
			h.Text(notARecipeInPost)
		} else {
			h.Text(notARecipeInText)
		}
		offer = true
	case fromPost && (timedOut || errors.Is(err, domain.ErrExternalUnavailable)):
		a.captions.put(req.user, captionWait{link: req.input.URL})
		h.Text(instagramDown).NL().Italic(captionWaitHint)
	case timedOut || errors.Is(err, domain.ErrExternalUnavailable):
		h.Text(importUnavailable)
	default:
		h.Text(a.userError(err, "import recipe"))
	}
	offer = offer && req.origin != nil
	if offer {
		h.NL().Text(draftOffer)
	}
	a.setStatus(ctx, req, h.String(), nil)
	if offer {
		kind := kindWish
		if errors.Is(err, service.ErrRecipeInVideo) {
			kind = kindRecipe
		}
		a.offerDraft(ctx, req, kind)
	}
}

// offerDraft starts a normal draft from the message whose import failed.
// It runs outside the update that started the import, so it takes the
// user's lock like a handler does.
func (a *app) offerDraft(ctx context.Context, req *importRequest, kind entityKind) {
	unlock, err := a.locks.lock(ctx, req.user)
	if err != nil {
		a.log.Warn("bot: draft after a failed import skipped", "user_id", req.user)
		return
	}
	defer unlock()
	m := req.origin
	a.startDraftAs(ctx, m, parseInput(m.Text, m.Entities), "", kind)
}

// setStatus shows text in the status message, or in a new message when
// there is none or it can no longer be edited.
func (a *app) setStatus(ctx context.Context, req *importRequest, text string, kb *models.InlineKeyboardMarkup) {
	if req.status != 0 {
		err := editHTML(ctx, a.api, req.chatID, req.status, false, text, kb)
		if err == nil {
			return
		}
		if !errors.Is(err, tg.ErrorBadRequest) {
			a.log.Warn("bot: edit import status failed", "err", err)
			return
		}
	}
	msg, err := sendHTML(ctx, a.api, req.chatID, text, kb)
	if err != nil {
		a.log.Warn("bot: send import status failed", "err", err)
		req.status = 0
		return
	}
	req.status = msg.ID
}

// react puts an emoji reaction on the user's message; it is a courtesy, so
// a failure is only logged.
func (a *app) react(ctx context.Context, m *models.Message, emoji string) {
	_, err := a.api.SetMessageReaction(ctx, &tg.SetMessageReactionParams{
		ChatID:    m.Chat.ID,
		MessageID: m.ID,
		Reaction: []models.ReactionType{{
			Type:              models.ReactionTypeTypeEmoji,
			ReactionTypeEmoji: &models.ReactionTypeEmoji{Emoji: emoji},
		}},
	})
	if err != nil {
		a.log.Debug("bot: set reaction failed", "err", err)
	}
}

func plainHTML(text string) string { return newHTML(maxMessageLen).Text(text).String() }

// importCard is the answer to an import: the cover with a caption when
// there is one.
func importCard(r domain.Recipe, warnings []string, openURL string) card {
	return card{
		cover:   coverOf(r.Images),
		caption: renderImported(r, warnings, maxCaptionLen),
		text:    renderImported(r, warnings, maxMessageLen),
		kb:      importKeyboard(r.ID, openURL),
	}
}

// renderImported is «✅ Название · 11 ингредиентов · 6 шагов · 4 порции ·
// ≈540 ккал/порц», the parser's warnings and a reminder to check.
func renderImported(r domain.Recipe, warnings []string, limit int) string {
	h := newHTML(limit)
	h.Text("✅ ").Bold(r.Title)
	if s := recipeSummary(r); s != "" {
		h.Text(" · " + s)
	}
	for _, w := range warnings[:min(len(warnings), maxImportWarnings)] {
		w, _ = excerpt(w, importWarningLen)
		h.NL().Text("⚠️ " + w)
	}
	if rest := len(warnings) - maxImportWarnings; rest > 0 {
		h.NL().Text("⚠️ …и ещё " + strconv.Itoa(rest))
	}
	h.NL().NL().Italic(importReviewHint)
	return h.String()
}

// fromInstagram reports whether the recipe links to an Instagram post.
func fromInstagram(r domain.Recipe) bool {
	if r.Link == nil {
		return false
	}
	_, ok := recipeimport.ParseURL(*r.Link)
	return ok
}

// renderRecipeImported is the partner's notice of an imported recipe.
func renderRecipeImported(actor string, r domain.Recipe, limit int) string {
	h := newHTML(limit)
	h.Text("📥 ").Bold(actor).Text(" добавил(а) рецепт")
	if fromInstagram(r) {
		h.Text(" из Instagram")
	}
	h.Text(": «" + r.Title + "»")
	if s := recipeSummary(r); s != "" {
		h.NL().Text(s)
	}
	return h.String()
}

// captionWait is a pending «перешлите текст подписи»: Instagram did not give
// the post behind link.
type captionWait struct {
	link  string
	asked time.Time
}

// captionWaits keeps at most one wait per user; a wait expires after ttl
// and is used up by the next text message.
type captionWaits struct {
	mu    sync.Mutex
	waits map[domain.UserID]captionWait
	ttl   time.Duration
	now   func() time.Time
}

func newCaptionWaits(ttl time.Duration, now func() time.Time) *captionWaits {
	return &captionWaits{waits: make(map[domain.UserID]captionWait), ttl: ttl, now: now}
}

func (c *captionWaits) put(user domain.UserID, w captionWait) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w.asked = c.now()
	c.waits[user] = w
}

// take removes and returns the user's live wait.
func (c *captionWaits) take(user domain.UserID) (captionWait, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.waits[user]
	delete(c.waits, user)
	if !ok || c.now().Sub(w.asked) >= c.ttl {
		return captionWait{}, false
	}
	return w, true
}

// drop forgets the user's wait and reports whether a live one was pending.
func (c *captionWaits) drop(user domain.UserID) bool {
	_, ok := c.take(user)
	return ok
}

// sweep forgets expired waits.
func (c *captionWaits) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for user, w := range c.waits {
		if c.now().Sub(w.asked) >= c.ttl {
			delete(c.waits, user)
		}
	}
}

// jobs runs work that outlives an update (imports) in tracked goroutines,
// outside the user's lock. stop cancels what runs and waits for it.
type jobs struct {
	ctx    context.Context
	cancel context.CancelFunc
	log    *slog.Logger

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func newJobs(log *slog.Logger) *jobs {
	ctx, cancel := context.WithCancel(context.Background())
	return &jobs{ctx: ctx, cancel: cancel, log: log}
}

// spawn runs fn in the background; after stop it reports false instead.
func (j *jobs) spawn(fn func(ctx context.Context)) bool {
	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		return false
	}
	j.wg.Add(1)
	j.mu.Unlock()
	go func() {
		defer j.wg.Done()
		defer func() {
			if p := recover(); p != nil {
				j.log.Error("bot: background job panic", "panic", fmt.Sprint(p))
			}
		}()
		fn(j.ctx)
	}()
	return true
}

// stop refuses new jobs, cancels the running ones and waits for them.
func (j *jobs) stop() {
	j.mu.Lock()
	j.closed = true
	j.mu.Unlock()
	j.cancel()
	j.wg.Wait()
}

// wait blocks until the running jobs are done (tests).
func (j *jobs) wait() { j.wg.Wait() }
