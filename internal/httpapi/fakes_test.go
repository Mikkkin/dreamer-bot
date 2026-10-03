package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// fakeDB is a small in-memory implementation of every use case, just
// faithful enough to exercise the HTTP adapter.
type fakeDB struct {
	mu         sync.Mutex
	now        time.Time
	nextID     int64
	maxImage   int64
	whitelist  []domain.UserID
	wishes     map[domain.WishID]domain.Wish
	recipes    map[domain.RecipeID]domain.Recipe
	categories map[domain.CategoryID]domain.Category
	blobs      map[domain.ImageID][]byte
	users      map[domain.UserID]domain.User
	touches    []domain.User
	removed    []domain.ImageID
	stats      domain.Stats

	savings  map[domain.WishID][]domain.Saving // newest first
	cooks    map[domain.RecipeID][]domain.Cook // newest first
	tags     map[domain.RecipeTagID]domain.RecipeTag
	shopping map[domain.ShoppingItemID]domain.ShoppingItem
	// calls records what the adapter passed to the new use cases.
	calls fakeCalls
	// importFn answers Recipes.Import (import_test.go).
	importFn func(ctx context.Context, actor domain.UserID, in service.ImportInput) (domain.Recipe, service.ImportReport, error)
}

type fakeCalls struct {
	cookRating    *service.RatingInput
	cooked        bool
	positions     []int
	fromRecipe    bool
	shoppingPatch *domain.ShoppingPatch
	savingAmount  *domain.Money
}

func newFakeDB(now time.Time, maxImage int64, whitelist ...domain.UserID) *fakeDB {
	return &fakeDB{
		now:        now,
		maxImage:   maxImage,
		whitelist:  whitelist,
		wishes:     map[domain.WishID]domain.Wish{},
		recipes:    map[domain.RecipeID]domain.Recipe{},
		categories: map[domain.CategoryID]domain.Category{},
		blobs:      map[domain.ImageID][]byte{},
		users:      map[domain.UserID]domain.User{},
		savings:    map[domain.WishID][]domain.Saving{},
		cooks:      map[domain.RecipeID][]domain.Cook{},
		tags:       map[domain.RecipeTagID]domain.RecipeTag{},
		shopping:   map[domain.ShoppingItemID]domain.ShoppingItem{},
	}
}

func (db *fakeDB) services() *service.Services {
	return &service.Services{
		Wishes:     fakeWishes{db},
		Recipes:    fakeRecipes{db},
		RecipeTags: fakeRecipeTags{db},
		Shopping:   fakeShopping{db},
		Categories: fakeCategories{db},
		Images:     fakeImages{db},
		Stats:      fakeStats{db},
		Users:      fakeUsers{db},
	}
}

func (db *fakeDB) id() int64 {
	db.nextID++
	return db.nextID
}

// storeImage mimics the media store: bounded read, format and size checks.
func (db *fakeDB) storeImage(src io.Reader) (domain.Image, error) {
	data, err := io.ReadAll(io.LimitReader(src, db.maxImage+1))
	if err != nil {
		return domain.Image{}, fmt.Errorf("read image: %w", err)
	}
	if int64(len(data)) > db.maxImage {
		return domain.Image{}, domain.ErrImageTooLarge
	}
	if !bytes.HasPrefix(data, []byte{0xff, 0xd8}) {
		return domain.Image{}, domain.ErrImageUnsupported
	}
	img := domain.Image{ID: domain.ImageID(db.id()), Key: "k", Width: 640, Height: 800, Bytes: int64(len(data)), CreatedAt: db.now}
	db.blobs[img.ID] = data
	return img, nil
}

func removeImage(imgs []domain.Image, id domain.ImageID) ([]domain.Image, bool) {
	i := slices.IndexFunc(imgs, func(img domain.Image) bool { return img.ID == id })
	if i < 0 {
		return imgs, false
	}
	return slices.Delete(imgs, i, i+1), true
}

type fakeWishes struct{ db *fakeDB }

func (f fakeWishes) Create(_ context.Context, actor domain.UserID, d domain.WishDraft) (domain.Wish, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	w, err := domain.NewWish(d, actor, f.db.now)
	if err != nil {
		return domain.Wish{}, err
	}
	if w.CategoryID != nil {
		if _, ok := f.db.categories[*w.CategoryID]; !ok {
			return domain.Wish{}, &domain.ValidationError{Field: "category_id", Message: "такой категории нет"}
		}
	}
	w.ID = domain.WishID(f.db.id())
	f.db.wishes[w.ID] = w
	return w, nil
}

func (f fakeWishes) Get(_ context.Context, id domain.WishID) (domain.Wish, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	w, ok := f.db.wishes[id]
	if !ok {
		return domain.Wish{}, domain.ErrNotFound
	}
	return w, nil
}

