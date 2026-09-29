package bot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// startDraft replaces the user's draft with a new one built from a message
// (and an optional first photo) and sends its card.
func (a *app) startDraft(ctx context.Context, m *models.Message, p parsedInput, photo string) {
	user := domain.UserID(m.From.ID)
	if old, ok := a.drafts.get(user); ok {
		a.retireCard(ctx, old, "Черновик заменён новым 👇")
	}
	d := a.drafts.begin(m.Chat.ID)
	d.applyParsed(p)
	if photo != "" {
		d.addPhoto(photo)
		d.albumID = m.MediaGroupID
	}
	a.showDraft(ctx, &d)
	a.drafts.put(user, d)
}

// onPhoto adds a photo to the current draft or starts a new one. Photos of
// one album always land in one draft; a lone photo without a caption joins
// a recent draft; a photo with a caption describes something new.
func (a *app) onPhoto(ctx context.Context, m *models.Message, fileID string) {
	user := domain.UserID(m.From.ID)
	album := m.MediaGroupID
	d, ok := a.drafts.get(user)
	sameAlbum := ok && album != "" && album == d.albumID
	joins := ok && (sameAlbum || (m.Caption == "" && d.acceptsPhoto(album, a.now())))
	if !joins {
		a.startDraft(ctx, m, parseInput(m.Caption, m.CaptionEntities), fileID)
		return
	}
	if m.Caption != "" {
		d.mergeParsed(parseInput(m.Caption, m.CaptionEntities))
	}
	d.addPhoto(fileID)
	if album != "" {
		d.albumID = album
	}
	a.showDraft(ctx, &d)
	a.drafts.put(user, d)
}

// fillField stores the answer to a ForceReply prompt. On a validation error
// the user gets the reason and the draft keeps waiting for a better value.
func (a *app) fillField(ctx context.Context, m *models.Message, d draft) {
	user := domain.UserID(m.From.ID)
	prompt := d.promptID
	if err := d.fill(m.Text, m.Entities, a.currency); err != nil {
		a.drafts.put(user, d)
		a.say(ctx, m.Chat.ID, a.userError(err, "fill draft field")+"\nПопробуйте ещё раз или отправьте /cancel.")
		return
	}
	// The prompt and the answer have served their purpose; removing them
	// keeps the updated card at the bottom of the chat.
	deleteQuietly(ctx, a.api, a.log, m.Chat.ID, prompt)
	deleteQuietly(ctx, a.api, a.log, m.Chat.ID, m.ID)
	a.showDraft(ctx, &d)
	a.drafts.put(user, d)
}

func (a *app) onDraftCallback(ctx context.Context, r *cbReply, user domain.UserID, msg *models.Message, c callback) {
	d, ok := a.drafts.lookup(user, c.draft)
	if !ok {
		r.answer(ctx, "Черновик устарел")
		if msg != nil {
			a.setKeyboard(ctx, msg, nil)
		}
		return
	}
	switch c.op {
	case opDraftKind:
		awaited := d.awaiting
		d.setKind(c.kind)
		if awaited != fieldNone && d.awaiting == fieldNone {
			deleteQuietly(ctx, a.api, a.log, d.chatID, d.promptID)
			d.promptID = 0
		}
	case opDraftCategories:
		d.view = viewCategories
	case opDraftBack:
		d.view = viewMain
	case opDraftCategory:
		if !a.chooseCategory(ctx, r, &d, domain.CategoryID(c.id)) {
			return
		}
	case opDraftCuisines, opDraftCourses, opDraftCuisine, opDraftCourse:
		if !a.pickTag(ctx, r, &d, c) {
			return
		}
	case opDraftHot:
		d.hot = !d.hot
	case opDraftField:
		a.dropSaving(ctx, user)
		if err := a.askField(ctx, &d, c.field); err != nil {
			r.answer(ctx, a.userError(err, "ask draft field"))
			return
		}
		a.drafts.put(user, d)
		return
	case opDraftSave:
		a.saveDraft(ctx, r, user, d)
		return
	case opDraftCancel:
		a.drafts.remove(user, d.id)
		a.retireCard(ctx, d, "✖️ Черновик удалён")
		r.answer(ctx, "Черновик удалён")
		return
	}
	a.showDraft(ctx, &d)
	a.drafts.put(user, d)
}

func (a *app) chooseCategory(ctx context.Context, r *cbReply, d *draft, id domain.CategoryID) bool {
	if id == 0 {
		d.setCategory(nil, "")
		return true
	}
	c, err := a.svc.Categories.Get(ctx, id)
	if err != nil {
		r.answer(ctx, a.userError(err, "get category"))
		return false
	}
	d.setCategory(&c.ID, c.Label())
	return true
}

