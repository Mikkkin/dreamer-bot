package bot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/nutrition"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

const (
	reelLink      = "https://www.instagram.com/reel/DItfAhKCJ3h/"
	recipeMessage = "Сырники\nТворог — 400 г\nЯйцо — 2 шт\nСахар — 2 ст. л."
)

// syrniki is the draft the fake importer turns every post into.
func syrniki(t *testing.T) domain.RecipeDraft {
	t.Helper()
	servings := 4
	return domain.RecipeDraft{
		Title: "Сырники",
		Body:  "1. Смешать творог с яйцом\n2. Обжарить",
		Ingredients: []domain.Ingredient{
			{Name: "Творог", Quantity: quantity(t, "400", "г")},
			{Name: "Яйцо", Quantity: quantity(t, "1", "шт")},
			{Name: "Сахар", Quantity: quantity(t, "1/2", "ч. л.")},
		},
		Servings: &servings,
	}
}

// importsAs makes the fake importer create d (with the cover image when
// it is not 0) and report warnings; notify, when set, sees the recipe like
// the service's notifier would.
func (e *testEnv) importsAs(d domain.RecipeDraft, cover domain.ImageID, warnings []string, notify func(domain.Recipe)) {
	e.svc.recipes.importer = func(ctx context.Context, actor domain.UserID, in service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		r, err := e.svc.recipes.Create(ctx, actor, d)
		if err != nil {
			return domain.Recipe{}, service.ImportReport{}, err
		}
		if cover != 0 {
			r.Images = []domain.Image{{ID: cover}}
			e.svc.recipes.put(r)
		}
		if notify != nil {
			notify(r)
		}
		return r, service.ImportReport{Source: service.ImportSourceInstagram, Parser: service.ImportParserRules, Warnings: warnings}, nil
	}
}

// settle waits for the imports started by the handled updates.
func (e *testEnv) settle() { e.app.jobs.wait() }

func (e *testEnv) editedTexts() []string {
	var out []string
	for _, p := range e.api.of("EditMessageText") {
		out = append(out, p.(*tg.EditMessageTextParams).Text)
	}
	return out
}

func TestImportLinkAnswersWithPhotoCard(t *testing.T) {
	e := newTestEnv(t)
	e.svc.images.data[7] = []byte("jpeg")
	e.importsAs(syrniki(t), 7, []string{"2 строки не распознаны"}, nil)

	e.handle(textUpdate(alice, "Смотри, что нашла! https://instagram.com/reels/DItfAhKCJ3h?igsh=abc123"))
	e.settle()

	reactions := e.api.of("SetMessageReaction")
	if len(reactions) != 1 {
		t.Fatalf("%d reactions", len(reactions))
	}
	rp := reactions[0].(*tg.SetMessageReactionParams)
	if rp.ChatID != int64(alice) || rp.MessageID != 10 || len(rp.Reaction) != 1 ||
		rp.Reaction[0].ReactionTypeEmoji == nil || rp.Reaction[0].ReactionTypeEmoji.Emoji != "👀" {
		t.Errorf("reaction %+v", rp)
	}
	if in := e.svc.recipes.importInputs(); len(in) != 1 || in[0] != (service.ImportInput{URL: reelLink}) {
		t.Errorf("imported %+v, want the canonical link", in)
	}
	status := e.api.lastSent(t)
	if status.Text != statusReadingCaption {
		t.Errorf("status %q", status.Text)
	}

	photos := e.api.of("SendPhoto")
	if len(photos) != 1 {
		t.Fatalf("%d photos", len(photos))
	}
	p := photos[0].(*tg.SendPhotoParams)
	head := "✅ <b>Сырники</b> · 3 ингредиента · 2 шага · 4 порции · ≈"
	if p.ChatID != int64(alice) || !strings.HasPrefix(p.Caption, head) || !strings.Contains(p.Caption, " ккал/порц\n⚠️ 2 строки не распознаны\n\n<i>Проверьте") {
		t.Errorf("card %v %q", p.ChatID, p.Caption)
	}
	assertSafeHTML(t, p.Caption)
	if urls := webAppURLs(p.ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?recipe=1"}) {
		t.Errorf("open button %v", urls)
	}
	if got := callbackData(p.ReplyMarkup); !slices.Equal(got, []string{"i:d:1"}) {
		t.Errorf("card callbacks %v", got)
	}
	// The status message made room for the card.
	deleted := e.api.of("DeleteMessage")
	if len(deleted) != 1 || deleted[0].(*tg.DeleteMessageParams).MessageID != messageID(t, e, statusReadingCaption) {
		t.Errorf("deleted %+v", deleted)
	}
	if _, ok := e.app.drafts.get(alice); ok {
		t.Error("an imported link also started a draft")
	}
}