func (f fakeWishes) List(_ context.Context, filter domain.WishFilter) ([]domain.Wish, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	var out []domain.Wish
	for _, w := range f.db.wishes {
		if filter.Status != nil && w.Status != *filter.Status {
			continue
		}
		if filter.CategoryID != nil {
			if *filter.CategoryID == domain.Uncategorized && w.CategoryID != nil {
				continue
			}
			if *filter.CategoryID != domain.Uncategorized && (w.CategoryID == nil || *w.CategoryID != *filter.CategoryID) {
				continue
			}
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(w.Title), strings.ToLower(filter.Query)) {
			continue
		}
		out = append(out, w)
	}
	slices.SortFunc(out, func(a, b domain.Wish) int { return int(b.ID - a.ID) })
	return out, nil
}

func (f fakeWishes) Update(_ context.Context, _ domain.UserID, id domain.WishID, p domain.WishPatch) (domain.Wish, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	w, ok := f.db.wishes[id]
	if !ok {
		return domain.Wish{}, domain.ErrNotFound
	}
	if err := w.Apply(p, f.db.now); err != nil {
		return domain.Wish{}, err
	}
	f.db.wishes[id] = w
	return w, nil
}

func (f fakeWishes) SetStatus(_ context.Context, _ domain.UserID, id domain.WishID, s domain.Status) (domain.Wish, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	w, ok := f.db.wishes[id]
	if !ok {
		return domain.Wish{}, domain.ErrNotFound
	}
	if _, err := w.SetStatus(s, f.db.now); err != nil {
		return domain.Wish{}, err
	}
	f.db.wishes[id] = w
	return w, nil
}

func (f fakeWishes) Delete(_ context.Context, _ domain.UserID, id domain.WishID) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if _, ok := f.db.wishes[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.db.wishes, id)
	return nil
}

func (f fakeWishes) AddImage(_ context.Context, _ domain.UserID, id domain.WishID, src io.Reader) (domain.Image, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	w, ok := f.db.wishes[id]
	if !ok {
		return domain.Image{}, domain.ErrNotFound
	}
	if !w.CanAddImage() {
		return domain.Image{}, domain.ErrLimitExceeded
	}
	img, err := f.db.storeImage(src)
	if err != nil {
		return domain.Image{}, fmt.Errorf("store image: %w", err)
	}
	w.Images = append(w.Images, img)
	f.db.wishes[id] = w
	return img, nil
}

func (f fakeWishes) RemoveImage(_ context.Context, _ domain.UserID, id domain.WishID, img domain.ImageID) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	w, ok := f.db.wishes[id]
	if !ok {
		return domain.ErrNotFound
	}
	if w.Images, ok = removeImage(w.Images, img); !ok {
		return domain.ErrNotFound
	}
	f.db.wishes[id] = w
	f.db.removed = append(f.db.removed, img)
	return nil
}

type fakeRecipes struct{ db *fakeDB }

func (f fakeRecipes) Create(_ context.Context, actor domain.UserID, d domain.RecipeDraft) (domain.Recipe, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	r, err := domain.NewRecipe(d, actor, f.db.now)
	if err != nil {
		return domain.Recipe{}, err
	}
	if err := f.db.checkTags(r); err != nil {
		return domain.Recipe{}, err
	}
	r.ID = domain.RecipeID(f.db.id())
	f.db.recipes[r.ID] = r
	return r, nil
}

func (f fakeRecipes) Get(_ context.Context, id domain.RecipeID) (domain.Recipe, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	r, ok := f.db.recipes[id]
	if !ok {
		return domain.Recipe{}, domain.ErrNotFound
	}
	return r, nil
}

func (f fakeRecipes) List(_ context.Context, filter domain.RecipeFilter) ([]domain.Recipe, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	var out []domain.Recipe
	for _, r := range f.db.recipes {
		switch {
		case filter.Query != "" && !strings.Contains(r.Title+" "+r.Body, filter.Query):
		case filter.CuisineID != 0 && (r.CuisineID == nil || *r.CuisineID != filter.CuisineID):
		case filter.CourseID != 0 && !slices.Contains(r.CourseIDs, filter.CourseID):
		default:
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b domain.Recipe) int { return int(b.ID - a.ID) })
	return out, nil
}

func (f fakeRecipes) Random(ctx context.Context) (domain.Recipe, error) {
	all, _ := f.List(ctx, domain.RecipeFilter{})
	if len(all) == 0 {
		return domain.Recipe{}, domain.ErrNotFound
	}
	return all[0], nil
}

func (f fakeRecipes) Update(_ context.Context, _ domain.UserID, id domain.RecipeID, p domain.RecipePatch) (domain.Recipe, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	r, ok := f.db.recipes[id]
	if !ok {
		return domain.Recipe{}, domain.ErrNotFound
	}
	if err := r.Apply(p, f.db.now); err != nil {
		return domain.Recipe{}, err
	}
	if err := f.db.checkTags(r); err != nil {
		return domain.Recipe{}, err
	}
	f.db.recipes[id] = r
	return r, nil
}

func (f fakeRecipes) Delete(_ context.Context, _ domain.UserID, id domain.RecipeID) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if _, ok := f.db.recipes[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.db.recipes, id)
	return nil
}

