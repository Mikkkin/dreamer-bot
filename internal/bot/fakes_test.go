package bot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

const (
	testToken   = "123456789:AAH5YkoiEuPk8-FZa32hStHTqXiLPtAEhx8"
	alice       = domain.UserID(111)
	bob         = domain.UserID(222)
	stranger    = domain.UserID(999)
	testWebApp  = "https://dreams.example/"
	botUsername = "dreamer_test_bot"
)

// call is one recorded Bot API request.
type call struct {
	method string
	params any
}

// fakeAPI records every Bot API call. Sends can be made to block until
// release is closed, and any method can be made to fail.
type fakeAPI struct {
	mu      sync.Mutex
	calls   []call
	nextID  int
	fail    map[string]error
	release chan struct{} // when set, SendMessage and SendPhoto wait for it
	file    *models.File
	link    string
}

func newFakeAPI() *fakeAPI { return &fakeAPI{nextID: 100, fail: map[string]error{}} }

func (f *fakeAPI) record(method string, params any) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{method, params})
	f.nextID++
	return f.nextID, f.fail[method]
}

func (f *fakeAPI) wait(ctx context.Context) {
	f.mu.Lock()
	ch := f.release
	f.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	case <-ctx.Done():
	}
}

func chatOf(id any) int64 {
	if v, ok := id.(int64); ok {
		return v
	}
	return 0
}

func (f *fakeAPI) SendMessage(ctx context.Context, p *tg.SendMessageParams) (*models.Message, error) {
	f.wait(ctx)
	id, err := f.record("SendMessage", p)
	if err != nil {
		return nil, err
	}
	return &models.Message{ID: id, Chat: models.Chat{ID: chatOf(p.ChatID), Type: models.ChatTypePrivate}, Text: p.Text}, nil
}

func (f *fakeAPI) SendPhoto(ctx context.Context, p *tg.SendPhotoParams) (*models.Message, error) {
	f.wait(ctx)
	if up, ok := p.Photo.(*models.InputFileUpload); ok {
		_, _ = io.Copy(io.Discard, up.Data) // like the real client, consume the upload
	}
	id, err := f.record("SendPhoto", p)
	if err != nil {
		return nil, err
	}
	return &models.Message{ID: id, Chat: models.Chat{ID: chatOf(p.ChatID)}, Photo: []models.PhotoSize{{FileID: "sent"}}}, nil
}

func (f *fakeAPI) EditMessageText(_ context.Context, p *tg.EditMessageTextParams) (*models.Message, error) {
	_, err := f.record("EditMessageText", p)
	return &models.Message{ID: p.MessageID}, err
}

func (f *fakeAPI) EditMessageCaption(_ context.Context, p *tg.EditMessageCaptionParams) (*models.Message, error) {
	_, err := f.record("EditMessageCaption", p)
	return &models.Message{ID: p.MessageID}, err
}

func (f *fakeAPI) EditMessageReplyMarkup(_ context.Context, p *tg.EditMessageReplyMarkupParams) (*models.Message, error) {
	_, err := f.record("EditMessageReplyMarkup", p)
	return &models.Message{ID: p.MessageID}, err
}

func (f *fakeAPI) DeleteMessage(_ context.Context, p *tg.DeleteMessageParams) (bool, error) {
	_, err := f.record("DeleteMessage", p)
	return err == nil, err
}

func (f *fakeAPI) AnswerCallbackQuery(_ context.Context, p *tg.AnswerCallbackQueryParams) (bool, error) {
	_, err := f.record("AnswerCallbackQuery", p)
	return err == nil, err
}

func (f *fakeAPI) SetMessageReaction(_ context.Context, p *tg.SetMessageReactionParams) (bool, error) {
	_, err := f.record("SetMessageReaction", p)
	return err == nil, err
}

func (f *fakeAPI) LeaveChat(_ context.Context, p *tg.LeaveChatParams) (bool, error) {
	_, err := f.record("LeaveChat", p)
	return err == nil, err
}