// messageID finds the ID the fake API gave the message sent with text.
func messageID(t *testing.T, e *testEnv, text string) int {
	t.Helper()
	id := 0
	for i, c := range e.api.all() {
		if p, ok := c.params.(*tg.SendMessageParams); ok && p.Text == text {
			id = 101 + i // fakeAPI numbers every call from 101
		}
	}
	if id == 0 {
		t.Fatalf("no message %q", text)
	}
	return id
}

func TestImportLinkWithoutCoverEditsTheStatus(t *testing.T) {
	e := newTestEnv(t)
	e.importsAs(syrniki(t), 0, nil, nil)
	e.handle(textUpdate(alice, reelLink))
	e.settle()
	if e.api.count("SendPhoto") != 0 || e.api.count("SendMessage") != 1 {
		t.Fatalf("calls %+v", e.api.all())
	}
	edit := e.api.lastEdit(t)
	if edit.MessageID != messageID(t, e, statusReadingCaption) || !strings.HasPrefix(edit.Text, "✅ <b>Сырники</b> · 3 ингредиента") {
		t.Errorf("final edit %d %q", edit.MessageID, edit.Text)
	}
	if texts := buttonTexts(edit.ReplyMarkup); !slices.Equal(texts, []string{"Открыть ✨", "🗑 Удалить"}) {
		t.Errorf("buttons %v", texts)
	}
}

func TestImportLinkBehindTextLink(t *testing.T) {
	e := newTestEnv(t)
	e.importsAs(syrniki(t), 0, nil, nil)
	u := textUpdate(alice, "вот этот рецепт")
	u.Message.Entities = []models.MessageEntity{{Type: models.MessageEntityTypeTextLink, Offset: 4, Length: 4, URL: "https://m.instagram.com/p/DVs8ssPihOG/"}}
	e.handle(u)
	e.settle()
	if in := e.svc.recipes.importInputs(); len(in) != 1 || in[0].URL != "https://www.instagram.com/p/DVs8ssPihOG/" {
		t.Errorf("imported %+v", in)
	}
}

func TestImportLinkDuplicate(t *testing.T) {
	e := newTestEnv(t)
	existing := e.addRecipe(t, domain.RecipeDraft{Title: "Борщ"})
	e.svc.recipes.importer = func(context.Context, domain.UserID, service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		return existing, service.ImportReport{Source: service.ImportSourceInstagram, Duplicate: true}, nil
	}
	e.handle(textUpdate(alice, reelLink))
	e.settle()
	edit := e.api.lastEdit(t)
	if edit.Text != "📌 Уже есть: «Борщ»" {
		t.Errorf("duplicate %q", edit.Text)
	}
	if urls := webAppURLs(edit.ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?recipe=1"}) || len(callbackData(edit.ReplyMarkup)) != 0 {
		t.Errorf("duplicate buttons %+v (open only, never delete)", edit.ReplyMarkup)
	}
}

