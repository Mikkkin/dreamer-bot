package sqlite

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// v1Columns lists every column that schema version 1 had, per table. The
// upgrade must keep all of them, with every value, unchanged.
var v1Columns = map[string]string{
	"users":      "id, first_name, last_name, username, has_chat, updated_at",
	"categories": "id, name, name_key, emoji, position, created_at",
	"wishes": "id, title, title_key, note, category_id, link, price_minor, price_currency, status, hot, " +
		"author_id, created_at, updated_at, fulfilled_at",
	"recipes": "id, title, title_key, link, body, body_key, author_id, created_at, updated_at",
	"images":  "id, file_key, wish_id, recipe_id, width, height, bytes, position, created_at",
}

// openAtVersion creates a database at path with only the first version
// migrations applied, i.e. as a binary of that version left it.
func openAtVersion(t *testing.T, path string, version int) *DB {
	t.Helper()
	dsn, err := prepareFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db := &DB{sql: sqlDB, log: slog.New(slog.DiscardHandler)}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.applyMigrations(context.Background(), migrations[:version]); err != nil {
		t.Fatalf("migrate to v%d: %v", version, err)
	}
	return db
}

// snapshotV1 reads every version-1 column of every version-1 table.
func snapshotV1(t *testing.T, db *DB) map[string][][]any {
	t.Helper()
	out := make(map[string][][]any, len(v1Columns))
	for table, cols := range v1Columns {
		rows, err := db.sql.Query(`SELECT ` + cols + ` FROM ` + table + ` ORDER BY rowid`)
		if err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		names, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			vals := make([]any, len(names))
			ptrs := make([]any, len(names))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			out[table] = append(out[table], vals)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
	}
	return out
}

