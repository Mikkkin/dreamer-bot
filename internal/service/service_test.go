package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/media"
	"github.com/Mikkkin/dreamer-bot/internal/service"
	"github.com/Mikkkin/dreamer-bot/internal/storage/sqlite"
)

const (
	dima domain.UserID = 111
	anya domain.UserID = 222
	lena domain.UserID = 333
)

var start = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

// ---- fakes

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

type event struct {
	kind   string
	r      service.Recipients
	wish   domain.Wish
	recipe domain.Recipe
	ctx    context.Context
}

type recordingNotifier struct {
	mu     sync.Mutex
	events []event
	panics bool
}

func (n *recordingNotifier) record(e event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, e)
	if n.panics {
		panic("notifier exploded")
	}
}

func (n *recordingNotifier) WishCreated(ctx context.Context, r service.Recipients, w domain.Wish) {
	n.record(event{kind: "wish_created", r: r, wish: w, ctx: ctx})
}

func (n *recordingNotifier) WishFulfilled(ctx context.Context, r service.Recipients, w domain.Wish) {
	n.record(event{kind: "wish_fulfilled", r: r, wish: w, ctx: ctx})
}

func (n *recordingNotifier) RecipeCreated(ctx context.Context, r service.Recipients, rec domain.Recipe) {
	n.record(event{kind: "recipe_created", r: r, recipe: rec, ctx: ctx})
}

func (n *recordingNotifier) all() []event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.events)
}

// memMedia is an in-memory MediaStore that counts calls.
type memMedia struct {
	mu      sync.Mutex
	next    int
	files   map[string]bool
	saves   int
	deleted []string
}

func newMemMedia() *memMedia { return &memMedia{files: map[string]bool{}} }