func TestImportLinkUnavailableThenCaption(t *testing.T) {
	e := newTestEnv(t)
	d := syrniki(t)
	e.svc.recipes.importer = func(ctx context.Context, actor domain.UserID, in service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		if in.URL != "" {
			return domain.Recipe{}, service.ImportReport{}, fmt.Errorf("fetch: %w", domain.ErrExternalUnavailable)
		}
		d := d
		if in.Link != "" { // the service gives a caption its post's link
			link := in.Link
			d.Link = &link
		}
		r, err := e.svc.recipes.Create(ctx, actor, d)
		return r, service.ImportReport{Source: service.ImportSourceText, Parser: service.ImportParserRules}, err
	}

	e.handle(textUpdate(alice, reelLink))
	e.settle()
	if edit := e.api.lastEdit(t).Text; !strings.HasPrefix(edit, instagramDown+"\n<i>") {
		t.Fatalf("503 answer %q", edit)
	}

	// The next text message is the caption: it is imported as text together
	// with the post's link (the service links the recipe and finds a post
	// imported before).
	e.handle(textUpdate(alice, recipeMessage))
	e.settle()
	in := e.svc.recipes.importInputs()
	if len(in) != 2 || in[1] != (service.ImportInput{Text: recipeMessage, Link: reelLink}) {
		t.Fatalf("imports %+v", in)
	}
	rec, err := e.svc.recipes.Get(context.Background(), 1)
	if err != nil || rec.Link == nil || *rec.Link != reelLink {
		t.Errorf("caption import not linked to its post: %+v, %v", rec.Link, err)
	}
	if edit := e.api.lastEdit(t).Text; !strings.HasPrefix(edit, "✅ <b>Сырники</b>") {
		t.Errorf("caption import answer %q", edit)
	}
	if _, ok := e.app.drafts.get(alice); ok {
		t.Error("the caption also started a draft")
	}

	// The wait is used up: the message after it is an ordinary draft.
	e.handle(textUpdate(alice, "Поездка в Токио 1200€"))
	e.settle()
	if n := len(e.svc.recipes.importInputs()); n != 2 {
		t.Errorf("%d imports, the wait must be used once", n)
	}
	if _, ok := e.app.drafts.get(alice); !ok {
		t.Error("no draft after the wait was used up")
	}
}

func TestCaptionWaitExpiresAndCancels(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, reelLink)) // no importer: Instagram is down
	e.settle()

	e.clock.Advance(captionWaitTTL)
	e.handle(textUpdate(alice, recipeMessage))
	e.settle()
	if n := len(e.svc.recipes.importInputs()); n != 1 {
		t.Errorf("%d imports, a text after 10 minutes is not the caption", n)
	}

	e.handle(textUpdate(alice, reelLink))
	e.settle()
	e.handle(commandUpdate(alice, "cancel"))
	if text := e.api.lastSent(t).Text; text != "Хорошо, оставил как было 👌" {
		t.Errorf("/cancel answered %q", text)
	}
	e.handle(textUpdate(alice, recipeMessage))
	e.settle()
	if n := len(e.svc.recipes.importInputs()); n != 2 {
		t.Errorf("%d imports, /cancel must stop waiting for the caption", n)
	}
}

func TestImportLinkNotARecipeOffersDraft(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		text string
		kind entityKind
	}{
		"not a recipe":  {service.ErrNotARecipe, notARecipeInPost, kindWish},
		"only in video": {service.ErrRecipeInVideo, recipeInVideo, kindRecipe},
	} {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			e.svc.recipes.importer = func(context.Context, domain.UserID, service.ImportInput) (domain.Recipe, service.ImportReport, error) {
				return domain.Recipe{}, service.ImportReport{}, tc.err
			}
			e.handle(textUpdate(alice, "Хочу такую лампу "+reelLink))
			e.settle()
			if want := tc.text + "\n" + draftOffer; e.api.lastEdit(t).Text != want {
				t.Errorf("answer %q, want %q", e.api.lastEdit(t).Text, want)
			}
			d, ok := e.app.drafts.get(alice)
			if !ok || d.link == nil || d.title != "Хочу такую лампу" || d.kind != tc.kind {
				t.Fatalf("draft %+v, %v", d, ok)
			}
			if card := e.api.lastSent(t); !strings.Contains(card.Text, "Хочу такую лампу") {
				t.Errorf("draft card %q", card.Text)
			}
			if n := len(e.svc.recipes.importInputs()); n != 1 {
				t.Errorf("%d imports", n)
			}
		})
	}
}

