package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

var testNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func openTestDB(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "dreamer.db")
	db, err := Open(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func ptr[T any](v T) *T { return &v }

func mustWish(t *testing.T, db *DB, d domain.WishDraft, at time.Time) domain.Wish {
	t.Helper()
	w, err := domain.NewWish(d, 111, at)
	if err != nil {
		t.Fatalf("NewWish: %v", err)
	}
	w, err = db.InsertWish(context.Background(), w)
	if err != nil {
		t.Fatalf("InsertWish: %v", err)
	}
	return w
}

func mustRecipe(t *testing.T, db *DB, d domain.RecipeDraft, at time.Time) domain.Recipe {
	t.Helper()
	r, err := domain.NewRecipe(d, 222, at)
	if err != nil {
		t.Fatalf("NewRecipe: %v", err)
	}
	r, err = db.InsertRecipe(context.Background(), r)
	if err != nil {
		t.Fatalf("InsertRecipe: %v", err)
	}
	return r
}

func mustImage(t *testing.T, db *DB, owner service.ImageOwner, key string) domain.Image {
	t.Helper()
	img, err := db.InsertImage(context.Background(), owner,
		domain.Image{Key: key, Width: 10, Height: 20, Bytes: 300, CreatedAt: testNow}, 10)
	if err != nil {
		t.Fatalf("InsertImage: %v", err)
	}
	return img
}

func testKey(n int) string { return fmt.Sprintf("%032x", n) }

func TestOpenAppliesPragmasAndPrivatePermissions(t *testing.T) {
	db, path := openTestDB(t)
	ctx := context.Background()

	var fk int
	if err := db.sql.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d (%v), want 1", fk, err)
	}
	var mode string
	if err := db.sql.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q (%v), want wal", mode, err)
	}
	var busy int
	if err := db.sql.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy); err != nil || busy != 5000 {
		t.Errorf("busy_timeout = %d (%v), want 5000", busy, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("database file mode = %o, want 600", perm)
	}
	if err := db.Ping(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestOpenHandlesPathsThatLookLikeURISyntax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odd dir?#%", "db.sqlite")
	db, err := Open(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created at the literal path: %v", err)
	}
}

func TestMigrationsAreIdempotentAndSeedOnce(t *testing.T) {
	db, path := openTestDB(t)
	ctx := context.Background()

	cats, err := db.ListCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defaults := domain.DefaultCategories()
	if len(cats) != len(defaults) {
		t.Fatalf("seeded %d categories, want %d", len(cats), len(defaults))
	}
	for i, c := range cats {
		if c.Name != defaults[i].Name || c.Emoji != defaults[i].Emoji || c.Position != i {
			t.Errorf("category %d = %+v, want %+v", i, c, defaults[i])
		}
	}

	// A deleted default must not come back on restart.
	if err := db.DeleteCategory(ctx, cats[0].ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if db, err = Open(ctx, path, nil); err != nil {
			t.Fatalf("reopen: %v", err)
		}
	}
	defer db.Close()

	cats, err = db.ListCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != len(defaults)-1 {
		t.Errorf("after reopen: %d categories, want %d", len(cats), len(defaults)-1)
	}
	var versions int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Errorf("schema_migrations has %d rows, want 1", versions)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	db, path := openTestDB(t)
	if _, err := db.sql.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (999, 0)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := Open(context.Background(), path, nil); err == nil {
		t.Fatal("Open accepted a database from a newer version")
	}
}

func TestWishRoundTrip(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	cats, _ := db.ListCategories(ctx)

	price, _ := domain.NewMoney(120050, "EUR")
	created := mustWish(t, db, domain.WishDraft{
		Title:      "Поездка в Токио",
		Note:       "Весной,\nна сакуру",
		CategoryID: &cats[1].ID,
		Link:       ptr("https://example.com/tour"),
		Price:      &price,
		Hot:        true,
	}, testNow)
	bare := mustWish(t, db, domain.WishDraft{Title: "Просто так"}, testNow)

	got, err := db.GetWish(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != created.Title || got.Note != created.Note || !got.Hot || got.AuthorID != 111 ||
		got.Status != domain.StatusWant || *got.CategoryID != cats[1].ID || *got.Link != *created.Link ||
		*got.Price != price || !got.CreatedAt.Equal(testNow) || !got.UpdatedAt.Equal(testNow) || got.FulfilledAt != nil {
		t.Errorf("GetWish = %+v, want %+v", got, created)
	}

	got, err = db.GetWish(ctx, bare.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CategoryID != nil || got.Link != nil || got.Price != nil || got.Images != nil {
		t.Errorf("optional fields should be nil: %+v", got)
	}

	if _, err := db.GetWish(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetWish(missing) = %v, want ErrNotFound", err)
	}
}

func TestModifyWish(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	w := mustWish(t, db, domain.WishDraft{Title: "Велосипед"}, testNow)

	later := testNow.Add(time.Hour)
	price, _ := domain.NewMoney(5000, "USD")
	got, err := db.ModifyWish(ctx, w.ID, func(w *domain.Wish) error {
		if err := w.Apply(domain.WishPatch{Title: domain.Some("Горный велосипед"), Price: domain.Some(&price)}, later); err != nil {
			return err
		}
		_, err := w.SetStatus(domain.StatusDone, later)
		return err
	})
	if err != nil {
		t.Fatalf("ModifyWish: %v", err)
	}
	stored, _ := db.GetWish(ctx, w.ID)
	for _, x := range []domain.Wish{got, stored} {
		if x.Title != "Горный велосипед" || *x.Price != price || x.Status != domain.StatusDone ||
			x.FulfilledAt == nil || !x.FulfilledAt.Equal(later) || !x.UpdatedAt.Equal(later) {
			t.Errorf("modified wish = %+v", x)
		}
	}

	// An error from fn rolls back.
	boom := errors.New("boom")
	if _, err := db.ModifyWish(ctx, w.ID, func(w *domain.Wish) error {
		w.Title = "changed"
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("ModifyWish error = %v, want boom", err)
	}
	if stored, _ := db.GetWish(ctx, w.ID); stored.Title != "Горный велосипед" {
		t.Errorf("failed modification leaked: %q", stored.Title)
	}

	if _, err := db.ModifyWish(ctx, 9999, func(*domain.Wish) error { return nil }); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ModifyWish(missing) = %v, want ErrNotFound", err)
	}
}

func TestListWishesFilters(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	cats, _ := db.ListCategories(ctx)
	travel := cats[1].ID

	tokyo := mustWish(t, db, domain.WishDraft{Title: "Поездка в Токио", CategoryID: &travel}, testNow)
	tree := mustWish(t, db, domain.WishDraft{Title: "Ёлка до потолка"}, testNow.Add(time.Minute))
	lego := mustWish(t, db, domain.WishDraft{Title: "LEGO Titanic", CategoryID: &cats[0].ID}, testNow.Add(2*time.Minute))
	if _, err := db.ModifyWish(ctx, lego.ID, func(w *domain.Wish) error {
		_, err := w.SetStatus(domain.StatusProgress, testNow)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	ids := func(ws []domain.Wish) []domain.WishID {
		out := make([]domain.WishID, 0, len(ws))
		for _, w := range ws {
			out = append(out, w.ID)
		}
		return out
	}
	uncategorized := domain.Uncategorized
	progress := domain.StatusProgress
	cases := []struct {
		name string
		f    domain.WishFilter
		want []domain.WishID
	}{
		{"all newest first", domain.WishFilter{}, []domain.WishID{lego.ID, tree.ID, tokyo.ID}},
		{"status", domain.WishFilter{Status: &progress}, []domain.WishID{lego.ID}},
		{"category", domain.WishFilter{CategoryID: &travel}, []domain.WishID{tokyo.ID}},
		{"uncategorized", domain.WishFilter{CategoryID: &uncategorized}, []domain.WishID{tree.ID}},
		{"cyrillic lower case", domain.WishFilter{Query: "токио"}, []domain.WishID{tokyo.ID}},
		{"cyrillic upper case", domain.WishFilter{Query: "ТОКИО"}, []domain.WishID{tokyo.ID}},
		{"ё matches е", domain.WishFilter{Query: "елка"}, []domain.WishID{tree.ID}},
		{"е matches ё", domain.WishFilter{Query: "ЁЛКА"}, []domain.WishID{tree.ID}},
		{"latin", domain.WishFilter{Query: "titan"}, []domain.WishID{lego.ID}},
		{"like wildcards are literal", domain.WishFilter{Query: "%"}, []domain.WishID{}},
		{"combined", domain.WishFilter{Query: "о", CategoryID: &travel}, []domain.WishID{tokyo.ID}},
		{"no match", domain.WishFilter{Query: "париж"}, []domain.WishID{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := db.ListWishes(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids(got), tc.want) {
				t.Errorf("ListWishes = %v, want %v", ids(got), tc.want)
			}
		})
	}
}

func TestListWishesLoadsImagesInOrder(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	a := mustWish(t, db, domain.WishDraft{Title: "A"}, testNow)
	b := mustWish(t, db, domain.WishDraft{Title: "B"}, testNow.Add(time.Second))
	mustWish(t, db, domain.WishDraft{Title: "C"}, testNow.Add(2*time.Second))
	a1 := mustImage(t, db, service.WishImages(a.ID), testKey(1))
	b1 := mustImage(t, db, service.WishImages(b.ID), testKey(2))
	a2 := mustImage(t, db, service.WishImages(a.ID), testKey(3))
	if a1.Position != 0 || a2.Position != 1 || b1.Position != 0 {
		t.Errorf("positions = %d, %d, %d; want 0, 1, 0", a1.Position, a2.Position, b1.Position)
	}

	list, err := db.ListWishes(ctx, domain.WishFilter{})
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string][]domain.Image{}
	for _, w := range list {
		byTitle[w.Title] = w.Images
	}
	if len(byTitle["A"]) != 2 || byTitle["A"][0].ID != a1.ID || byTitle["A"][1].ID != a2.ID {
		t.Errorf("images of A = %+v", byTitle["A"])
	}
	if len(byTitle["B"]) != 1 || byTitle["B"][0].Key != testKey(2) {
		t.Errorf("images of B = %+v", byTitle["B"])
	}
	if len(byTitle["C"]) != 0 {
		t.Errorf("images of C = %+v", byTitle["C"])
	}
}

func TestDeleteWishCascadesImages(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	w := mustWish(t, db, domain.WishDraft{Title: "С фото"}, testNow)
	other := mustWish(t, db, domain.WishDraft{Title: "Другое"}, testNow)
	mustImage(t, db, service.WishImages(w.ID), testKey(1))
	mustImage(t, db, service.WishImages(w.ID), testKey(2))
	kept := mustImage(t, db, service.WishImages(other.ID), testKey(3))

	keys, err := db.DeleteWish(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{testKey(1), testKey(2)}) {
		t.Errorf("DeleteWish keys = %v", keys)
	}
	var left int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM images`).Scan(&left); err != nil || left != 1 {
		t.Errorf("images left = %d (%v), want 1", left, err)
	}
	if _, err := db.GetImage(ctx, kept.ID); err != nil {
		t.Errorf("image of another wish was removed: %v", err)
	}
	if _, err := db.DeleteWish(ctx, w.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second DeleteWish = %v, want ErrNotFound", err)
	}
}

// IDs must never be reused: stale bot buttons, deep links and signed media
// URLs carry bare IDs, so a reused ID would point at a different entity.
func TestIDsAreNeverReusedAfterDeletingTheNewestRow(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()

	w1 := mustWish(t, db, domain.WishDraft{Title: "Первое"}, testNow)
	img1 := mustImage(t, db, service.WishImages(w1.ID), testKey(1))
	if _, err := db.DeleteImage(ctx, service.WishImages(w1.ID), img1.ID); err != nil {
		t.Fatal(err)
	}
	if img2 := mustImage(t, db, service.WishImages(w1.ID), testKey(2)); img2.ID == img1.ID {
		t.Errorf("image id %d was reused", img1.ID)
	}
	if _, err := db.DeleteWish(ctx, w1.ID); err != nil {
		t.Fatal(err)
	}
	if w2 := mustWish(t, db, domain.WishDraft{Title: "Второе"}, testNow); w2.ID == w1.ID {
		t.Errorf("wish id %d was reused", w1.ID)
	}

	r1 := mustRecipe(t, db, domain.RecipeDraft{Title: "Суп"}, testNow)
	if _, err := db.DeleteRecipe(ctx, r1.ID); err != nil {
		t.Fatal(err)
	}
	if r2 := mustRecipe(t, db, domain.RecipeDraft{Title: "Салат"}, testNow); r2.ID == r1.ID {
		t.Errorf("recipe id %d was reused", r1.ID)
	}

	c1, err := db.InsertCategory(ctx, domain.Category{Name: "Временная", Emoji: "🧪", CreatedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteCategory(ctx, c1.ID); err != nil {
		t.Fatal(err)
	}
	c2, err := db.InsertCategory(ctx, domain.Category{Name: "Новая", Emoji: "🧪", CreatedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if c2.ID == c1.ID {
		t.Errorf("category id %d was reused", c1.ID)
	}
}

func TestImages(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	w := mustWish(t, db, domain.WishDraft{Title: "Wish"}, testNow)
	r := mustRecipe(t, db, domain.RecipeDraft{Title: "Recipe"}, testNow)

	t.Run("limit is enforced atomically", func(t *testing.T) {
		for i := range 2 {
			if _, err := db.InsertImage(ctx, service.RecipeImages(r.ID),
				domain.Image{Key: testKey(100 + i), Width: 1, Height: 1, CreatedAt: testNow}, 2); err != nil {
				t.Fatal(err)
			}
		}
		_, err := db.InsertImage(ctx, service.RecipeImages(r.ID),
			domain.Image{Key: testKey(102), Width: 1, Height: 1, CreatedAt: testNow}, 2)
		if !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("InsertImage over the limit = %v, want ErrLimitExceeded", err)
		}
	})

	t.Run("missing owner", func(t *testing.T) {
		_, err := db.InsertImage(ctx, service.WishImages(9999), domain.Image{Key: testKey(200), Width: 1, Height: 1}, 10)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("InsertImage(missing wish) = %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid owner", func(t *testing.T) {
		for _, o := range []service.ImageOwner{{}, {Wish: w.ID, Recipe: r.ID}} {
			if _, err := db.InsertImage(ctx, o, domain.Image{Key: testKey(201), Width: 1, Height: 1}, 10); err == nil {
				t.Errorf("InsertImage(%+v) succeeded", o)
			}
		}
	})

	t.Run("duplicate key", func(t *testing.T) {
		mustImage(t, db, service.WishImages(w.ID), testKey(300))
		_, err := db.InsertImage(ctx, service.WishImages(w.ID), domain.Image{Key: testKey(300), Width: 1, Height: 1}, 10)
		if !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("duplicate key = %v, want ErrConflict", err)
		}
	})

	t.Run("delete only through the owner", func(t *testing.T) {
		img := mustImage(t, db, service.WishImages(w.ID), testKey(400))
		if _, err := db.DeleteImage(ctx, service.RecipeImages(r.ID), img.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("DeleteImage via another owner = %v, want ErrNotFound", err)
		}
		other := mustWish(t, db, domain.WishDraft{Title: "Other"}, testNow)
		if _, err := db.DeleteImage(ctx, service.WishImages(other.ID), img.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("DeleteImage via another wish = %v, want ErrNotFound", err)
		}
		got, err := db.DeleteImage(ctx, service.WishImages(w.ID), img.ID)
		if err != nil || got.Key != testKey(400) {
			t.Fatalf("DeleteImage = %+v, %v", got, err)
		}
		if _, err := db.GetImage(ctx, img.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("GetImage after delete = %v, want ErrNotFound", err)
		}
	})

	t.Run("schema allows exactly one owner", func(t *testing.T) {
		_, err := db.sql.ExecContext(ctx, `INSERT INTO images (file_key, wish_id, recipe_id, width, height, bytes, position, created_at)
			VALUES ('x', ?, ?, 1, 1, 1, 0, 0)`, int64(w.ID), int64(r.ID))
		if err == nil {
			t.Error("image with two owners accepted")
		}
		_, err = db.sql.ExecContext(ctx, `INSERT INTO images (file_key, width, height, bytes, position, created_at)
			VALUES ('y', 1, 1, 1, 0, 0)`)
		if err == nil {
			t.Error("image without an owner accepted")
		}
	})
}

func TestSchemaChecks(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	insert := func(minor, currency, status any, category any) error {
		_, err := db.sql.ExecContext(ctx, `INSERT INTO wishes (title, title_key, category_id, price_minor, price_currency, status, author_id, created_at, updated_at)
			VALUES ('t', 't', ?, ?, ?, ?, 1, 0, 0)`, category, minor, currency, status)
		return err
	}
	if err := insert(100, nil, "want", nil); err == nil {
		t.Error("price without currency accepted")
	}
	if err := insert(nil, "EUR", "want", nil); err == nil {
		t.Error("currency without price accepted")
	}
	if err := insert(0, "EUR", "want", nil); err == nil {
		t.Error("zero price accepted")
	}
	if err := insert(nil, nil, "maybe", nil); err == nil {
		t.Error("unknown status accepted")
	}
	if err := insert(nil, nil, "want", 9999); err == nil {
		t.Error("dangling category accepted (foreign keys off?)")
	}
	if err := insert(100, "EUR", "done", nil); err != nil {
		t.Errorf("valid row rejected: %v", err)
	}
}

func TestCategories(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()

	books, err := db.InsertCategory(ctx, domain.Category{Name: "Книги", Emoji: "📚", CreatedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if books.ID == 0 || books.Position != len(domain.DefaultCategories()) {
		t.Errorf("InsertCategory = %+v, want the next position", books)
	}
	got, err := db.GetCategory(ctx, books.ID)
	if err != nil || got != books {
		t.Errorf("GetCategory = %+v, %v; want %+v", got, err, books)
	}

	for _, name := range []string{"книги", "КНИГИ", "Покупки", "пОКУПКИ"} {
		if _, err := db.InsertCategory(ctx, domain.Category{Name: name, Emoji: "📦", CreatedAt: testNow}); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("InsertCategory(%q) = %v, want ErrConflict", name, err)
		}
	}

	renamed, err := db.ModifyCategory(ctx, books.ID, func(c *domain.Category) error { return c.Rename("КНИГИ", "📖") })
	if err != nil {
		t.Fatalf("renaming to a different case of the same name: %v", err)
	}
	if renamed.Name != "КНИГИ" || renamed.Emoji != "📖" || renamed.Position != books.Position {
		t.Errorf("ModifyCategory = %+v", renamed)
	}
	if _, err := db.ModifyCategory(ctx, books.ID, func(c *domain.Category) error { return c.Rename("покупки", "📖") }); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("rename onto an existing name = %v, want ErrConflict", err)
	}
	if _, err := db.ModifyCategory(ctx, 9999, func(c *domain.Category) error { return c.Rename("Нечто", "📖") }); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ModifyCategory(missing) = %v, want ErrNotFound", err)
	}
	if err := db.DeleteCategory(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("DeleteCategory(missing) = %v, want ErrNotFound", err)
	}
}

func TestDeleteCategoryKeepsWishes(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	cats, _ := db.ListCategories(ctx)
	w := mustWish(t, db, domain.WishDraft{Title: "Рюкзак", CategoryID: &cats[0].ID}, testNow)

	if err := db.DeleteCategory(ctx, cats[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetWish(ctx, w.ID)
	if err != nil {
		t.Fatalf("wish disappeared with its category: %v", err)
	}
	if got.CategoryID != nil {
		t.Errorf("CategoryID = %v, want nil", *got.CategoryID)
	}
}

func TestRecipes(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()

	if _, err := db.RandomRecipe(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("RandomRecipe(empty) = %v, want ErrNotFound", err)
	}

	carbonara := mustRecipe(t, db, domain.RecipeDraft{Title: "Паста карбонара", Link: ptr("https://example.com/c"), Body: "Сварить пасту.\nДобавить ГУАНЧАЛЕ."}, testNow)
	borscht := mustRecipe(t, db, domain.RecipeDraft{Title: "Борщ"}, testNow.Add(time.Minute))
	img := mustImage(t, db, service.RecipeImages(borscht.ID), testKey(1))

	got, err := db.GetRecipe(ctx, carbonara.ID)
	if err != nil || got.Title != carbonara.Title || got.Body != carbonara.Body || *got.Link != *carbonara.Link || got.AuthorID != 222 {
		t.Errorf("GetRecipe = %+v, %v", got, err)
	}

	cases := []struct {
		query string
		want  []domain.RecipeID
	}{
		{"", []domain.RecipeID{borscht.ID, carbonara.ID}},
		{"КАРБОНАРА", []domain.RecipeID{carbonara.ID}},
		{"гуанчале", []domain.RecipeID{carbonara.ID}},
		{"борщ", []domain.RecipeID{borscht.ID}},
		{"пицца", nil},
	}
	for _, tc := range cases {
		list, err := db.ListRecipes(ctx, domain.RecipeFilter{Query: tc.query})
		if err != nil {
			t.Fatal(err)
		}
		var ids []domain.RecipeID
		for _, r := range list {
			ids = append(ids, r.ID)
			if r.ID == borscht.ID && (len(r.Images) != 1 || r.Images[0].ID != img.ID) {
				t.Errorf("borscht images = %+v", r.Images)
			}
		}
		if !slices.Equal(ids, tc.want) {
			t.Errorf("ListRecipes(%q) = %v, want %v", tc.query, ids, tc.want)
		}
	}

	seen := map[domain.RecipeID]bool{}
	for range 50 {
		r, err := db.RandomRecipe(ctx)
		if err != nil {
			t.Fatal(err)
		}
		seen[r.ID] = true
		if r.ID == borscht.ID && len(r.Images) != 1 {
			t.Errorf("RandomRecipe did not load images")
		}
	}
	if len(seen) != 2 {
		t.Errorf("RandomRecipe returned %d distinct recipes in 50 draws, want 2", len(seen))
	}

	updated, err := db.ModifyRecipe(ctx, carbonara.ID, func(r *domain.Recipe) error {
		return r.Apply(domain.RecipePatch{Link: domain.Some[*string](nil), Body: domain.Some("Новый текст")}, testNow.Add(time.Hour))
	})
	if err != nil || updated.Link != nil || updated.Body != "Новый текст" {
		t.Fatalf("ModifyRecipe = %+v, %v", updated, err)
	}
	if list, _ := db.ListRecipes(ctx, domain.RecipeFilter{Query: "гуанчале"}); len(list) != 0 {
		t.Error("search key was not updated with the body")
	}

	if n, err := db.CountRecipes(ctx); err != nil || n != 2 {
		t.Errorf("CountRecipes = %d, %v; want 2", n, err)
	}
	keys, err := db.DeleteRecipe(ctx, borscht.ID)
	if err != nil || !slices.Equal(keys, []string{testKey(1)}) {
		t.Errorf("DeleteRecipe = %v, %v", keys, err)
	}
	if _, err := db.GetImage(ctx, img.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("recipe image survived the cascade: %v", err)
	}
	if _, err := db.DeleteRecipe(ctx, borscht.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second DeleteRecipe = %v, want ErrNotFound", err)
	}
}

func TestUsersUpsertKeepsHasChatSticky(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()

	if _, err := db.GetUser(ctx, 111); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetUser(unknown) = %v, want ErrNotFound", err)
	}
	if err := db.UpsertUser(ctx, domain.User{ID: 111, FirstName: "Дима", HasChat: true, UpdatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertUser(ctx, domain.User{ID: 111, FirstName: "Дмитрий", Username: "dima", UpdatedAt: testNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertUser(ctx, domain.User{ID: 42, FirstName: "Аня", UpdatedAt: testNow}); err != nil {
		t.Fatal(err)
	}

	u, err := db.GetUser(ctx, 111)
	if err != nil {
		t.Fatal(err)
	}
	if u.FirstName != "Дмитрий" || u.Username != "dima" || !u.HasChat || !u.UpdatedAt.Equal(testNow.Add(time.Hour)) {
		t.Errorf("GetUser = %+v", u)
	}
	users, err := db.ListUsers(ctx)
	if err != nil || len(users) != 2 || users[0].ID != 42 || users[1].ID != 111 {
		t.Errorf("ListUsers = %+v, %v", users, err)
	}
}

func TestSearchKey(t *testing.T) {
	cases := map[string]string{
		"Поездка в ТОКИО": "поездка в токио",
		"Ёжик":            "ежик",
		"ΣΊΣΥΦΟΣ":         "σίσυφοσ",
		"σίσυφος":         "σίσυφοσ",
		"Straße":          "straße",
	}
	for in, want := range cases {
		if got := searchKey(in); got != want {
			t.Errorf("searchKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConcurrentWritersRespectImageLimit(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	w := mustWish(t, db, domain.WishDraft{Title: "Гонка"}, testNow)

	const writers, limit = 12, 3
	errs := make(chan error, writers)
	for i := range writers {
		go func() {
			_, err := db.InsertImage(ctx, service.WishImages(w.ID),
				domain.Image{Key: testKey(i), Width: 1, Height: 1, CreatedAt: testNow}, limit)
			errs <- err
		}()
	}
	ok := 0
	for range writers {
		switch err := <-errs; {
		case err == nil:
			ok++
		case !errors.Is(err, domain.ErrLimitExceeded):
			t.Errorf("unexpected error (busy database?): %v", err)
		}
	}
	if ok != limit {
		t.Errorf("%d inserts succeeded, want exactly %d", ok, limit)
	}
	got, err := db.GetWish(ctx, w.ID)
	if err != nil || len(got.Images) != limit {
		t.Fatalf("wish has %d images (%v), want %d", len(got.Images), err, limit)
	}
	for i, img := range got.Images {
		if img.Position != i {
			t.Errorf("image %d has position %d", i, img.Position)
		}
	}
}