// pickTag handles the cuisine and course pickers of a recipe draft. It
// reports false when the tap was refused (the answer is already sent).
func (a *app) pickTag(ctx context.Context, r *cbReply, d *draft, c callback) bool {
	if d.kind != kindRecipe {
		d.view = viewMain // a button of an older card
		return true
	}
	id := domain.RecipeTagID(c.id)
	switch c.op {
	case opDraftCuisines:
		d.view = viewCuisines
	case opDraftCourses:
		d.view = viewCourses
	case opDraftCuisine:
		if id == 0 {
			d.setCuisine(nil)
			return true
		}
		t, err := a.findTag(ctx, domain.TagCuisine, id)
		if err != nil {
			r.answer(ctx, a.userError(err, "get cuisine"))
			return false
		}
		d.setCuisine(&tagRef{id: t.ID, label: t.Label()})
	case opDraftCourse:
		switch {
		case id == 0:
			d.courses = nil
		case d.hasCourse(id): // removing needs no lookup, even of a deleted tag
			d.toggleCourse(tagRef{id: id})
		default:
			t, err := a.findTag(ctx, domain.TagCourse, id)
			if err != nil {
				r.answer(ctx, a.userError(err, "get course"))
				return false
			}
			if !d.toggleCourse(tagRef{id: t.ID, label: t.Label()}) {
				r.answer(ctx, "Не больше "+strconv.Itoa(domain.MaxCoursesPerRecipe)+" типов блюда")
				return false
			}
		}
	}
	return true
}

// findTag returns the recipe tag with id if it is of the given kind.
func (a *app) findTag(ctx context.Context, kind domain.TagKind, id domain.RecipeTagID) (domain.RecipeTag, error) {
	tags, err := a.svc.RecipeTags.List(ctx)
	if err != nil {
		return domain.RecipeTag{}, err
	}
	for _, t := range tags {
		if t.ID == id && t.Kind == kind {
			return t, nil
		}
	}
	return domain.RecipeTag{}, domain.ErrNotFound
}

// askField puts the draft into the "awaiting <field>" state and sends a
// ForceReply prompt, replacing an older prompt if there was one.
func (a *app) askField(ctx context.Context, d *draft, f draftField) error {
	if !d.fieldApplies(f) {
		return &domain.ValidationError{Field: "field", Message: "для рецепта это поле не нужно"}
	}
	deleteQuietly(ctx, a.api, a.log, d.chatID, d.promptID)
	d.promptID = 0
	text, placeholder := a.prompt(*d, f)
	msg, err := a.api.SendMessage(ctx, &tg.SendMessageParams{
		ChatID:    d.chatID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: &models.ForceReply{
			ForceReply:            true,
			InputFieldPlaceholder: placeholder,
		},
	})
	if err != nil {
		d.awaiting = fieldNone
		return err
	}
	d.awaiting, d.promptID = f, msg.ID
	return nil
}

// prompt returns the HTML text and the input placeholder asking for f.
func (a *app) prompt(d draft, f draftField) (string, string) {
	h := newHTML(maxMessageLen)
	var placeholder string
	switch f {
	case fieldTitle:
		h.Text("✏️ Отправьте название (до 120 символов)").NL().Text("/cancel чтобы отменить")
		placeholder = "Название"
	case fieldPrice:
		currency := a.currency
		if d.price != nil {
			currency = d.price.Currency
		}
		h.Text("💰 Отправьте сумму, например ").Code("1200").Text(" или ").Code("15 000 ₽").NL()
		h.Text("Без знака валюты — в " + currency.Symbol() + " · «-» — убрать сумму · /cancel чтобы отменить")
		placeholder = "1200 " + currency.Symbol()
	case fieldLink:
		h.Text("🔗 Отправьте ссылку").NL().Text("«-» — убрать ссылку · /cancel чтобы отменить")
		placeholder = "https://…"
	case fieldText:
		if d.kind == kindRecipe {
			h.Text("📝 Отправьте текст рецепта (до 10 000 символов)")
			placeholder = "Текст рецепта"
		} else {
			h.Text("💬 Отправьте заметку (до 2000 символов)")
			placeholder = "Заметка"
		}
		h.NL().Text("«-» — убрать текст · /cancel чтобы отменить")
	}
	return h.String(), placeholder
}