func (f *fakeAPI) GetFile(_ context.Context, p *tg.GetFileParams) (*models.File, error) {
	if _, err := f.record("GetFile", p); err != nil {
		return nil, err
	}
	if f.file != nil {
		return f.file, nil
	}
	return &models.File{FileID: p.FileID, FilePath: "photos/" + p.FileID + ".jpg"}, nil
}

func (f *fakeAPI) FileDownloadLink(file *models.File) string {
	return f.link + "/file/bot" + testToken + "/" + file.FilePath
}

func (f *fakeAPI) DeleteWebhook(_ context.Context, p *tg.DeleteWebhookParams) (bool, error) {
	_, err := f.record("DeleteWebhook", p)
	return err == nil, err
}

func (f *fakeAPI) SetMyCommands(_ context.Context, p *tg.SetMyCommandsParams) (bool, error) {
	_, err := f.record("SetMyCommands", p)
	return err == nil, err
}

func (f *fakeAPI) SetChatMenuButton(_ context.Context, p *tg.SetChatMenuButtonParams) (bool, error) {
	_, err := f.record("SetChatMenuButton", p)
	return err == nil, err
}

func (f *fakeAPI) all() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeAPI) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

func (f *fakeAPI) of(method string) []any {
	var out []any
	for _, c := range f.all() {
		if c.method == method {
			out = append(out, c.params)
		}
	}
	return out
}

func (f *fakeAPI) count(method string) int { return len(f.of(method)) }

// lastSent returns the params of the last SendMessage call.
func (f *fakeAPI) lastSent(t *testing.T) *tg.SendMessageParams {
	t.Helper()
	sent := f.of("SendMessage")
	if len(sent) == 0 {
		t.Fatal("no SendMessage call")
	}
	return sent[len(sent)-1].(*tg.SendMessageParams)
}

// lastEdit returns the params of the last EditMessageText call.
func (f *fakeAPI) lastEdit(t *testing.T) *tg.EditMessageTextParams {
	t.Helper()
	edits := f.of("EditMessageText")
	if len(edits) == 0 {
		t.Fatal("no EditMessageText call")
	}
	return edits[len(edits)-1].(*tg.EditMessageTextParams)
}

// answers returns the texts of all callback answers.
func (f *fakeAPI) answers() []string {
	var out []string
	for _, p := range f.of("AnswerCallbackQuery") {
		out = append(out, p.(*tg.AnswerCallbackQueryParams).Text)
	}
	return out
}

// callbackData lists every callback payload of an inline keyboard.
func callbackData(m models.ReplyMarkup) []string {
	kb, ok := m.(*models.InlineKeyboardMarkup)
	if !ok || kb == nil {
		return nil
	}
	var out []string
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			if b.CallbackData != "" {
				out = append(out, b.CallbackData)
			}
		}
	}
	return out
}

// buttonTexts lists every button label of an inline keyboard.
func buttonTexts(m models.ReplyMarkup) []string {
	kb, ok := m.(*models.InlineKeyboardMarkup)
	if !ok || kb == nil {
		return nil
	}
	var out []string
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			out = append(out, b.Text)
		}
	}
	return out
}

func webAppURLs(m models.ReplyMarkup) []string {
	kb, ok := m.(*models.InlineKeyboardMarkup)
	if !ok || kb == nil {
		return nil
	}
	var out []string
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			if b.WebApp != nil {
				out = append(out, b.WebApp.URL)
			}
		}
	}
	return out
}

// clock is a controllable time source.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeServices are in-memory use cases good enough for handler tests.
type fakeServices struct {
	wishes     *fakeWishes
	recipes    *fakeRecipes
	tags       *fakeRecipeTags
	shopping   *fakeShopping
	categories *fakeCategories
	images     *fakeImages
	stats      *fakeStats
	users      *fakeUsers
}

func newFakeServices(now func() time.Time) *fakeServices {
	recipes := &fakeRecipes{now: now, items: map[domain.RecipeID]domain.Recipe{}, cooks: map[domain.CookID]domain.Cook{}}
	return &fakeServices{
		wishes:     &fakeWishes{now: now, items: map[domain.WishID]domain.Wish{}},
		recipes:    recipes,
		tags:       newFakeRecipeTags(),
		shopping:   &fakeShopping{now: now, recipes: recipes},
		categories: newFakeCategories(),
		images:     &fakeImages{data: map[domain.ImageID][]byte{}},
		stats:      &fakeStats{},
		users:      &fakeUsers{items: map[domain.UserID]domain.User{}},
	}
}