func TestImportStatusAdmitsWatchingTheVideo(t *testing.T) {
	e := newTestEnv(t)
	e.app.importHint = 5 * time.Millisecond
	release := make(chan struct{})
	d := syrniki(t)
	e.svc.recipes.importer = func(ctx context.Context, actor domain.UserID, _ service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		<-release
		r, err := e.svc.recipes.Create(ctx, actor, d)
		return r, service.ImportReport{Parser: service.ImportParserVideo}, err
	}
	e.handle(textUpdate(alice, reelLink))
	waitFor(t, "the video hint", func() bool { return slices.Contains(e.editedTexts(), statusWatchingVideo) })
	close(release)
	e.settle()
	edits := e.editedTexts()
	if len(edits) != 2 || edits[0] != statusWatchingVideo || !strings.HasPrefix(edits[1], "✅ <b>Сырники</b>") {
		t.Errorf("status edits %q", edits)
	}
	status := messageID(t, e, statusReadingCaption)
	for _, p := range e.api.of("EditMessageText") {
		if id := p.(*tg.EditMessageTextParams).MessageID; id != status {
			t.Errorf("edited message %d, want the status %d", id, status)
		}
	}
}

func TestImportFastAnswerHasNoVideoHint(t *testing.T) {
	e := newTestEnv(t)
	e.app.importHint = time.Hour
	e.importsAs(syrniki(t), 0, nil, nil)
	e.handle(textUpdate(alice, reelLink))
	e.settle()
	if slices.Contains(e.editedTexts(), statusWatchingVideo) {
		t.Error("video hint for a fast import")
	}
}

func TestImportStoppedByShutdown(t *testing.T) {
	e := newTestEnv(t)
	started := make(chan struct{})
	e.svc.recipes.importer = func(ctx context.Context, _ domain.UserID, _ service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		close(started)
		<-ctx.Done()
		return domain.Recipe{}, service.ImportReport{}, fmt.Errorf("fetch: %w", domain.ErrExternalUnavailable)
	}
	e.handle(textUpdate(alice, reelLink))
	<-started
	e.app.jobs.stop()
	if edit := e.api.lastEdit(t).Text; edit != importStopping {
		t.Errorf("answer on shutdown %q", edit)
	}
	if _, ok := e.app.captions.take(alice); ok {
		t.Error("shutdown is not Instagram being down")
	}
	e.handle(textUpdate(alice, reelLink))
	if n := len(e.svc.recipes.importInputs()); n != 1 {
		t.Errorf("%d imports after stop", n)
	}
	if edit := e.api.lastEdit(t); edit.Text != importStopping || edit.MessageID == e.api.of("EditMessageText")[0].(*tg.EditMessageTextParams).MessageID {
		t.Errorf("import after stop answered %d %q", edit.MessageID, edit.Text)
	}
}

func TestImportedCardDeleteCancelsPartnerNotice(t *testing.T) {
	e := newTestEnv(t)
	n := newNotifier(e.api, e.app.web, slog.New(slog.DiscardHandler))
	n.attach(e.svc.services())
	n.importQuiet, n.timeout = time.Hour, 5*time.Second
	e.app.notices = n
	e.importsAs(syrniki(t), 0, nil, func(r domain.Recipe) {
		n.RecipeImported(context.Background(), recipients(), r)
	})
	e.handle(textUpdate(alice, reelLink))
	e.settle()
	if _, ok := pendingImportDue(n, 1); !ok {
		t.Fatal("no pending import notice")
	}
	card := cardMessage(alice, messageID(t, e, statusReadingCaption))

	// 🗑 asks first; «Отмена» brings the import buttons back.
	e.handle(callbackUpdate(alice, "i:d:1", card))
	if got := callbackData(e.lastKeyboardEdit(t).ReplyMarkup); !slices.Equal(got, []string{"r:y:1", "i:n:1"}) {
		t.Fatalf("confirmation %v", got)
	}
	e.handle(callbackUpdate(alice, "i:n:1", card))
	if got := buttonTexts(e.lastKeyboardEdit(t).ReplyMarkup); !slices.Equal(got, []string{"Открыть ✨", "🗑 Удалить"}) {
		t.Errorf("restored buttons %v", got)
	}
	if _, err := e.svc.recipes.Get(context.Background(), 1); err != nil {
		t.Fatal("«Отмена» deleted the recipe")
	}

	e.handle(callbackUpdate(alice, "i:d:1", card))
	e.handle(callbackUpdate(alice, "r:y:1", card))
	if _, err := e.svc.recipes.Get(context.Background(), 1); err == nil {
		t.Error("recipe not deleted")
	}
	if got := e.lastAnswer(t); got != "Удалено 🗑 Партнёр о нём не узнает" {
		t.Errorf("toast %q", got)
	}
	if _, ok := pendingImportDue(n, 1); ok {
		t.Error("import notice still pending after delete")
	}
	n.drain()
	for _, c := range e.api.all() {
		switch p := c.params.(type) {
		case *tg.SendMessageParams:
			if chatOf(p.ChatID) == int64(bob) {
				t.Errorf("partner told about a deleted import: %q", p.Text)
			}
		case *tg.SendPhotoParams:
			if chatOf(p.ChatID) == int64(bob) {
				t.Errorf("partner told about a deleted import: %q", p.Caption)
			}
		}
	}
}