// seedV1 writes the kind of data production holds, through the SQL that the
// version-1 binary ran.
func seedV1(t *testing.T, db *DB) {
	t.Helper()
	ms := func(t time.Time) int64 { return t.UnixMilli() }
	created, fulfilled := testNow.Add(-72*time.Hour), testNow.Add(-time.Hour)
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, first_name, last_name, username, has_chat, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			[]any{111, "Дима", "", "dima", 1, ms(testNow)}},
		{`INSERT INTO users (id, first_name, last_name, username, has_chat, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			[]any{222, "Аня", "К.", "", 0, ms(testNow)}},
		{`INSERT INTO categories (name, name_key, emoji, position, created_at) VALUES (?, ?, ?, ?, ?)`,
			[]any{"Книги", "книги", "📚", 6, ms(created)}},
		{`DELETE FROM categories WHERE name = 'Подарки'`, nil},
		{`INSERT INTO wishes (title, title_key, note, category_id, link, price_minor, price_currency, status, hot,
			author_id, created_at, updated_at, fulfilled_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"Поездка в Токио", "поездка в токио", "Весной,\nна сакуру", 2, "https://example.com/tour",
				120050, "EUR", "progress", 1, 111, ms(created), ms(testNow), nil}},
		{`INSERT INTO wishes (title, title_key, note, category_id, link, price_minor, price_currency, status, hot,
			author_id, created_at, updated_at, fulfilled_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"Диван", "диван", "", nil, nil, 4500000, "RUB", "done", 0, 222, ms(created), ms(fulfilled), ms(fulfilled)}},
		{`INSERT INTO wishes (title, title_key, status, author_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			[]any{"Просто так", "просто так", "want", 111, ms(created), ms(created)}},
		{`INSERT INTO recipes (title, title_key, link, body, body_key, author_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"Паста карбонара", "паста карбонара", "https://example.com/c", "Сварить пасту.\nДобавить гуанчале.",
				"сварить пасту.\nдобавить гуанчале.", 222, ms(created), ms(created)}},
		{`INSERT INTO recipes (title, title_key, author_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			[]any{"Борщ", "борщ", 111, ms(testNow), ms(testNow)}},
		{`INSERT INTO images (file_key, wish_id, recipe_id, width, height, bytes, position, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, []any{testKey(1), 1, nil, 1600, 1200, 34567, 0, ms(created)}},
		{`INSERT INTO images (file_key, wish_id, recipe_id, width, height, bytes, position, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, []any{testKey(2), 1, nil, 800, 600, 12345, 1, ms(created)}},
		{`INSERT INTO images (file_key, wish_id, recipe_id, width, height, bytes, position, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, []any{testKey(3), nil, 1, 1170, 2532, 99999, 0, ms(created)}},
	}
	for _, s := range stmts {
		if _, err := db.sql.Exec(s.sql, s.args...); err != nil {
			t.Fatalf("seed v1: %v\n%s", err, s.sql)
		}
	}
}

func TestUpgradeFromV1KeepsEveryRow(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "prod.db")

	old := openAtVersion(t, path, 1)
	seedV1(t, old)
	before := snapshotV1(t, old)
	if len(before["wishes"]) != 3 || len(before["recipes"]) != 2 || len(before["images"]) != 3 || len(before["users"]) != 2 {
		t.Fatalf("v1 fixture incomplete: %v", before)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if after := snapshotV1(t, db); !reflect.DeepEqual(after, before) {
		t.Fatalf("version-1 data changed by the upgrade:\nbefore %v\nafter  %v", before, after)
	}
	assertSchemaV2(t, db)

	// The old rows read back through the new code, with the new fields empty.
	w, err := db.GetWish(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if w.Title != "Поездка в Токио" || w.Note != "Весной,\nна сакуру" || *w.CategoryID != 2 || *w.Link != "https://example.com/tour" ||
		*w.Price != (domain.Money{Minor: 120050, Currency: "EUR"}) || w.Status != domain.StatusProgress || !w.Hot ||
		w.AuthorID != 111 || len(w.Images) != 2 || w.Images[1].Key != testKey(2) || w.Saved != nil {
		t.Errorf("old wish reads back as %+v", w)
	}
	if done, _ := db.GetWish(ctx, 2); done.FulfilledAt == nil || done.Status != domain.StatusDone {
		t.Errorf("fulfilled wish lost its stamp: %+v", done)
	}
	recipes, err := db.ListRecipes(ctx, domain.RecipeFilter{})
	if err != nil || len(recipes) != 2 {
		t.Fatalf("ListRecipes = %+v, %v", recipes, err)
	}
	carbonara := recipes[1]
	if carbonara.Title != "Паста карбонара" || *carbonara.Link != "https://example.com/c" || len(carbonara.Images) != 1 ||
		carbonara.CuisineID != nil || carbonara.Nutrition != nil || carbonara.CourseIDs == nil || len(carbonara.CourseIDs) != 0 ||
		carbonara.Ingredients == nil || len(carbonara.Ingredients) != 0 || carbonara.Cooking != (domain.CookingSummary{}) {
		t.Errorf("old recipe reads back as %+v", carbonara)
	}
	if found, _ := db.ListRecipes(ctx, domain.RecipeFilter{Query: "ГУАНЧАЛЕ"}); len(found) != 1 {
		t.Error("search over old recipes broke")
	}
	cats, _ := db.ListCategories(ctx)
	if len(cats) != len(domain.DefaultCategories()) || cats[len(cats)-1].Name != "Книги" {
		t.Errorf("categories after upgrade = %+v", cats)
	}
	assertDefaultTags(t, db)

	// Reopening is a no-op: nothing is migrated or seeded twice.
	for range 2 {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if db, err = Open(ctx, path, nil); err != nil {
			t.Fatalf("reopen: %v", err)
		}
	}
	defer db.Close()
	if after := snapshotV1(t, db); !reflect.DeepEqual(after, before) {
		t.Fatal("version-1 data changed by a reopen")
	}
	assertSchemaV2(t, db)
	assertDefaultTags(t, db)

	// The new features work on the old rows.
	tags, _ := db.ListRecipeTags(ctx)
	if _, err := db.ModifyRecipe(ctx, carbonara.ID, func(r *domain.Recipe) error {
		return r.Apply(domain.RecipePatch{CuisineID: domain.Some(&tags[2].ID)}, testNow)
	}); err != nil {
		t.Fatalf("tag an old recipe: %v", err)
	}
	if _, err := db.InsertCook(ctx, domain.Cook{RecipeID: carbonara.ID, CookedBy: 111, CookedAt: testNow}); err != nil {
		t.Fatalf("cook an old recipe: %v", err)
	}
	if _, _, err := db.InsertSaving(ctx, 1, func(w *domain.Wish) (domain.Saving, error) {
		return domain.NewSaving(*w, domain.Money{Minor: 5000, Currency: "EUR"}, 111, "", testNow)
	}); err != nil {
		t.Fatalf("save for an old wish: %v", err)
	}

	// A deleted default tag does not come back on restart.
	if err := db.DeleteRecipeTag(ctx, tags[0].ID); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if db, err = Open(ctx, path, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := db.ListRecipeTags(ctx); len(again) != len(tags)-1 {
		t.Errorf("after deleting a default tag and restarting: %d tags, want %d", len(again), len(tags)-1)
	}
}

func assertSchemaV2(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	var versions []int
	rows, err := db.sql.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	_ = rows.Close()
	if !slices.Equal(versions, []int{1, 2}) {
		t.Errorf("schema versions = %v, want [1 2]", versions)
	}
	for _, table := range []string{"recipe_tags", "recipe_courses", "recipe_ingredients", "recipe_cooks",
		"recipe_ratings", "wish_savings", "shopping_items"} {
		var strict int
		if err := db.sql.QueryRowContext(ctx, `SELECT strict FROM pragma_table_list WHERE name = ?`, table).Scan(&strict); err != nil {
			t.Errorf("table %s missing: %v", table, err)
		} else if strict != 1 {
			t.Errorf("table %s is not STRICT", table)
		}
	}
	for _, col := range []string{"cuisine_id", "kcal_tenths", "protein_tenths", "fat_tenths", "carbs_tenths", "weight_g", "servings"} {
		var n int
		if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('recipes') WHERE name = ?`, col).Scan(&n); err != nil || n != 1 {
			t.Errorf("recipes.%s missing (%v)", col, err)
		}
	}
	var check string
	if err := db.sql.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
		t.Errorf("integrity_check = %q, %v", check, err)
	}
	fk, err := db.sql.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if fk.Next() {
		t.Error("foreign_key_check reports violations")
	}
	_ = fk.Close()
}