func (f *fakeServices) services() *service.Services {
	return &service.Services{
		Wishes:     f.wishes,
		Recipes:    f.recipes,
		RecipeTags: f.tags,
		Shopping:   f.shopping,
		Categories: f.categories,
		Images:     f.images,
		Stats:      f.stats,
		Users:      f.users,
	}
}

var errNotImplemented = errors.New("not implemented in fake")

type fakeWishes struct {
	mu        sync.Mutex
	now       func() time.Time
	seq       int64
	imgSeq    int64
	savingSeq int64
	items     map[domain.WishID]domain.Wish
	created   []domain.WishDraft
	added     map[domain.WishID][][]byte
	savings   []domain.Saving
}

func (f *fakeWishes) Create(_ context.Context, actor domain.UserID, d domain.WishDraft) (domain.Wish, error) {
	w, err := domain.NewWish(d, actor, f.now())
	if err != nil {
		return domain.Wish{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	w.ID = domain.WishID(f.seq)
	f.items[w.ID] = w
	f.created = append(f.created, d)
	return w, nil
}

func (f *fakeWishes) put(w domain.Wish) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[w.ID] = w
}

func (f *fakeWishes) Get(_ context.Context, id domain.WishID) (domain.Wish, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.items[id]
	if !ok {
		return domain.Wish{}, domain.ErrNotFound
	}
	return w, nil
}

func (f *fakeWishes) List(_ context.Context, flt domain.WishFilter) ([]domain.Wish, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Wish
	for _, w := range f.items {
		if flt.Status == nil || *flt.Status == w.Status {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, func(a, b domain.Wish) int { return int(b.ID - a.ID) })
	return out, nil
}

func (f *fakeWishes) Update(context.Context, domain.UserID, domain.WishID, domain.WishPatch) (domain.Wish, error) {
	return domain.Wish{}, errNotImplemented
}

func (f *fakeWishes) SetStatus(_ context.Context, _ domain.UserID, id domain.WishID, s domain.Status) (domain.Wish, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.items[id]
	if !ok {
		return domain.Wish{}, domain.ErrNotFound
	}
	if _, err := w.SetStatus(s, f.now()); err != nil {
		return domain.Wish{}, err
	}
	f.items[id] = w
	return w, nil
}

func (f *fakeWishes) Delete(_ context.Context, _ domain.UserID, id domain.WishID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

func (f *fakeWishes) AddImage(_ context.Context, _ domain.UserID, id domain.WishID, src io.Reader) (domain.Image, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return domain.Image{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.items[id]
	if !ok {
		return domain.Image{}, domain.ErrNotFound
	}
	if !w.CanAddImage() {
		return domain.Image{}, domain.ErrLimitExceeded
	}
	f.imgSeq++
	img := domain.Image{ID: domain.ImageID(f.imgSeq), Bytes: int64(len(data))}
	w.Images = append(w.Images, img)
	f.items[id] = w
	if f.added == nil {
		f.added = map[domain.WishID][][]byte{}
	}
	f.added[id] = append(f.added[id], data)
	return img, nil
}

func (f *fakeWishes) RemoveImage(context.Context, domain.UserID, domain.WishID, domain.ImageID) error {
	return errNotImplemented
}

// AddSaving behaves like the real use case: the domain validates the
// amount and currency, the total grows and a «Хотим» wish moves to «Копим».
func (f *fakeWishes) AddSaving(_ context.Context, actor domain.UserID, id domain.WishID, amount domain.Money, note string) (domain.Saving, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.items[id]
	if !ok {
		return domain.Saving{}, domain.ErrNotFound
	}
	s, err := domain.NewSaving(w, amount, actor, note, f.now())
	if err != nil {
		return domain.Saving{}, err
	}
	f.savingSeq++
	s.ID = domain.SavingID(f.savingSeq)
	f.savings = append(f.savings, s)
	total := s.Amount
	if w.Saved != nil {
		total.Minor += w.Saved.Minor
	}
	w.Saved = &total
	if w.Status == domain.StatusWant {
		if _, err := w.SetStatus(domain.StatusProgress, f.now()); err != nil {
			return domain.Saving{}, err
		}
	}
	f.items[id] = w
	return s, nil
}

func (f *fakeWishes) ListSavings(_ context.Context, id domain.WishID) ([]domain.Saving, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Saving
	for _, s := range f.savings {
		if s.WishID == id {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeWishes) RemoveSaving(context.Context, domain.UserID, domain.WishID, domain.SavingID) error {
	return errNotImplemented
}

func (f *fakeWishes) savingsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.savings)
}

type fakeRecipes struct {
	mu      sync.Mutex
	now     func() time.Time
	seq     int64
	cookSeq int64
	items   map[domain.RecipeID]domain.Recipe
	cooks   map[domain.CookID]domain.Cook
	created []domain.RecipeDraft
	random  []domain.RecipeID // IDs returned by Random in order (cycled)
	rolls   int
	// importer answers Import; inputs records every call.
	importer func(ctx context.Context, actor domain.UserID, in service.ImportInput) (domain.Recipe, service.ImportReport, error)
	inputs   []service.ImportInput
	patches  []domain.RecipePatch
	images   int // photos added with AddImage
}

func (f *fakeRecipes) Create(_ context.Context, actor domain.UserID, d domain.RecipeDraft) (domain.Recipe, error) {
	r, err := domain.NewRecipe(d, actor, f.now())
	if err != nil {
		return domain.Recipe{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	r.ID = domain.RecipeID(f.seq)
	f.items[r.ID] = r
	f.created = append(f.created, d)
	return r, nil
}

func (f *fakeRecipes) Get(_ context.Context, id domain.RecipeID) (domain.Recipe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.items[id]
	if !ok {
		return domain.Recipe{}, domain.ErrNotFound
	}
	return r, nil
}

func (f *fakeRecipes) List(context.Context, domain.RecipeFilter) ([]domain.Recipe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Recipe
	for _, r := range f.items {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b domain.Recipe) int { return int(b.ID - a.ID) })
	return out, nil
}

func (f *fakeRecipes) Random(context.Context) (domain.Recipe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.items) == 0 {
		return domain.Recipe{}, domain.ErrNotFound
	}
	id := f.random[f.rolls%len(f.random)]
	f.rolls++
	return f.items[id], nil
}

func (f *fakeRecipes) Update(_ context.Context, _ domain.UserID, id domain.RecipeID, p domain.RecipePatch) (domain.Recipe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.items[id]
	if !ok {
		return domain.Recipe{}, domain.ErrNotFound
	}
	if err := r.Apply(p, f.now()); err != nil {
		return domain.Recipe{}, err
	}
	f.items[id] = r
	f.patches = append(f.patches, p)
	return r, nil
}

func (f *fakeRecipes) Delete(_ context.Context, _ domain.UserID, id domain.RecipeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, id)
	return nil
}

func (f *fakeRecipes) AddImage(context.Context, domain.UserID, domain.RecipeID, io.Reader) (domain.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.images++
	return domain.Image{}, nil
}

func (f *fakeRecipes) RemoveImage(context.Context, domain.UserID, domain.RecipeID, domain.ImageID) error {
	return errNotImplemented
}

func (f *fakeRecipes) put(r domain.Recipe) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[r.ID] = r
}

func (f *fakeRecipes) Cook(_ context.Context, actor domain.UserID, id domain.RecipeID, in *service.RatingInput) (domain.Cook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[id]; !ok {
		return domain.Cook{}, domain.ErrNotFound
	}
	c := domain.Cook{RecipeID: id, CookedBy: actor, CookedAt: f.now()}
	if in != nil {
		rt, err := domain.NewRating(actor, in.Stars, in.Comment, f.now())
		if err != nil {
			return domain.Cook{}, err
		}
		c.Ratings = []domain.Rating{rt}
	}
	f.cookSeq++
	c.ID = domain.CookID(f.cookSeq)
	f.cooks[c.ID] = c
	f.summarize(id)
	return c, nil
}

func (f *fakeRecipes) Rate(_ context.Context, actor domain.UserID, id domain.RecipeID, cookID domain.CookID, in service.RatingInput) (domain.Cook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cooks[cookID]
	if !ok || c.RecipeID != id {
		return domain.Cook{}, domain.ErrNotFound
	}
	rt, err := domain.NewRating(actor, in.Stars, in.Comment, f.now())
	if err != nil {
		return domain.Cook{}, err
	}
	c.Ratings = slices.DeleteFunc(slices.Clone(c.Ratings), func(r domain.Rating) bool { return r.UserID == actor })
	c.Ratings = append(c.Ratings, rt)
	f.cooks[cookID] = c
	f.summarize(id)
	return c, nil
}

// summarize refreshes the cooking summary of a recipe; f.mu must be held.
func (f *fakeRecipes) summarize(id domain.RecipeID) {
	r, ok := f.items[id]
	if !ok {
		return
	}
	var s domain.CookingSummary
	for _, c := range f.cooks {
		if c.RecipeID != id {
			continue
		}
		s.Count++
		if at := c.CookedAt; s.LastCookedAt == nil || at.After(*s.LastCookedAt) {
			s.LastCookedAt = &at
		}
		for _, rt := range c.Ratings {
			s.RatingSum += rt.Stars
			s.RatingCount++
		}
	}
	r.Cooking = s
	f.items[id] = r
}

func (f *fakeRecipes) ListCooks(_ context.Context, id domain.RecipeID) ([]domain.Cook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Cook
	for _, c := range f.cooks {
		if c.RecipeID == id {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b domain.Cook) int { return int(b.ID - a.ID) })
	return out, nil
}

func (f *fakeRecipes) RemoveCook(context.Context, domain.UserID, domain.RecipeID, domain.CookID) error {
	return errNotImplemented
}

func (f *fakeRecipes) cookList() []domain.Cook {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Cook
	for _, c := range f.cooks {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b domain.Cook) int { return int(a.ID - b.ID) })
	return out
}

// fakeRecipeTags serves the default tags: cuisines get IDs 1–6, courses 7–15.
type fakeRecipeTags struct {
	mu    sync.Mutex
	items []domain.RecipeTag
	fail  error
}

func newFakeRecipeTags() *fakeRecipeTags {
	tags := domain.DefaultRecipeTags()
	for i := range tags {
		tags[i].ID = domain.RecipeTagID(i + 1)
	}
	return &fakeRecipeTags{items: tags}
}

func (f *fakeRecipeTags) List(context.Context) ([]domain.RecipeTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	return slices.Clone(f.items), nil
}

func (f *fakeRecipeTags) Create(context.Context, domain.UserID, domain.TagKind, string, string) (domain.RecipeTag, error) {
	return domain.RecipeTag{}, errNotImplemented
}

func (f *fakeRecipeTags) Update(context.Context, domain.UserID, domain.RecipeTagID, domain.RecipeTagPatch) (domain.RecipeTag, error) {
	return domain.RecipeTag{}, errNotImplemented
}

func (f *fakeRecipeTags) Delete(_ context.Context, _ domain.UserID, id domain.RecipeTagID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = slices.DeleteFunc(f.items, func(t domain.RecipeTag) bool { return t.ID == id })
	return nil
}

// fakeShopping is the shared list with the real merge rule.
type fakeShopping struct {
	mu         sync.Mutex
	now        func() time.Time
	recipes    *fakeRecipes
	seq        int64
	items      []domain.ShoppingItem
	fromRecipe []domain.RecipeID
}

func (f *fakeShopping) List(context.Context) ([]domain.ShoppingItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.items)
	slices.SortStableFunc(out, func(a, b domain.ShoppingItem) int {
		if a.Checked != b.Checked {
			if a.Checked {
				return 1
			}
			return -1
		}
		return int(a.ID - b.ID)
	})
	return out, nil
}