func TestDraftOffersImportOnlyForRecipeText(t *testing.T) {
	e := newTestEnv(t)
	for _, text := range []string{"Поездка в Токио 1200€", "Купить молоко 2 л", "Подарки: 2 книги и 3 кружки"} {
		e.handle(textUpdate(alice, text))
		card := e.api.lastSent(t)
		if slices.Contains(buttonTexts(card.ReplyMarkup), "📥 Разобрать как рецепт") || strings.Contains(card.Text, "Похоже на рецепт") {
			t.Errorf("%q offered as a recipe", text)
		}
	}

	e.handle(textUpdate(alice, recipeMessage))
	card := e.api.lastSent(t)
	d, _ := e.app.drafts.get(alice)
	want := callback{op: opDraftImport, draft: d.id}.String()
	if !slices.Contains(callbackData(card.ReplyMarkup), want) || !strings.Contains(card.Text, "Похоже на рецепт") {
		t.Fatalf("recipe text without «Разобрать»: %q %v", card.Text, buttonTexts(card.ReplyMarkup))
	}
	if d.kind != kindRecipe {
		t.Error("a recipe text starts a wish draft")
	}
	if n := len(e.svc.recipes.importInputs()); n != 0 {
		t.Fatalf("%d imports before the tap", n)
	}

	// The tap imports the message as text; the draft card shows how it goes.
	cardID := messageID(t, e, card.Text)
	e.importsAs(syrniki(t), 0, nil, nil)
	e.handle(textUpdate(alice, "x")) // a new draft would replace this one…
	e.handle(callbackUpdate(alice, want, cardMessage(alice, cardID)))
	if got := e.lastAnswer(t); got != "Черновик устарел" {
		t.Errorf("stale draft button answered %q", got)
	}
	e.handle(textUpdate(alice, recipeMessage)) // …so start over
	card = e.api.lastSent(t)
	cardID = messageID(t, e, card.Text)
	d, _ = e.app.drafts.get(alice)
	e.handle(callbackUpdate(alice, callback{op: opDraftImport, draft: d.id}.String(), cardMessage(alice, cardID)))
	e.settle()
	if in := e.svc.recipes.importInputs(); len(in) != 1 || in[0] != (service.ImportInput{Text: recipeMessage}) {
		t.Errorf("imports %+v", in)
	}
	edits := e.api.of("EditMessageText")
	var onCard []string
	for _, p := range edits {
		if p := p.(*tg.EditMessageTextParams); p.MessageID == cardID {
			onCard = append(onCard, p.Text)
		}
	}
	if len(onCard) != 2 || onCard[0] != statusReadingText || !strings.HasPrefix(onCard[1], "✅ <b>Сырники</b>") {
		t.Errorf("draft card edits %q", onCard)
	}
	if _, ok := e.app.drafts.get(alice); ok {
		t.Error("draft kept after the import")
	}
	if e.api.count("SetMessageReaction") != 0 {
		t.Error("a button tap got a reaction")
	}
}