// saveDraft creates the wish or recipe, attaches the photos and turns the
// card into its saved state.
func (a *app) saveDraft(ctx context.Context, r *cbReply, user domain.UserID, d draft) {
	if strings.TrimSpace(d.title) == "" {
		r.answer(ctx, "Сначала добавьте название ✏️")
		a.dropSaving(ctx, user)
		if err := a.askField(ctx, &d, fieldTitle); err != nil {
			a.log.Warn("bot: ask title failed", "err", err)
		}
		a.drafts.put(user, d)
		return
	}
	// Validate locally first so a mistake is shown on the button tap
	// instead of after a round trip.
	if err := a.validate(d, user); err != nil {
		r.alert(ctx, a.userError(err, "validate draft"))
		return
	}
	r.answer(ctx, "Сохраняю…")

	var (
		add  func(context.Context, io.Reader) error
		open string
		err  error
	)
	switch d.kind {
	case kindWish:
		var w domain.Wish
		w, err = a.svc.Wishes.Create(ctx, user, d.wishDraft())
		add = func(ctx context.Context, src io.Reader) error {
			_, err := a.svc.Wishes.AddImage(ctx, user, w.ID, src)
			return err
		}
		open = a.web.wishLink(w.ID)
	case kindRecipe:
		var rec domain.Recipe
		rec, err = a.svc.Recipes.Create(ctx, user, d.recipeDraft())
		add = func(ctx context.Context, src io.Reader) error {
			_, err := a.svc.Recipes.AddImage(ctx, user, rec.ID, src)
			return err
		}
		open = a.web.recipeLink(rec.ID)
	}
	if err != nil {
		a.say(ctx, d.chatID, a.userError(err, "save draft"))
		return
	}
	a.drafts.remove(user, d.id)
	deleteQuietly(ctx, a.api, a.log, d.chatID, d.promptID)

	failed := a.attachPhotos(ctx, d.photos, d.photoLimit(), add)
	text := renderSaved(d, failed)
	if err := editHTML(ctx, a.api, d.chatID, d.cardID, false, text, openKeyboard("Открыть ✨", open)); err != nil {
		a.log.Warn("bot: edit saved card failed", "err", err)
	}
}

func (a *app) validate(d draft, user domain.UserID) error {
	var err error
	if d.kind == kindWish {
		_, err = domain.NewWish(d.wishDraft(), user, a.now())
	} else {
		_, err = domain.NewRecipe(d.recipeDraft(), user, a.now())
	}
	return err
}

// attachPhotos downloads the pending photos and adds them to the saved
// entity. It returns how many could not be added.
func (a *app) attachPhotos(ctx context.Context, photos []string, limit int, add func(context.Context, io.Reader) error) int {
	failed := 0
	for i, fileID := range photos {
		if i >= limit {
			return failed + len(photos) - limit
		}
		data, err := a.files.fetch(ctx, fileID)
		if err == nil {
			err = add(ctx, bytes.NewReader(data))
		}
		if err != nil {
			failed++
			a.log.Warn("bot: attach photo failed", "err", err)
			if errors.Is(err, domain.ErrLimitExceeded) {
				return failed + len(photos) - i - 1
			}
		}
	}
	return failed
}

// showDraft sends the draft card or updates it in place.
func (a *app) showDraft(ctx context.Context, d *draft) {
	var (
		categories []domain.Category
		tags       []domain.RecipeTag
		err        error
	)
	switch d.view {
	case viewCategories:
		if categories, err = a.svc.Categories.List(ctx); err != nil {
			a.log.Error("bot: list categories failed", "err", err)
			d.view = viewMain
		}
	case viewCuisines, viewCourses:
		if tags, err = a.svc.RecipeTags.List(ctx); err != nil {
			a.log.Error("bot: list recipe tags failed", "err", err)
			d.view = viewMain
		}
	}
	text, kb := renderDraft(*d), draftKeyboard(*d, categories, tags)
	if d.cardID != 0 {
		err := editHTML(ctx, a.api, d.chatID, d.cardID, false, text, kb)
		if err == nil {
			return
		}
		if !errors.Is(err, tg.ErrorBadRequest) {
			a.log.Warn("bot: edit draft card failed", "err", err)
			return
		}
		// The card is gone (deleted by the user or too old): send a new one.
	}
	msg, err := sendHTML(ctx, a.api, d.chatID, text, kb)
	if err != nil {
		a.log.Warn("bot: send draft card failed", "err", err)
		return
	}
	d.cardID = msg.ID
}

// retireCard freezes the card of a draft that is no longer live.
func (a *app) retireCard(ctx context.Context, d draft, note string) {
	deleteQuietly(ctx, a.api, a.log, d.chatID, d.promptID)
	if d.cardID == 0 {
		return
	}
	h := newHTML(maxMessageLen).Text(note)
	if d.title != "" {
		h.NL().Italic("«" + d.title + "»")
	}
	if err := editHTML(ctx, a.api, d.chatID, d.cardID, false, h.String(), nil); err != nil {
		a.log.Debug("bot: retire draft card failed", "err", err)
	}
}

// dropAwaiting leaves the "awaiting <field>" state, e.g. when the user runs
// another command in between. It reports whether there was a prompt.
func (a *app) dropAwaiting(ctx context.Context, user domain.UserID) bool {
	d, ok := a.drafts.get(user)
	if !ok || d.awaiting == fieldNone {
		return false
	}
	deleteQuietly(ctx, a.api, a.log, d.chatID, d.promptID)
	d.awaiting, d.promptID = fieldNone, 0
	a.drafts.put(user, d)
	return true
}