func (f *fakeShopping) Add(_ context.Context, actor domain.UserID, drafts []domain.ShoppingDraft) ([]domain.ShoppingItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.add(actor, drafts)
}

// add merges or appends every draft; f.mu must be held.
func (f *fakeShopping) add(actor domain.UserID, drafts []domain.ShoppingDraft) ([]domain.ShoppingItem, error) {
	var out []domain.ShoppingItem
	for _, d := range drafts {
		it, err := domain.NewShoppingItem(d, actor, f.now())
		if err != nil {
			return nil, err
		}
		merged := false
		for i := range f.items {
			if strings.EqualFold(f.items[i].Name, it.Name) && f.items[i].MergeInto(it.Quantity, f.now()) {
				out, merged = append(out, f.items[i]), true
				break
			}
		}
		if merged {
			continue
		}
		f.seq++
		it.ID = domain.ShoppingItemID(f.seq)
		f.items = append(f.items, it)
		out = append(out, it)
	}
	return out, nil
}

func (f *fakeShopping) AddFromRecipe(ctx context.Context, actor domain.UserID, id domain.RecipeID, positions []int) ([]domain.ShoppingItem, error) {
	r, err := f.recipes.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if positions != nil {
		return nil, errNotImplemented
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fromRecipe = append(f.fromRecipe, id)
	drafts := make([]domain.ShoppingDraft, 0, len(r.Ingredients))
	for _, ing := range r.Ingredients {
		drafts = append(drafts, domain.ShoppingDraft{Name: ing.Name, Quantity: ing.Quantity, RecipeID: &id})
	}
	return f.add(actor, drafts)
}

func (f *fakeShopping) Update(_ context.Context, _ domain.UserID, id domain.ShoppingItemID, p domain.ShoppingPatch) (domain.ShoppingItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.items {
		if f.items[i].ID == id {
			if err := f.items[i].Apply(p, f.now()); err != nil {
				return domain.ShoppingItem{}, err
			}
			return f.items[i], nil
		}
	}
	return domain.ShoppingItem{}, domain.ErrNotFound
}

func (f *fakeShopping) Delete(_ context.Context, _ domain.UserID, id domain.ShoppingItemID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.items)
	f.items = slices.DeleteFunc(f.items, func(it domain.ShoppingItem) bool { return it.ID == id })
	if len(f.items) == n {
		return domain.ErrNotFound
	}
	return nil
}