func TestImportFromDraftAddsItsPhotos(t *testing.T) {
	e := newTestEnv(t)
	e.importsAs(syrniki(t), 0, nil, nil)
	e.handle(textUpdate(alice, recipeMessage))
	e.handle(photoUpdate(alice, "p1", "", "")) // joins the recent draft
	d, _ := e.app.drafts.get(alice)
	if len(d.photos) != 1 || d.source == "" {
		t.Fatalf("draft %+v", d)
	}
	e.handle(callbackUpdate(alice, callback{op: opDraftImport, draft: d.id}.String(), cardMessage(alice, d.cardID)))
	e.settle()
	// The download fails (no file server), which is reported, not fatal.
	if edit := e.api.lastEdit(t).Text; !strings.Contains(edit, "⚠️ Не получилось добавить фото: 1") {
		t.Errorf("answer %q", edit)
	}
}

func TestRecipeCardShowsServingsFractionsAndEstimate(t *testing.T) {
	servings := 4
	r := domain.Recipe{
		Title: "Блины",
		Ingredients: []domain.Ingredient{
			{Name: "Мука пшеничная", Quantity: quantity(t, "1 1/2", "стакан")},
			{Name: "Сахар", Quantity: quantity(t, "½", "чайной ложки")},
			{Name: "Яйцо", Quantity: quantity(t, "2", "шт")},
			{Name: "Соль", Quantity: quantity(t, "", "по вкусу")},
			{Name: "Гуанчале", Quantity: quantity(t, "100", "г")},
		},
		Servings:  &servings,
		CreatedAt: time.Now(),
	}
	res := nutrition.Default().Compute(r.Ingredients, servings)
	if res.Coverage.Counted != 3 || res.Coverage.Total != 4 || res.PerServing == nil {
		t.Fatalf("table changed: %+v", res.Coverage)
	}
	v := *res.PerServing
	line := fmt.Sprintf("🔥 ≈%d ккал · Б %s · Ж %s · У %s (на порцию, по 3 из 4 ингредиентов)",
		(v.Kcal+5)/10, domain.FormatTenths(v.Protein), domain.FormatTenths(v.Fat), domain.FormatTenths(v.Carbs))
	meta := entityMeta{author: "Аня"}
	out := renderRecipe(r, meta, time.UTC, maxMessageLen, messageBodyExcerpt)
	for _, want := range []string{
		"🍽 4 порции\n" + line,
		"• Мука пшеничная — 1½\u00a0стакана",
		"• Сахар — ½\u00a0чайной ложки",
		"• Яйцо — 2\u00a0шт",
		"• Соль — по вкусу",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("card lacks %q:\n%s", want, out)
		}
	}
	if caption := renderRecipe(r, meta, time.UTC, maxCaptionLen, captionBodyExcerpt); !strings.Contains(caption, "🍽 4 порции\n"+line) {
		t.Errorf("caption %q", caption)
	}

	// Without servings the estimate is per 100 g.
	r.Servings = nil
	if out := renderRecipe(r, meta, time.UTC, maxMessageLen, messageBodyExcerpt); !strings.Contains(out, "(на 100 г, по 3 из 4 ингредиентов)") || strings.Contains(out, "порци") {
		t.Errorf("card without servings %q", out)
	}
	// Own КБЖУ wins over the estimate.
	r.Servings = &servings
	r.Nutrition = &domain.Nutrition{KcalPer100: 2000, ProteinPer100: 60, FatPer100: 50, CarbsPer100: 300, WeightGrams: 600, Servings: 4}
	if out := renderRecipe(r, meta, time.UTC, maxMessageLen, messageBodyExcerpt); strings.Contains(out, "≈") || !strings.Contains(out, "🔥 300 ккал · Б 9 · Ж 7,5 · У 45 (на порцию)") {
		t.Errorf("card with own КБЖУ %q", out)
	}
	// Every ingredient counted.
	r.Nutrition = nil
	r.Ingredients = r.Ingredients[:3]
	if out := renderRecipe(r, meta, time.UTC, maxMessageLen, messageBodyExcerpt); !strings.Contains(out, "(на порцию, по всем ингредиентам)") {
		t.Errorf("fully counted card %q", out)
	}
	// Nothing counted: no line at all.
	r.Ingredients = []domain.Ingredient{{Name: "Гуанчале", Quantity: quantity(t, "100", "г")}}
	if out := renderRecipe(r, meta, time.UTC, maxMessageLen, messageBodyExcerpt); strings.Contains(out, "🔥") {
		t.Errorf("estimate without counted ingredients %q", out)
	}
}