func (m *memMedia) Save(_ context.Context, src io.Reader) (service.StoredImage, error) {
	if _, err := io.Copy(io.Discard, src); err != nil {
		return service.StoredImage{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	m.saves++
	key := fmt.Sprintf("%032x", m.next)
	m.files[key] = true
	return service.StoredImage{Key: key, Width: 160, Height: 120, Bytes: 4096}, nil
}

type readSeekNopCloser struct{ *bytes.Reader }

func (readSeekNopCloser) Close() error { return nil }

func (m *memMedia) Open(key string, _ service.ImageVariant) (service.ImageFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.files[key] {
		return service.ImageFile{}, domain.ErrNotFound
	}
	return service.ImageFile{Content: readSeekNopCloser{bytes.NewReader([]byte(key))}, ContentType: "image/jpeg", Size: int64(len(key))}, nil
}

func (m *memMedia) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
	m.deleted = append(m.deleted, key)
	return nil
}

// failingImageInsert simulates a database failure after the files of an
// upload have been written.
type failingImageInsert struct{ service.Repositories }

func (failingImageInsert) InsertImage(context.Context, service.ImageOwner, domain.Image, int) (domain.Image, error) {
	return domain.Image{}, errors.New("disk I/O error")
}

// ---- environment

type env struct {
	svc      *service.Services
	db       *sqlite.DB
	notifier *recordingNotifier
	clock    *fakeClock
}

type envOption func(*service.Deps)

func withMedia(m service.MediaStore) envOption { return func(d *service.Deps) { d.Media = m } }

func withRepos(wrap func(service.Repositories) service.Repositories) envOption {
	return func(d *service.Deps) { d.Repos = wrap(d.Repos) }
}

func newEnv(t *testing.T, opts ...envOption) *env {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	e := &env{db: db, notifier: &recordingNotifier{}, clock: &fakeClock{t: start}}
	deps := service.Deps{
		Repos:     db,
		Media:     newMemMedia(),
		Notifier:  e.notifier,
		Whitelist: []domain.UserID{dima, anya, lena},
		Now:       e.clock.Now,
	}
	for _, o := range opts {
		o(&deps)
	}
	if e.svc, err = service.New(deps); err != nil {
		t.Fatalf("service.New: %v", err)
	}
	return e
}

func (e *env) touch(t *testing.T, id domain.UserID, name string, hasChat bool) {
	t.Helper()
	if err := e.svc.Users.Touch(context.Background(), domain.User{ID: id, FirstName: name, HasChat: hasChat}); err != nil {
		t.Fatalf("Touch(%d): %v", id, err)
	}
}

func (e *env) createWish(t *testing.T, actor domain.UserID, d domain.WishDraft) domain.Wish {
	t.Helper()
	w, err := e.svc.Wishes.Create(context.Background(), actor, d)
	if err != nil {
		t.Fatalf("Create wish: %v", err)
	}
	return w
}

func ids(users []domain.User) []domain.UserID {
	out := make([]domain.UserID, 0, len(users))
	for _, u := range users {
		out = append(out, u.ID)
	}
	return out
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(1, 1, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ---- tests

func TestNewValidatesDeps(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "x.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cases := map[string]service.Deps{
		"no repos":        {Media: newMemMedia()},
		"no media":        {Repos: db},
		"bad whitelist":   {Repos: db, Media: newMemMedia(), Whitelist: []domain.UserID{0}},
		"negative member": {Repos: db, Media: newMemMedia(), Whitelist: []domain.UserID{-5}},
	}
	for name, d := range cases {
		if _, err := service.New(d); err == nil {
			t.Errorf("%s: New succeeded", name)
		}
	}
	if _, err := service.New(service.Deps{Repos: db, Media: newMemMedia()}); err != nil {
		t.Errorf("minimal deps rejected: %v", err)
	}
}

func TestWishCreatedNotifiesPartnersWithChat(t *testing.T) {
	e := newEnv(t)
	e.touch(t, dima, "Дима", true)
	e.touch(t, anya, "Аня", true)
	e.touch(t, lena, "Лена", false) // whitelisted but never opened the chat

	ctx, cancel := context.WithCancel(context.Background())
	w, err := e.svc.Wishes.Create(ctx, dima, domain.WishDraft{Title: "Поездка в Токио"})
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	events := e.notifier.all()
	if len(events) != 1 || events[0].kind != "wish_created" {
		t.Fatalf("events = %+v, want one wish_created", events)
	}
	ev := events[0]
	if got := ids(ev.r.To); !slices.Equal(got, []domain.UserID{anya}) {
		t.Errorf("recipients = %v, want [anya]", got)
	}
	if ev.r.Actor.ID != dima || ev.r.Actor.FirstName != "Дима" {
		t.Errorf("actor = %+v, want Дима's profile", ev.r.Actor)
	}
	if ev.wish.ID != w.ID || ev.wish.Title != w.Title {
		t.Errorf("notified wish = %+v, want %+v", ev.wish, w)
	}
	if ev.ctx.Err() != nil {
		t.Error("notifier context is cancelled together with the request; async delivery would be cut off")
	}

	// Lena acts: both Dima and Anya have a chat.
	e.createWish(t, lena, domain.WishDraft{Title: "Кофемашина"})
	events = e.notifier.all()
	if got := ids(events[1].r.To); !slices.Equal(got, []domain.UserID{dima, anya}) {
		t.Errorf("recipients = %v, want [dima anya]", got)
	}
}

func TestNotificationsSkippedWithoutRecipients(t *testing.T) {
	e := newEnv(t)
	e.touch(t, dima, "Дима", true)
	e.touch(t, anya, "Аня", false)

	e.createWish(t, dima, domain.WishDraft{Title: "Одному"})
	if _, err := e.svc.Recipes.Create(context.Background(), dima, domain.RecipeDraft{Title: "Омлет"}); err != nil {
		t.Fatal(err)
	}
	if events := e.notifier.all(); len(events) != 0 {
		t.Fatalf("notifier called without recipients: %+v", events)
	}
}

func TestNotificationActorFallsBackToID(t *testing.T) {
	e := newEnv(t)
	e.touch(t, anya, "Аня", true)

	e.createWish(t, dima, domain.WishDraft{Title: "Без профиля"})
	events := e.notifier.all()
	if len(events) != 1 || events[0].r.Actor != (domain.User{ID: dima}) {
		t.Fatalf("events = %+v, want actor {ID: dima}", events)
	}
}

func TestNotifierPanicDoesNotFailUseCase(t *testing.T) {
	e := newEnv(t)
	e.notifier.panics = true
	e.touch(t, dima, "Дима", true)
	e.touch(t, anya, "Аня", true)

	w := e.createWish(t, dima, domain.WishDraft{Title: "Надёжность"})
	if _, err := e.svc.Wishes.Get(context.Background(), w.ID); err != nil {
		t.Fatalf("wish was not stored: %v", err)
	}
}

func TestWishFulfilledOnlyOnTransition(t *testing.T) {
	e := newEnv(t)
	e.touch(t, dima, "Дима", true)
	e.touch(t, anya, "Аня", true)
	ctx := context.Background()
	w := e.createWish(t, anya, domain.WishDraft{Title: "Абонемент в бассейн"})

	steps := []struct {
		status        domain.Status
		wantFulfilled bool
	}{
		{domain.StatusProgress, false},
		{domain.StatusDone, true},
		{domain.StatusDone, false}, // no transition
		{domain.StatusWant, false},
		{domain.StatusDone, true},
	}
	for i, st := range steps {
		e.clock.Set(start.Add(time.Duration(i+1) * time.Hour))
		before := len(e.notifier.all())
		got, err := e.svc.Wishes.SetStatus(ctx, dima, w.ID, st.status)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if got.Status != st.status {
			t.Errorf("step %d: status = %s", i, got.Status)
		}
		if (got.FulfilledAt != nil) != (st.status == domain.StatusDone) {
			t.Errorf("step %d: FulfilledAt = %v for status %s", i, got.FulfilledAt, st.status)
		}
		events := e.notifier.all()[before:]
		fulfilled := len(events) == 1 && events[0].kind == "wish_fulfilled"
		if fulfilled != st.wantFulfilled || len(events) > 1 {
			t.Errorf("step %d (%s): events %+v, want fulfilled=%v", i, st.status, events, st.wantFulfilled)
		}
		if fulfilled {
			if ev := events[0]; ev.r.Actor.ID != dima || !slices.Equal(ids(ev.r.To), []domain.UserID{anya}) || ev.wish.ID != w.ID {
				t.Errorf("step %d: fulfilled event = %+v", i, ev)
			}
		}
	}

	if _, err := e.svc.Wishes.SetStatus(ctx, dima, w.ID, "maybe"); err == nil {
		t.Error("unknown status accepted")
	}
	if _, err := e.svc.Wishes.SetStatus(ctx, dima, 9999, domain.StatusDone); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("SetStatus(missing) = %v, want ErrNotFound", err)
	}
}

func TestRecipeCreatedNotifies(t *testing.T) {
	e := newEnv(t)
	e.touch(t, dima, "Дима", true)
	e.touch(t, anya, "Аня", true)

	r, err := e.svc.Recipes.Create(context.Background(), anya, domain.RecipeDraft{Title: "Сырники", Body: "Творог, яйцо, мука"})
	if err != nil {
		t.Fatal(err)
	}
	events := e.notifier.all()
	if len(events) != 1 || events[0].kind != "recipe_created" || events[0].recipe.ID != r.ID ||
		!slices.Equal(ids(events[0].r.To), []domain.UserID{dima}) || events[0].r.Actor.FirstName != "Аня" {
		t.Fatalf("events = %+v", events)
	}
}

func TestWishCategoryMustExist(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	missing := domain.CategoryID(9999)
	sentinel := domain.Uncategorized

	for _, id := range []*domain.CategoryID{&missing, &sentinel} {
		_, err := e.svc.Wishes.Create(ctx, dima, domain.WishDraft{Title: "X", CategoryID: id})
		if v, ok := domain.AsValidation(err); !ok || v.Field != "category_id" {
			t.Errorf("Create with category %d = %v, want a category_id validation error", *id, err)
		}
	}

	w := e.createWish(t, dima, domain.WishDraft{Title: "Y"})
	_, err := e.svc.Wishes.Update(ctx, dima, w.ID, domain.WishPatch{CategoryID: domain.Some(&missing)})
	if v, ok := domain.AsValidation(err); !ok || v.Field != "category_id" {
		t.Errorf("Update with a missing category = %v, want a category_id validation error", err)
	}

	cats, _ := e.svc.Categories.List(ctx)
	got, err := e.svc.Wishes.Update(ctx, dima, w.ID, domain.WishPatch{CategoryID: domain.Some(&cats[2].ID)})
	if err != nil || got.CategoryID == nil || *got.CategoryID != cats[2].ID {
		t.Fatalf("Update with a real category = %+v, %v", got, err)
	}
	got, err = e.svc.Wishes.Update(ctx, dima, w.ID, domain.WishPatch{CategoryID: domain.Some[*domain.CategoryID](nil)})
	if err != nil || got.CategoryID != nil {
		t.Fatalf("clearing the category = %+v, %v", got, err)
	}
}

func TestWishUpdateReturnsWhatIsStored(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.clock.Set(start.Add(123456789 * time.Nanosecond)) // sub-millisecond part must not leak
	w := e.createWish(t, dima, domain.WishDraft{Title: "Точность"})

	e.clock.Set(start.Add(time.Hour + 987654321*time.Nanosecond))
	updated, err := e.svc.Wishes.Update(ctx, anya, w.ID, domain.WishPatch{Title: domain.Some("  Точность  "), Hot: domain.Some(true)})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := e.svc.Wishes.Get(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.UpdatedAt != stored.UpdatedAt || updated.CreatedAt != stored.CreatedAt || w.CreatedAt != stored.CreatedAt {
		t.Errorf("returned times %v/%v differ from stored %v/%v", updated.CreatedAt, updated.UpdatedAt, stored.CreatedAt, stored.UpdatedAt)
	}
	if stored.AuthorID != dima || !stored.Hot || stored.Title != "Точность" {
		t.Errorf("stored = %+v", stored)
	}

	_, err = e.svc.Wishes.Update(ctx, dima, w.ID, domain.WishPatch{Title: domain.Some("")})
	if v, ok := domain.AsValidation(err); !ok || v.Field != "title" {
		t.Errorf("empty title = %v, want a title validation error", err)
	}
	if _, err := e.svc.Wishes.Update(ctx, dima, 9999, domain.WishPatch{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update(missing) = %v, want ErrNotFound", err)
	}
}

func TestWishListSearchAndFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tokyo := e.createWish(t, dima, domain.WishDraft{Title: "Поездка в Токио"})
	e.createWish(t, dima, domain.WishDraft{Title: "Велосипед"})

	got, err := e.svc.Wishes.List(ctx, domain.WishFilter{Query: "  токио "})
	if err != nil || len(got) != 1 || got[0].ID != tokyo.ID {
		t.Fatalf("List(токио) = %+v, %v", got, err)
	}
	bad := domain.Status("maybe")
	if _, err := e.svc.Wishes.List(ctx, domain.WishFilter{Status: &bad}); err == nil {
		t.Error("unknown status filter accepted")
	}
}

func TestAddImageRespectsLimit(t *testing.T) {
	m := newMemMedia()
	e := newEnv(t, withMedia(m))
	ctx := context.Background()
	w := e.createWish(t, dima, domain.WishDraft{Title: "Фотоальбом"})

	for i := range domain.MaxImagesPerWish {
		img, err := e.svc.Wishes.AddImage(ctx, dima, w.ID, bytes.NewReader([]byte("img")))
		if err != nil {
			t.Fatalf("image %d: %v", i, err)
		}
		if img.Position != i || img.Width != 160 || img.Height != 120 || img.Bytes != 4096 || img.ID == 0 {
			t.Errorf("image %d = %+v", i, img)
		}
	}
	_, err := e.svc.Wishes.AddImage(ctx, dima, w.ID, bytes.NewReader([]byte("img")))
	if !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("image over the limit = %v, want ErrLimitExceeded", err)
	}
	if m.saves != domain.MaxImagesPerWish {
		t.Errorf("media.Save called %d times; the rejected upload must not be processed", m.saves)
	}

	if _, err := e.svc.Wishes.AddImage(ctx, dima, 9999, bytes.NewReader(nil)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("AddImage(missing wish) = %v, want ErrNotFound", err)
	}

	r, err := e.svc.Recipes.Create(ctx, dima, domain.RecipeDraft{Title: "Скриншоты"})
	if err != nil {
		t.Fatal(err)
	}
	for range domain.MaxImagesPerRecipe {
		if _, err := e.svc.Recipes.AddImage(ctx, dima, r.ID, bytes.NewReader(nil)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.Recipes.AddImage(ctx, dima, r.ID, bytes.NewReader(nil)); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Errorf("recipe image over the limit = %v, want ErrLimitExceeded", err)
	}
}

func TestAddImageRemovesFilesWhenInsertFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "media")
	store, err := media.NewStore(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, withMedia(store), withRepos(func(r service.Repositories) service.Repositories {
		return failingImageInsert{r}
	}))
	w := e.createWish(t, dima, domain.WishDraft{Title: "Сломанный диск"})

	if _, err := e.svc.Wishes.AddImage(context.Background(), dima, w.ID, bytes.NewReader(pngBytes(t))); err == nil {
		t.Fatal("AddImage succeeded although the insert failed")
	}
	if n := countFiles(t, dir); n != 0 {
		t.Errorf("%d orphan files left in the media directory", n)
	}
}

func TestImageLifecycleWithRealStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "media")
	store, err := media.NewStore(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, withMedia(store))
	ctx := context.Background()
	w := e.createWish(t, dima, domain.WishDraft{Title: "С фото"})
	other := e.createWish(t, dima, domain.WishDraft{Title: "Другое"})

	first, err := e.svc.Wishes.AddImage(ctx, dima, w.ID, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.svc.Wishes.AddImage(ctx, anya, w.ID, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	if first.Width != 40 || first.Height != 30 {
		t.Errorf("image size = %dx%d, want 40x30", first.Width, first.Height)
	}
	if n := countFiles(t, dir); n != 4 {
		t.Fatalf("%d files after two uploads, want 4 (two variants each)", n)
	}

	f, err := e.svc.Images.Open(ctx, first.ID, service.VariantThumb)
	if err != nil {
		t.Fatalf("Images.Open: %v", err)
	}
	head := make([]byte, 2)
	_, _ = io.ReadFull(f.Content, head)
	_ = f.Content.Close()
	if f.ContentType != "image/jpeg" || !bytes.Equal(head, []byte{0xFF, 0xD8}) {
		t.Errorf("served %q starting with %x, want a JPEG", f.ContentType, head)
	}
	if _, err := e.svc.Images.Open(ctx, first.ID, "original"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown variant = %v, want ErrNotFound", err)
	}
	if _, err := e.svc.Images.Open(ctx, 9999, service.VariantFull); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown image = %v, want ErrNotFound", err)
	}

	// An image can only be removed through the wish that owns it.
	if err := e.svc.Wishes.RemoveImage(ctx, dima, other.ID, first.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("RemoveImage via another wish = %v, want ErrNotFound", err)
	}
	if err := e.svc.Wishes.RemoveImage(ctx, dima, w.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if n := countFiles(t, dir); n != 2 {
		t.Errorf("%d files after removing one image, want 2", n)
	}
	got, _ := e.svc.Wishes.Get(ctx, w.ID)
	if len(got.Images) != 1 || got.Images[0].ID != second.ID {
		t.Errorf("remaining images = %+v", got.Images)
	}

	if err := e.svc.Wishes.Delete(ctx, dima, w.ID); err != nil {
		t.Fatal(err)
	}
	if n := countFiles(t, dir); n != 0 {
		t.Errorf("%d files left after deleting the wish", n)
	}
	if _, err := e.svc.Images.Open(ctx, second.ID, service.VariantFull); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("image of a deleted wish = %v, want ErrNotFound", err)
	}
	if err := e.svc.Wishes.Delete(ctx, dima, w.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestRecipeDeleteRemovesFiles(t *testing.T) {
	m := newMemMedia()
	e := newEnv(t, withMedia(m))
	ctx := context.Background()
	r, err := e.svc.Recipes.Create(ctx, dima, domain.RecipeDraft{Title: "Плов"})
	if err != nil {
		t.Fatal(err)
	}
	img, err := e.svc.Recipes.AddImage(ctx, dima, r.ID, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := e.svc.Recipes.Get(ctx, r.ID)
	if len(stored.Images) != 1 || stored.Images[0].ID != img.ID {
		t.Fatalf("recipe images = %+v", stored.Images)
	}
	if err := e.svc.Recipes.Delete(ctx, anya, r.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.deleted, []string{stored.Images[0].Key}) {
		t.Errorf("deleted files = %v, want the recipe's image", m.deleted)
	}
}

func TestRecipesRandomAndSearch(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Recipes.Random(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Random(empty) = %v, want ErrNotFound", err)
	}
	r, err := e.svc.Recipes.Create(ctx, dima, domain.RecipeDraft{Title: "Паста", Body: "Сливочный СОУС"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Recipes.Random(ctx)
	if err != nil || got.ID != r.ID {
		t.Fatalf("Random = %+v, %v", got, err)
	}
	list, err := e.svc.Recipes.List(ctx, domain.RecipeFilter{Query: " соус "})
	if err != nil || len(list) != 1 {
		t.Fatalf("List(соус) = %+v, %v", list, err)
	}
	updated, err := e.svc.Recipes.Update(ctx, anya, r.ID, domain.RecipePatch{Link: domain.Some(new("example.com/pasta"))})
	if err != nil || updated.Link == nil || *updated.Link != "https://example.com/pasta" {
		t.Fatalf("Update = %+v, %v", updated, err)
	}
}

func TestCategories(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	books, err := e.svc.Categories.Create(ctx, dima, "  Книги ", "📚")
	if err != nil {
		t.Fatal(err)
	}
	if books.Name != "Книги" || books.Position != len(domain.DefaultCategories()) {
		t.Errorf("Create = %+v", books)
	}
	if _, err := e.svc.Categories.Create(ctx, anya, "КНИГИ", "📖"); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate name = %v, want ErrConflict", err)
	}
	if _, err := e.svc.Categories.Create(ctx, anya, "", "📖"); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := e.svc.Categories.Update(ctx, anya, books.ID, domain.CategoryPatch{Emoji: domain.Some("abc")}); err == nil {
		t.Error("letters accepted as emoji")
	}
	renamed, err := e.svc.Categories.Update(ctx, anya, books.ID, domain.CategoryPatch{Name: domain.Some("Чтение")})
	if err != nil || renamed.Name != "Чтение" || renamed.Emoji != books.Emoji {
		t.Fatalf("name-only Update must keep the emoji: %+v, %v", renamed, err)
	}
	renamed, err = e.svc.Categories.Update(ctx, anya, books.ID, domain.CategoryPatch{Emoji: domain.Some("📖")})
	if err != nil || renamed.Name != "Чтение" || renamed.Emoji != "📖" {
		t.Fatalf("emoji-only Update must keep the name: %+v, %v", renamed, err)
	}

	w := e.createWish(t, dima, domain.WishDraft{Title: "Дюна", CategoryID: &books.ID})
	if err := e.svc.Categories.Delete(ctx, dima, books.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Wishes.Get(ctx, w.ID)
	if err != nil || got.CategoryID != nil {
		t.Fatalf("wish after category delete = %+v, %v", got, err)
	}
	if _, err := e.svc.Categories.Get(ctx, books.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get(deleted) = %v, want ErrNotFound", err)
	}
}

func TestStats(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cats, _ := e.svc.Categories.List(ctx)
	shopping, travel := cats[0].ID, cats[1].ID
	money := func(minor int64, c domain.Currency) *domain.Money {
		m, err := domain.NewMoney(minor, c)
		if err != nil {
			t.Fatal(err)
		}
		return &m
	}
	setStatus := func(w domain.Wish, s domain.Status) {
		if _, err := e.svc.Wishes.SetStatus(ctx, dima, w.ID, s); err != nil {
			t.Fatal(err)
		}
	}

	e.createWish(t, dima, domain.WishDraft{Title: "a", CategoryID: &shopping, Price: money(10000, "EUR")})
	e.createWish(t, dima, domain.WishDraft{Title: "b", CategoryID: &shopping, Price: money(5050, "EUR")})
	e.createWish(t, dima, domain.WishDraft{Title: "c", CategoryID: &shopping, Price: money(3000, "USD")})
	setStatus(e.createWish(t, dima, domain.WishDraft{Title: "d", CategoryID: &travel}), domain.StatusProgress)
	thisYear := e.createWish(t, dima, domain.WishDraft{Title: "e", Price: money(1000, "RUB")})
	setStatus(thisYear, domain.StatusDone)
	lastYear := e.createWish(t, dima, domain.WishDraft{Title: "f"})
	e.clock.Set(time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC))
	setStatus(lastYear, domain.StatusDone)
	e.clock.Set(start)
	for _, title := range []string{"r1", "r2"} {
		if _, err := e.svc.Recipes.Create(ctx, dima, domain.RecipeDraft{Title: title}); err != nil {
			t.Fatal(err)
		}
	}

	s, err := e.svc.Stats.Compute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Recipes != 2 || s.FulfilledThisYear != 1 {
		t.Errorf("Recipes = %d, FulfilledThisYear = %d; want 2, 1", s.Recipes, s.FulfilledThisYear)
	}
	wantOverall := map[domain.Status]domain.StatusTotals{
		domain.StatusWant:     {Count: 3, Sums: []domain.Money{{Minor: 15050, Currency: "EUR"}, {Minor: 3000, Currency: "USD"}}},
		domain.StatusProgress: {Count: 1, Sums: []domain.Money{}},
		domain.StatusDone:     {Count: 2, Sums: []domain.Money{{Minor: 1000, Currency: "RUB"}}},
	}
	assertTotals(t, "overall", s.Overall, wantOverall)

	if len(s.Categories) != len(cats)+1 {
		t.Fatalf("%d category buckets, want %d categories plus uncategorized", len(s.Categories), len(cats))
	}
	for i, c := range cats {
		if s.Categories[i].Category == nil || s.Categories[i].Category.ID != c.ID {
			t.Fatalf("bucket %d = %+v, want category %d in display order", i, s.Categories[i].Category, c.ID)
		}
	}
	assertTotals(t, "shopping", s.Categories[0].ByStatus, map[domain.Status]domain.StatusTotals{
		domain.StatusWant:     {Count: 3, Sums: []domain.Money{{Minor: 15050, Currency: "EUR"}, {Minor: 3000, Currency: "USD"}}},
		domain.StatusProgress: {Count: 0, Sums: []domain.Money{}},
		domain.StatusDone:     {Count: 0, Sums: []domain.Money{}},
	})
	assertTotals(t, "travel", s.Categories[1].ByStatus, map[domain.Status]domain.StatusTotals{
		domain.StatusWant:     {Sums: []domain.Money{}},
		domain.StatusProgress: {Count: 1, Sums: []domain.Money{}},
		domain.StatusDone:     {Sums: []domain.Money{}},
	})
	empty := map[domain.Status]domain.StatusTotals{
		domain.StatusWant: {Sums: []domain.Money{}}, domain.StatusProgress: {Sums: []domain.Money{}}, domain.StatusDone: {Sums: []domain.Money{}},
	}
	assertTotals(t, "empty category", s.Categories[2].ByStatus, empty)
	last := s.Categories[len(s.Categories)-1]
	if last.Category != nil {
		t.Fatalf("last bucket = %+v, want the uncategorized one", last.Category)
	}
	assertTotals(t, "uncategorized", last.ByStatus, map[domain.Status]domain.StatusTotals{
		domain.StatusWant:     {Sums: []domain.Money{}},
		domain.StatusProgress: {Sums: []domain.Money{}},
		domain.StatusDone:     {Count: 2, Sums: []domain.Money{{Minor: 1000, Currency: "RUB"}}},
	})

	// Without uncategorized wishes there is no uncategorized bucket.
	for _, w := range []domain.Wish{thisYear, lastYear} {
		if err := e.svc.Wishes.Delete(ctx, dima, w.ID); err != nil {
			t.Fatal(err)
		}
	}
	s, err = e.svc.Stats.Compute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Categories) != len(cats) {
		t.Errorf("%d buckets, want %d (no uncategorized bucket)", len(s.Categories), len(cats))
	}
}

func assertTotals(t *testing.T, name string, got, want map[domain.Status]domain.StatusTotals) {
	t.Helper()
	if len(got) != len(domain.Statuses) {
		t.Errorf("%s: %d statuses, want all %d", name, len(got), len(domain.Statuses))
	}
	for _, st := range domain.Statuses {
		g, w := got[st], want[st]
		if g.Count != w.Count || !slices.Equal(g.Sums, w.Sums) || g.Sums == nil {
			t.Errorf("%s/%s = %+v, want %+v (sums non-nil)", name, st, g, w)
		}
	}
}

func TestUsersTouch(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	e.touch(t, dima, "Дима", true)
	u, err := e.svc.Users.Get(ctx, dima)
	if err != nil || !u.HasChat || !u.UpdatedAt.Equal(start) {
		t.Fatalf("after first touch = %+v, %v", u, err)
	}

	// The Mini App touches without a chat: HasChat stays, nothing is written.
	e.clock.Set(start.Add(30 * time.Minute))
	e.touch(t, dima, "Дима", false)
	if u, _ = e.svc.Users.Get(ctx, dima); !u.HasChat || !u.UpdatedAt.Equal(start) {
		t.Errorf("unchanged touch within an hour wrote %+v", u)
	}

	// A changed profile is written right away.
	e.clock.Set(start.Add(40 * time.Minute))
	e.touch(t, dima, "Дмитрий", false)
	if u, _ = e.svc.Users.Get(ctx, dima); u.FirstName != "Дмитрий" || !u.HasChat || !u.UpdatedAt.Equal(start.Add(40*time.Minute)) {
		t.Errorf("changed profile = %+v", u)
	}

	// An unchanged profile is refreshed after an hour.
	e.clock.Set(start.Add(2 * time.Hour))
	e.touch(t, dima, "Дмитрий", false)
	if u, _ = e.svc.Users.Get(ctx, dima); !u.UpdatedAt.Equal(start.Add(2*time.Hour)) || !u.HasChat {
		t.Errorf("stale profile was not refreshed: %+v", u)
	}

	// Strangers are never stored.
	if err := e.svc.Users.Touch(ctx, domain.User{ID: 999, FirstName: "Чужой", HasChat: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Users.Get(ctx, 999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-whitelisted user stored: %v", err)
	}
	if err := e.svc.Users.Touch(ctx, domain.User{ID: 0}); err == nil {
		t.Error("zero user ID accepted")
	}
}

func TestUsersListAndPartners(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.touch(t, lena, "Лена", true)
	e.touch(t, dima, "Дима", true)
	e.touch(t, anya, "Аня", false)

	users, err := e.svc.Users.List(ctx)
	if err != nil || !slices.Equal(ids(users), []domain.UserID{dima, anya, lena}) {
		t.Errorf("List = %v, %v; want whitelist order", ids(users), err)
	}
	partners, err := e.svc.Users.Partners(ctx, dima)
	if err != nil || !slices.Equal(ids(partners), []domain.UserID{lena}) {
		t.Errorf("Partners(dima) = %v, %v; want [lena]", ids(partners), err)
	}
	partners, err = e.svc.Users.Partners(ctx, anya)
	if err != nil || !slices.Equal(ids(partners), []domain.UserID{dima, lena}) {
		t.Errorf("Partners(anya) = %v, %v; want [dima lena]", ids(partners), err)
	}
}