func (f *fakeShopping) ClearChecked(context.Context, domain.UserID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.items)
	f.items = slices.DeleteFunc(f.items, func(it domain.ShoppingItem) bool { return it.Checked })
	return n - len(f.items), nil
}

func (f *fakeShopping) snapshot() []domain.ShoppingItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.items)
}

type fakeCategories struct{ items []domain.Category }

func newFakeCategories() *fakeCategories {
	cats := domain.DefaultCategories()
	for i := range cats {
		cats[i].ID = domain.CategoryID(i + 1)
	}
	return &fakeCategories{items: cats}
}

func (f *fakeCategories) List(context.Context) ([]domain.Category, error) {
	return slices.Clone(f.items), nil
}

func (f *fakeCategories) Get(_ context.Context, id domain.CategoryID) (domain.Category, error) {
	for _, c := range f.items {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.Category{}, domain.ErrNotFound
}

func (f *fakeCategories) Create(context.Context, domain.UserID, string, string) (domain.Category, error) {
	return domain.Category{}, errNotImplemented
}

func (f *fakeCategories) Update(context.Context, domain.UserID, domain.CategoryID, domain.CategoryPatch) (domain.Category, error) {
	return domain.Category{}, errNotImplemented
}

func (f *fakeCategories) Delete(context.Context, domain.UserID, domain.CategoryID) error {
	return errNotImplemented
}

type fakeImages struct {
	mu   sync.Mutex
	data map[domain.ImageID][]byte
}

type readSeekNopCloser struct{ *bytes.Reader }

func (readSeekNopCloser) Close() error { return nil }

func (f *fakeImages) Open(_ context.Context, id domain.ImageID, _ service.ImageVariant) (service.ImageFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.data[id]
	if !ok {
		return service.ImageFile{}, domain.ErrNotFound
	}
	return service.ImageFile{Content: readSeekNopCloser{bytes.NewReader(data)}, ContentType: "image/jpeg", Size: int64(len(data))}, nil
}

type fakeStats struct{ stats domain.Stats }

func (f *fakeStats) Compute(context.Context) (domain.Stats, error) { return f.stats, nil }

type fakeUsers struct {
	mu      sync.Mutex
	items   map[domain.UserID]domain.User
	touches []domain.User
}

func (f *fakeUsers) Touch(_ context.Context, u domain.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touches = append(f.touches, u)
	f.items[u.ID] = u
	return nil
}

func (f *fakeUsers) Get(_ context.Context, id domain.UserID) (domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.items[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (f *fakeUsers) List(context.Context) ([]domain.User, error) { return nil, errNotImplemented }

func (f *fakeUsers) Partners(context.Context, domain.UserID) ([]domain.User, error) {
	return nil, errNotImplemented
}

func (f *fakeUsers) touchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.touches)
}

// testEnv is an app wired to fakes.
type testEnv struct {
	app   *app
	api   *fakeAPI
	svc   *fakeServices
	clock *clock
	logs  *bytes.Buffer
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	api := newFakeAPI()
	clk := newClock()
	svc := newFakeServices(clk.Now)
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	web := &webApp{}
	web.set(testWebApp)
	s := svc.services()
	a := &app{
		api:      api,
		files:    newDownloader(api, 10<<20, newRedactor(testToken)),
		svc:      s,
		drafts:   newDraftStore(draftTTL, clk.Now),
		savings:  newSavingPrompts(draftTTL, clk.Now),
		captions: newCaptionWaits(captionWaitTTL, clk.Now),
		recent:   newRecentActions(recentActionTTL, clk.Now),
		locks:    newUserLocks(),
		toucher:  newToucher(s.Users, clk.Now, log),
		jobs:     newJobs(log),
		web:      web,
		username: botUsername,
		currency: "EUR",
		loc:      time.UTC,
		now:      clk.Now,
		log:      log,
		timeout:  5 * time.Second,
	}
	return &testEnv{app: a, api: api, svc: svc, clock: clk, logs: logs}
}

func testWhitelist() auth.Whitelist { return auth.NewWhitelist([]domain.UserID{alice, bob}) }

// Update builders.

func privateChat(user domain.UserID) models.Chat {
	return models.Chat{ID: int64(user), Type: models.ChatTypePrivate}
}

func userOf(id domain.UserID) *models.User {
	return &models.User{ID: int64(id), FirstName: "Дима"}
}

func textUpdate(user domain.UserID, text string) *models.Update {
	return &models.Update{ID: 1, Message: &models.Message{
		ID: 10, From: userOf(user), Chat: privateChat(user), Text: text,
	}}
}

func commandUpdate(user domain.UserID, cmd string) *models.Update {
	u := textUpdate(user, "/"+cmd)
	u.Message.Entities = []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len(cmd) + 1}}
	return u
}

func photoUpdate(user domain.UserID, fileID, album, caption string) *models.Update {
	return &models.Update{ID: 2, Message: &models.Message{
		ID: 11, From: userOf(user), Chat: privateChat(user), MediaGroupID: album, Caption: caption,
		Photo: []models.PhotoSize{
			{FileID: fileID + "-small", Width: 90, Height: 90},
			{FileID: fileID, Width: 1280, Height: 960},
			{FileID: fileID + "-mid", Width: 320, Height: 240},
		},
	}}
}

func callbackUpdate(user domain.UserID, data string, msg *models.Message) *models.Update {
	cq := &models.CallbackQuery{ID: "cb-1", From: *userOf(user), Data: data}
	if msg != nil {
		cq.Message = models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: msg}
	}
	return &models.Update{ID: 3, CallbackQuery: cq}
}

func cardMessage(user domain.UserID, id int) *models.Message {
	return &models.Message{ID: id, Chat: privateChat(user)}
}

func (e *testEnv) handle(u *models.Update) {
	e.app.handle(context.Background(), nil, u)
}