func TestRecipeSummary(t *testing.T) {
	one := 1
	r := domain.Recipe{
		Title: "Чай",
		Body:  "1. Вскипятить воду\n2) Заварить\nПодавать горячим\n\n" + sourceHeading + "\n1. строка подписи\n2. ещё одна",
		Ingredients: []domain.Ingredient{
			{Name: "Чай чёрный", Quantity: quantity(t, "1", "ч. л.")},
		},
		Servings:  &one,
		Nutrition: &domain.Nutrition{KcalPer100: 10, WeightGrams: 250, Servings: 1},
	}
	if got := recipeSummary(r); got != "1 ингредиент · 2 шага · 1 порция · 3 ккал/порц" {
		t.Errorf("summary %q", got)
	}
	r.Nutrition.WeightGrams = 0 // own values without weight: no per-serving kcal
	r.Servings = nil
	if got := recipeSummary(r); got != "1 ингредиент · 2 шага" {
		t.Errorf("summary %q", got)
	}
	if got := recipeSummary(domain.Recipe{Title: "Пусто"}); got != "" {
		t.Errorf("empty summary %q", got)
	}
}

func TestInstagramLinkAndRecipeText(t *testing.T) {
	for text, want := range map[string]string{
		reelLink: reelLink,
		"instagram.com/p/DVs8ssPihOG/?igsh=1 — вкусно":      "https://www.instagram.com/p/DVs8ssPihOG/",
		"https://www.instagram.com/stories/user/123/":       "",
		"https://instagram.com.evil.example/p/DVs8ssPihOG/": "",
		"https://youtube.com/watch?v=x":                     "",
	} {
		got, ok := instagramLink(text, nil)
		if got != want || ok != (want != "") {
			t.Errorf("instagramLink(%q) = %q, %v", text, got, ok)
		}
	}
}

// An import runs outside the user's lock: the chat goes on meanwhile.
func TestImportDoesNotHoldTheUserLock(t *testing.T) {
	e := newTestEnv(t)
	release := make(chan struct{})
	var once sync.Once
	d := syrniki(t)
	e.svc.recipes.importer = func(ctx context.Context, actor domain.UserID, _ service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		<-release
		r, err := e.svc.recipes.Create(ctx, actor, d)
		return r, service.ImportReport{}, err
	}
	defer once.Do(func() { close(release) })
	e.handle(textUpdate(alice, reelLink))
	done := make(chan struct{})
	go func() {
		e.handle(commandUpdate(alice, "help"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the chat waited for the import")
	}
	once.Do(func() { close(release) })
	e.settle()
}

func TestEndToEndImportReactsThroughLibraryClient(t *testing.T) {
	api := &fakeBotAPI{}
	srv := httptest.NewServer(api)
	defer srv.Close()
	b, err := newWithClientOptions(context.Background(), Options{
		Token:     testToken,
		Whitelist: testWhitelist(),
		WebAppURL: testWebApp,
	}, tg.WithServerURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	svc := newFakeServices(time.Now)
	imported := make(chan service.ImportInput, 1)
	svc.recipes.importer = func(ctx context.Context, actor domain.UserID, in service.ImportInput) (domain.Recipe, service.ImportReport, error) {
		imported <- in
		r, err := svc.recipes.Create(ctx, actor, domain.RecipeDraft{Title: "Сырники"})
		return r, service.ImportReport{}, err
	}
	b.Attach(svc.services())
	api.push(privateTextUpdate(1, int64(alice), "https://www.instagram.com/p/DVs8ssPihOG/?igsh=abc"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	select {
	case in := <-imported:
		if in.URL != "https://www.instagram.com/p/DVs8ssPihOG/" {
			t.Errorf("imported %+v", in)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("link not imported")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	reactions := api.called("setMessageReaction")
	if len(reactions) != 1 {
		t.Fatalf("%d reactions", len(reactions))
	}
	if r := reactions[0].Get("reaction"); r != `[{"type":"emoji","emoji":"👀"}]` || reactions[0].Get("message_id") != "1" {
		t.Errorf("reaction %v", reactions[0])
	}
}