func (f fakeRecipes) AddImage(_ context.Context, _ domain.UserID, id domain.RecipeID, src io.Reader) (domain.Image, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	r, ok := f.db.recipes[id]
	if !ok {
		return domain.Image{}, domain.ErrNotFound
	}
	if !r.CanAddImage() {
		return domain.Image{}, domain.ErrLimitExceeded
	}
	img, err := f.db.storeImage(src)
	if err != nil {
		return domain.Image{}, fmt.Errorf("store image: %w", err)
	}
	r.Images = append(r.Images, img)
	f.db.recipes[id] = r
	return img, nil
}

func (f fakeRecipes) RemoveImage(_ context.Context, _ domain.UserID, id domain.RecipeID, img domain.ImageID) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	r, ok := f.db.recipes[id]
	if !ok {
		return domain.ErrNotFound
	}
	if r.Images, ok = removeImage(r.Images, img); !ok {
		return domain.ErrNotFound
	}
	f.db.recipes[id] = r
	f.db.removed = append(f.db.removed, img)
	return nil
}

type fakeCategories struct{ db *fakeDB }

func (f fakeCategories) List(context.Context) ([]domain.Category, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	out := make([]domain.Category, 0, len(f.db.categories))
	for _, c := range f.db.categories {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b domain.Category) int { return a.Position - b.Position })
	return out, nil
}

func (f fakeCategories) Get(_ context.Context, id domain.CategoryID) (domain.Category, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	c, ok := f.db.categories[id]
	if !ok {
		return domain.Category{}, domain.ErrNotFound
	}
	return c, nil
}

func (f fakeCategories) Create(_ context.Context, _ domain.UserID, name, emoji string) (domain.Category, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	c, err := domain.NewCategory(name, emoji, f.db.now)
	if err != nil {
		return domain.Category{}, err
	}
	for _, other := range f.db.categories {
		if strings.EqualFold(other.Name, c.Name) {
			return domain.Category{}, domain.ErrConflict
		}
	}
	c.ID = domain.CategoryID(f.db.id())
	c.Position = len(f.db.categories)
	f.db.categories[c.ID] = c
	return c, nil
}

func (f fakeCategories) Update(_ context.Context, _ domain.UserID, id domain.CategoryID, p domain.CategoryPatch) (domain.Category, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	c, ok := f.db.categories[id]
	if !ok {
		return domain.Category{}, domain.ErrNotFound
	}
	if err := c.Apply(p); err != nil {
		return domain.Category{}, err
	}
	f.db.categories[id] = c
	return c, nil
}

func (f fakeCategories) Delete(_ context.Context, _ domain.UserID, id domain.CategoryID) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if _, ok := f.db.categories[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.db.categories, id)
	return nil
}

type fakeImages struct{ db *fakeDB }

func (f fakeImages) Open(_ context.Context, id domain.ImageID, _ service.ImageVariant) (service.ImageFile, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	data, ok := f.db.blobs[id]
	if !ok {
		return service.ImageFile{}, domain.ErrNotFound
	}
	return service.ImageFile{
		Content:     readSeekNopCloser{bytes.NewReader(data)},
		ContentType: "image/jpeg",
		ModTime:     f.db.now,
		Size:        int64(len(data)),
	}, nil
}

type readSeekNopCloser struct{ io.ReadSeeker }

func (readSeekNopCloser) Close() error { return nil }

type fakeStats struct{ db *fakeDB }

func (f fakeStats) Compute(context.Context) (domain.Stats, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	return f.db.stats, nil
}

type fakeUsers struct{ db *fakeDB }

func (f fakeUsers) Touch(_ context.Context, u domain.User) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	f.db.touches = append(f.db.touches, u)
	if stored, ok := f.db.users[u.ID]; ok {
		u.HasChat = u.HasChat || stored.HasChat
	}
	f.db.users[u.ID] = u
	return nil
}

func (f fakeUsers) Get(_ context.Context, id domain.UserID) (domain.User, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	u, ok := f.db.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (f fakeUsers) List(context.Context) ([]domain.User, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	var out []domain.User
	for _, id := range f.db.whitelist {
		if u, ok := f.db.users[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}

func (f fakeUsers) Partners(ctx context.Context, of domain.UserID) ([]domain.User, error) {
	all, _ := f.List(ctx)
	return slices.DeleteFunc(all, func(u domain.User) bool { return u.ID == of || !u.HasChat }), nil
}

// panickingStats simulates a bug deep inside a use case.
type panickingStats struct{}

func (panickingStats) Compute(context.Context) (domain.Stats, error) {
	panic("stats exploded: internal-detail-4711")
}

// failingStats simulates an infrastructure failure.
type failingStats struct{}

func (failingStats) Compute(context.Context) (domain.Stats, error) {
	return domain.Stats{}, fmt.Errorf("query stats: %w", errDiskOnFire)
}

var errDiskOnFire = fmt.Errorf("sqlite: disk I/O error at /data/dreamer.db")