func assertDefaultTags(t *testing.T, db *DB) {
	t.Helper()
	tags, err := db.ListRecipeTags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defaults := domain.DefaultRecipeTags()
	if len(tags) != len(defaults) {
		t.Fatalf("%d recipe tags, want the %d defaults", len(tags), len(defaults))
	}
	for i, tag := range tags {
		d := defaults[i]
		if tag.Kind != d.Kind || tag.Name != d.Name || tag.Emoji != d.Emoji || tag.Position != d.Position || tag.ID == 0 {
			t.Errorf("tag %d = %+v, want %+v", i, tag, d)
		}
	}
}

// A fresh install gets both migrations and both seeds.
func TestFreshInstallHasSchemaV2(t *testing.T) {
	db, _ := openTestDB(t)
	assertSchemaV2(t, db)
	assertDefaultTags(t, db)
}

// The upgrade must not depend on anything but the database: images owned
// through the new code keep working for old recipes.
func TestUpgradedDatabaseAcceptsNewImages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prod.db")
	old := openAtVersion(t, path, 1)
	seedV1(t, old)
	_ = old.Close()
	db, err := Open(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	img := mustImage(t, db, service.RecipeImages(1), testKey(50))
	if img.Position != 1 {
		t.Errorf("new image of an old recipe at position %d, want 1", img.Position)
	}
}
