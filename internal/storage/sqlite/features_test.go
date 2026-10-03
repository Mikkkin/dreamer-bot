package sqlite

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func tagsByKind(t *testing.T, db *DB) (cuisines, courses []domain.RecipeTag) {
	t.Helper()
	tags, err := db.ListRecipeTags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range tags {
		if tag.Kind == domain.TagCuisine {
			cuisines = append(cuisines, tag)
		} else {
			courses = append(courses, tag)
		}
	}
	return cuisines, courses
}

func qty(t *testing.T, amount, unit string) *domain.Quantity {
	t.Helper()
	q, err := domain.ParseQuantity(amount, unit)
	if err != nil {
		t.Fatalf("ParseQuantity(%q, %q): %v", amount, unit, err)
	}
	return q
}

// qs renders a quantity with plain spaces ("750 мл"), unlike Format, which
// uses a no-break space for display.
func qs(q *domain.Quantity) string {
	if q == nil {
		return "<nil>"
	}
	return strings.TrimSpace(q.Amount() + " " + string(q.Unit))
}

func countRows(t *testing.T, db *DB, table string) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRecipeTagsCRUD(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	cuisines, courses := tagsByKind(t, db)

	georgian, err := db.InsertRecipeTag(ctx, domain.RecipeTag{Kind: domain.TagCuisine, Name: "Грузинская", Emoji: "🍢", CreatedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if georgian.ID == 0 || georgian.Position != len(cuisines) {
		t.Errorf("InsertRecipeTag = %+v, want position %d (end of the cuisines)", georgian, len(cuisines))
	}
	brunch, err := db.InsertRecipeTag(ctx, domain.RecipeTag{Kind: domain.TagCourse, Name: "Бранч", Emoji: "🥞", CreatedAt: testNow})
	if err != nil || brunch.Position != len(courses) {
		t.Fatalf("course position = %+v, %v; want %d", brunch, err, len(courses))
	}

	all, _ := db.ListRecipeTags(ctx)
	for i, tag := range all {
		if i > 0 && all[i-1].Kind == domain.TagCourse && tag.Kind == domain.TagCuisine {
			t.Fatal("cuisines must come before courses")
		}
	}
	if all[len(cuisines)].ID != georgian.ID || all[len(all)-1].ID != brunch.ID {
		t.Errorf("new tags are not at the end of their kind: %+v", all)
	}

	for _, name := range []string{"грузинская", "ГРУЗИНСКАЯ", "Русская"} {
		if _, err := db.InsertRecipeTag(ctx, domain.RecipeTag{Kind: domain.TagCuisine, Name: name, Emoji: "🍢"}); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("InsertRecipeTag(cuisine %q) = %v, want ErrConflict", name, err)
		}
	}
	if _, err := db.InsertRecipeTag(ctx, domain.RecipeTag{Kind: domain.TagCourse, Name: "Грузинская", Emoji: "🍢"}); err != nil {
		t.Errorf("the same name in the other kind must be allowed: %v", err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO recipe_tags (kind, name, name_key, emoji, position, created_at)
		VALUES ('colour', 'x', 'x', '🔴', 0, 0)`); err == nil {
		t.Error("unknown tag kind accepted by the schema")
	}

	renamed, err := db.ModifyRecipeTag(ctx, georgian.ID, func(tag *domain.RecipeTag) error {
		tag.Kind = domain.TagCourse // must be ignored
		tag.Position = 99           // must be ignored
		return tag.Apply(domain.RecipeTagPatch{Name: domain.Some("Кавказская кухня"), Emoji: domain.Some("🥙")})
	})
	if err != nil || renamed.Name != "Кавказская кухня" || renamed.Emoji != "🥙" || renamed.Kind != domain.TagCuisine || renamed.Position != georgian.Position {
		t.Fatalf("ModifyRecipeTag = %+v, %v", renamed, err)
	}
	all, _ = db.ListRecipeTags(ctx)
	if i := slices.IndexFunc(all, func(tag domain.RecipeTag) bool { return tag.ID == georgian.ID }); i < 0 || all[i] != renamed {
		t.Errorf("stored tag differs from the returned %+v: %+v", renamed, all)
	}
	if _, err := db.ModifyRecipeTag(ctx, georgian.ID, func(tag *domain.RecipeTag) error {
		return tag.Apply(domain.RecipeTagPatch{Name: domain.Some("итальянская")})
	}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("rename onto an existing cuisine = %v, want ErrConflict", err)
	}
	if _, err := db.ModifyRecipeTag(ctx, 9999, func(*domain.RecipeTag) error { return nil }); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ModifyRecipeTag(missing) = %v, want ErrNotFound", err)
	}
	if err := db.DeleteRecipeTag(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("DeleteRecipeTag(missing) = %v, want ErrNotFound", err)
	}
}

func TestRecipeFieldsRoundTrip(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	cuisines, courses := tagsByKind(t, db)
	nutrition, err := domain.NewNutrition("150", "12,5", "6", "10.4", 800, 4)
	if err != nil {
		t.Fatal(err)
	}
	ingredients := []domain.Ingredient{
		{Name: "Спагетти", Quantity: qty(t, "320", "г")},
		{Name: "Соль", Quantity: qty(t, "", "по вкусу")},
		{Name: "Яйца", Quantity: qty(t, "4", "")},
		{Name: "Перец"},
		{Name: "Лук", Quantity: &domain.Quantity{Unit: domain.UnitPiece}},
		{Name: "Масло", Quantity: qty(t, "1,5", "ст. л.")},
	}
	r := mustRecipe(t, db, domain.RecipeDraft{
		Title:       "Паста карбонара",
		CuisineID:   &cuisines[2].ID,
		CourseIDs:   []domain.RecipeTagID{courses[2].ID, courses[0].ID, courses[4].ID},
		Ingredients: ingredients,
		Nutrition:   &nutrition,
	}, testNow)

	got, err := db.GetRecipe(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *got.CuisineID != cuisines[2].ID {
		t.Errorf("cuisine = %v", *got.CuisineID)
	}
	if want := []domain.RecipeTagID{courses[2].ID, courses[0].ID, courses[4].ID}; !slices.Equal(got.CourseIDs, want) {
		t.Errorf("courses = %v, want %v in the chosen order", got.CourseIDs, want)
	}
	if !slices.EqualFunc(got.Ingredients, ingredients, func(a, b domain.Ingredient) bool {
		return a.Name == b.Name && ((a.Quantity == nil && b.Quantity == nil) || (a.Quantity != nil && b.Quantity != nil && *a.Quantity == *b.Quantity))
	}) {
		t.Errorf("ingredients = %+v, want %+v", got.Ingredients, ingredients)
	}
	if got.Nutrition == nil || *got.Nutrition != nutrition || got.Servings == nil || *got.Servings != 4 {
		t.Errorf("nutrition = %+v, servings %v; want %+v and 4", got.Nutrition, got.Servings, nutrition)
	}
	if got.Cooking != (domain.CookingSummary{}) {
		t.Errorf("new recipe has cooking %+v", got.Cooking)
	}

	// КБЖУ without weight keeps it unknown; without servings it keeps the
	// recipe's servings, which the КБЖУ mirrors.
	per100, _ := domain.NewNutrition("0", "0", "0", "0", 0, 0)
	mirrored := per100
	mirrored.Servings = 4
	updated, err := db.ModifyRecipe(ctx, r.ID, func(r *domain.Recipe) error {
		return r.Apply(domain.RecipePatch{
			CuisineID:   domain.Some[*domain.RecipeTagID](nil),
			CourseIDs:   domain.Some([]domain.RecipeTagID{courses[1].ID}),
			Ingredients: domain.Some(ingredients[:1]),
			Nutrition:   domain.Some(&per100),
		}, testNow.Add(time.Hour))
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := db.GetRecipe(ctx, r.ID)
	for _, x := range []domain.Recipe{updated, stored} {
		if x.CuisineID != nil || !slices.Equal(x.CourseIDs, []domain.RecipeTagID{courses[1].ID}) || len(x.Ingredients) != 1 ||
			x.Nutrition == nil || *x.Nutrition != mirrored || x.Servings == nil || *x.Servings != 4 {
			t.Errorf("after replacing = %+v, nutrition %+v", x, x.Nutrition)
		}
	}

	cleared, err := db.ModifyRecipe(ctx, r.ID, func(r *domain.Recipe) error {
		return r.Apply(domain.RecipePatch{
			CourseIDs:   domain.Some([]domain.RecipeTagID{}),
			Ingredients: domain.Some([]domain.Ingredient{}),
			Nutrition:   domain.Some[*domain.Nutrition](nil),
		}, testNow.Add(2*time.Hour))
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ = db.GetRecipe(ctx, r.ID)
	for _, x := range []domain.Recipe{cleared, stored} {
		if x.CourseIDs == nil || len(x.CourseIDs) != 0 || x.Ingredients == nil || len(x.Ingredients) != 0 || x.Nutrition != nil ||
			x.Servings == nil || *x.Servings != 4 {
			t.Errorf("after clearing = %+v (lists must be empty, not nil; servings outlive the КБЖУ)", x)
		}
	}
	if n := countRows(t, db, "recipe_ingredients") + countRows(t, db, "recipe_courses"); n != 0 {
		t.Errorf("%d orphan list rows after clearing", n)
	}
}

func TestRecipeFiltersAndTagDeletion(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	cuisines, courses := tagsByKind(t, db)
	russian, italian := cuisines[0].ID, cuisines[2].ID
	breakfast, dinner, soup := courses[0].ID, courses[2].ID, courses[3].ID

	syrniki := mustRecipe(t, db, domain.RecipeDraft{Title: "Сырники", CuisineID: &russian, CourseIDs: []domain.RecipeTagID{breakfast}}, testNow)
	borscht := mustRecipe(t, db, domain.RecipeDraft{Title: "Борщ", CuisineID: &russian, CourseIDs: []domain.RecipeTagID{soup, dinner}}, testNow.Add(time.Minute))
	pasta := mustRecipe(t, db, domain.RecipeDraft{Title: "Паста", CuisineID: &italian, CourseIDs: []domain.RecipeTagID{dinner}}, testNow.Add(2*time.Minute))
	plain := mustRecipe(t, db, domain.RecipeDraft{Title: "Бутерброд"}, testNow.Add(3*time.Minute))

	ids := func(f domain.RecipeFilter) []domain.RecipeID {
		t.Helper()
		list, err := db.ListRecipes(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []domain.RecipeID
		for _, r := range list {
			out = append(out, r.ID)
		}
		return out
	}
	cases := []struct {
		name string
		f    domain.RecipeFilter
		want []domain.RecipeID
	}{
		{"all", domain.RecipeFilter{}, []domain.RecipeID{plain.ID, pasta.ID, borscht.ID, syrniki.ID}},
		{"cuisine", domain.RecipeFilter{CuisineID: russian}, []domain.RecipeID{borscht.ID, syrniki.ID}},
		{"course", domain.RecipeFilter{CourseID: dinner}, []domain.RecipeID{pasta.ID, borscht.ID}},
		{"cuisine and course", domain.RecipeFilter{CuisineID: russian, CourseID: dinner}, []domain.RecipeID{borscht.ID}},
		{"query and cuisine", domain.RecipeFilter{Query: "сыр", CuisineID: russian}, []domain.RecipeID{syrniki.ID}},
		{"unknown tag", domain.RecipeFilter{CourseID: 9999}, nil},
	}
	for _, tc := range cases {
		if got := ids(tc.f); !slices.Equal(got, tc.want) {
			t.Errorf("%s: ListRecipes = %v, want %v", tc.name, got, tc.want)
		}
	}

	// Deleting tags detaches them; the recipes stay.
	if err := db.DeleteRecipeTag(ctx, russian); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteRecipeTag(ctx, dinner); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetRecipe(ctx, borscht.ID)
	if err != nil {
		t.Fatalf("recipe disappeared with its tags: %v", err)
	}
	if got.CuisineID != nil || !slices.Equal(got.CourseIDs, []domain.RecipeTagID{soup}) {
		t.Errorf("after deleting tags: cuisine %v, courses %v", got.CuisineID, got.CourseIDs)
	}
	if got := ids(domain.RecipeFilter{}); len(got) != 4 {
		t.Errorf("%d recipes after deleting tags, want 4", len(got))
	}

	// A tag deleted between the service's check and the write is a validation
	// error, and nothing is written.
	gone := russian
	r, err := domain.NewRecipe(domain.RecipeDraft{Title: "Пельмени", CuisineID: &gone}, 111, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertRecipe(ctx, r); !errors.Is(err, domain.ErrRecipeTagGone) {
		t.Errorf("InsertRecipe with a deleted cuisine = %v, want ErrRecipeTagGone", err)
	}
	if _, err := db.ModifyRecipe(ctx, plain.ID, func(r *domain.Recipe) error {
		return r.Apply(domain.RecipePatch{Title: domain.Some("Бутерброд с сыром"), CourseIDs: domain.Some([]domain.RecipeTagID{dinner})}, testNow)
	}); !errors.Is(err, domain.ErrRecipeTagGone) {
		t.Errorf("ModifyRecipe with a deleted course = %v, want ErrRecipeTagGone", err)
	}
	if got, _ := db.GetRecipe(ctx, plain.ID); got.Title != "Бутерброд" {
		t.Errorf("the failed update leaked: %q", got.Title)
	}
	if got := ids(domain.RecipeFilter{}); len(got) != 4 {
		t.Errorf("%d recipes after the failed insert, want 4", len(got))
	}
}

func TestRecipeSchemaChecks(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	r := mustRecipe(t, db, domain.RecipeDraft{Title: "Проверка"}, testNow)
	w := mustWish(t, db, domain.WishDraft{Title: "Проверка"}, testNow)
	c, err := db.InsertCook(ctx, domain.Cook{RecipeID: r.ID, CookedBy: 1, CookedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}

	// ?1 recipe, ?2 wish, ?3 cook. Each bad statement must fail on a
	// constraint (not on a typo); its good twin proves the statement shape.
	cases := []struct{ name, bad, good string }{
		{"partial nutrition",
			`UPDATE recipes SET kcal_tenths = 100 WHERE id = ?1`,
			`UPDATE recipes SET kcal_tenths = 100, protein_tenths = 0, fat_tenths = 0, carbs_tenths = 0 WHERE id = ?1`},
		{"kcal out of range",
			`UPDATE recipes SET kcal_tenths = 9001 WHERE id = ?1`,
			`UPDATE recipes SET kcal_tenths = 9000 WHERE id = ?1`},
		{"zero weight",
			`UPDATE recipes SET weight_g = 0 WHERE id = ?1`,
			`UPDATE recipes SET weight_g = 1 WHERE id = ?1`},
		{"dangling cuisine",
			`UPDATE recipes SET cuisine_id = 9999 WHERE id = ?1`,
			`UPDATE recipes SET cuisine_id = (SELECT MIN(id) FROM recipe_tags) WHERE id = ?1`},
		{"dangling course",
			`INSERT INTO recipe_courses (recipe_id, tag_id, position) VALUES (?1, 9999, 0)`,
			`INSERT INTO recipe_courses (recipe_id, tag_id, position) VALUES (?1, (SELECT MAX(id) FROM recipe_tags), 0)`},
		{"zero quantity",
			`INSERT INTO recipe_ingredients (recipe_id, position, name, qty_hundredths) VALUES (?1, 0, 'x', 0)`,
			`INSERT INTO recipe_ingredients (recipe_id, position, name, qty_hundredths) VALUES (?1, 0, 'x', 1)`},
		{"zero stars",
			`INSERT INTO recipe_ratings (cook_id, user_id, stars, rated_at) VALUES (?3, 1, 0, 0)`,
			`INSERT INTO recipe_ratings (cook_id, user_id, stars, rated_at) VALUES (?3, 1, 1, 0)`},
		{"six stars",
			`INSERT INTO recipe_ratings (cook_id, user_id, stars, rated_at) VALUES (?3, 2, 6, 0)`,
			`INSERT INTO recipe_ratings (cook_id, user_id, stars, rated_at) VALUES (?3, 2, 5, 0)`},
		{"second rating of one person",
			`INSERT INTO recipe_ratings (cook_id, user_id, stars, rated_at) VALUES (?3, 1, 3, 0)`,
			`INSERT INTO recipe_ratings (cook_id, user_id, stars, rated_at) VALUES (?3, 3, 3, 0)`},
		{"saving of no wish",
			`INSERT INTO wish_savings (wish_id, amount_minor, currency, user_id, created_at) VALUES (9999, 1, 'RUB', 1, 0)`,
			`INSERT INTO wish_savings (wish_id, amount_minor, currency, user_id, created_at) VALUES (?2, 1, 'RUB', 1, 0)`},
		{"zero saving",
			`INSERT INTO wish_savings (wish_id, amount_minor, currency, user_id, created_at) VALUES (?2, 0, 'RUB', 1, 0)`,
			`INSERT INTO wish_savings (wish_id, amount_minor, currency, user_id, created_at) VALUES (?2, 2, 'RUB', 1, 0)`},
		{"shopping item of no recipe",
			`INSERT INTO shopping_items (name, name_key, recipe_id, added_by, created_at, updated_at) VALUES ('x', 'x', 9999, 1, 0, 0)`,
			`INSERT INTO shopping_items (name, name_key, recipe_id, added_by, created_at, updated_at) VALUES ('x', 'x', ?1, 1, 0, 0)`},
		{"checked is a flag",
			`INSERT INTO shopping_items (name, name_key, checked, added_by, created_at, updated_at) VALUES ('y', 'y', 2, 1, 0, 0)`,
			`INSERT INTO shopping_items (name, name_key, checked, added_by, created_at, updated_at) VALUES ('y', 'y', 1, 1, 0, 0)`},
	}
	args := []any{int64(r.ID), int64(w.ID), int64(c.ID)}
	for _, tc := range cases {
		_, err := db.sql.ExecContext(ctx, tc.bad, args...)
		if err == nil || !strings.Contains(err.Error(), "constraint failed") {
			t.Errorf("%s: bad statement = %v, want a constraint violation", tc.name, err)
		}
		if _, err := db.sql.ExecContext(ctx, tc.good, args...); err != nil {
			t.Errorf("%s: good statement failed: %v", tc.name, err)
		}
	}
}

func TestCooksAndRatings(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	pasta := mustRecipe(t, db, domain.RecipeDraft{Title: "Паста"}, testNow)
	soup := mustRecipe(t, db, domain.RecipeDraft{Title: "Суп"}, testNow.Add(time.Minute))
	rating := func(user domain.UserID, stars int, at time.Time) domain.Rating {
		r, err := domain.NewRating(user, stars, "", at)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	if _, err := db.InsertCook(ctx, domain.Cook{RecipeID: 9999, CookedBy: 111, CookedAt: testNow}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("InsertCook(missing recipe) = %v, want ErrNotFound", err)
	}
	first, err := db.InsertCook(ctx, domain.Cook{RecipeID: pasta.ID, CookedBy: 111, CookedAt: testNow.Add(time.Hour),
		Ratings: []domain.Rating{rating(111, 5, testNow.Add(time.Hour))}})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || first.RecipeID != pasta.ID || len(first.Ratings) != 1 || first.Ratings[0].Stars != 5 {
		t.Fatalf("InsertCook = %+v", first)
	}
	second, err := db.InsertCook(ctx, domain.Cook{RecipeID: pasta.ID, CookedBy: 222, CookedAt: testNow.Add(2 * time.Hour)})
	if err != nil || second.Ratings == nil || len(second.Ratings) != 0 {
		t.Fatalf("cook without rating = %+v, %v (ratings must be empty, not nil)", second, err)
	}
	soupCook, err := db.InsertCook(ctx, domain.Cook{RecipeID: soup.ID, CookedBy: 111, CookedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}

	// Anya rates the first cooking, then changes her mind: one rating each.
	if _, err := db.UpsertRating(ctx, pasta.ID, first.ID, rating(222, 2, testNow.Add(3*time.Hour))); err != nil {
		t.Fatal(err)
	}
	c, err := db.UpsertRating(ctx, pasta.ID, first.ID, domain.Rating{UserID: 222, Stars: 4, Comment: "Вкусно", RatedAt: testNow.Add(4 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Ratings) != 2 || c.Ratings[0].UserID != 111 || c.Ratings[1].UserID != 222 || c.Ratings[1].Stars != 4 || c.Ratings[1].Comment != "Вкусно" {
		t.Errorf("ratings after re-rating = %+v", c.Ratings)
	}
	if _, err := db.UpsertRating(ctx, pasta.ID, second.ID, rating(222, 3, testNow.Add(5*time.Hour))); err != nil {
		t.Fatal(err)
	}

	// A cook of another recipe or a missing cook cannot be rated through pasta.
	if _, err := db.UpsertRating(ctx, pasta.ID, soupCook.ID, rating(222, 1, testNow)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rating a cook of another recipe = %v, want ErrNotFound", err)
	}
	if _, err := db.UpsertRating(ctx, pasta.ID, 9999, rating(222, 1, testNow)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rating a missing cook = %v, want ErrNotFound", err)
	}
	if got, _ := db.ListCooks(ctx, soup.ID); len(got) != 1 || len(got[0].Ratings) != 0 {
		t.Errorf("the rejected rating leaked into soup: %+v", got)
	}

	// The summary covers every rating of every cooking, in get and list.
	want := domain.CookingSummary{Count: 2, RatingSum: 5 + 4 + 3, RatingCount: 3}
	lastCooked := testNow.Add(2 * time.Hour)
	got, err := db.GetRecipe(ctx, pasta.ID)
	if err != nil {
		t.Fatal(err)
	}
	list, err := db.ListRecipes(ctx, domain.RecipeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []domain.CookingSummary{got.Cooking, list[1].Cooking} {
		if s.Count != want.Count || s.RatingSum != want.RatingSum || s.RatingCount != want.RatingCount ||
			s.LastCookedAt == nil || !s.LastCookedAt.Equal(lastCooked) {
			t.Errorf("summary = %+v, want %+v last %v", s, want, lastCooked)
		}
		if avg, ok := s.AverageTenths(); !ok || avg != 40 {
			t.Errorf("average = %d, %v; want 4.0", avg, ok)
		}
	}
	if list[0].ID != soup.ID || list[0].Cooking.Count != 1 || list[0].Cooking.RatingCount != 0 {
		t.Errorf("soup summary = %+v", list[0].Cooking)
	}

	cooks, err := db.ListCooks(ctx, pasta.ID)
	if err != nil || len(cooks) != 2 || cooks[0].ID != second.ID || cooks[1].ID != first.ID {
		t.Fatalf("ListCooks = %+v, %v; want newest first", cooks, err)
	}
	if len(cooks[1].Ratings) != 2 || len(cooks[0].Ratings) != 1 {
		t.Errorf("ratings per cook = %d, %d", len(cooks[1].Ratings), len(cooks[0].Ratings))
	}
	if _, err := db.ListCooks(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ListCooks(missing recipe) = %v, want ErrNotFound", err)
	}
	if n, err := db.CountCooks(ctx); err != nil || n != 3 {
		t.Errorf("CountCooks = %d, %v; want 3", n, err)
	}

	// Removing a cooking takes its ratings; only through its own recipe.
	if err := db.DeleteCook(ctx, soup.ID, first.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("DeleteCook via another recipe = %v, want ErrNotFound", err)
	}
	if err := db.DeleteCook(ctx, pasta.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, db, "recipe_ratings"); n != 1 {
		t.Errorf("%d ratings left, want 1 (of the second cooking)", n)
	}
	if got, _ := db.GetRecipe(ctx, pasta.ID); got.Cooking.Count != 1 || got.Cooking.RatingSum != 3 {
		t.Errorf("summary after removing a cooking = %+v", got.Cooking)
	}

	// Deleting the recipe removes its history.
	if _, err := db.DeleteRecipe(ctx, pasta.ID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, db, "recipe_cooks"); n != 1 {
		t.Errorf("%d cooks left, want only the soup's", n)
	}
	if n := countRows(t, db, "recipe_ratings"); n != 0 {
		t.Errorf("%d ratings survived the recipe", n)
	}
}

func TestSavingsStorage(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	price, _ := domain.NewMoney(4_500_000, "RUB")
	sofa := mustWish(t, db, domain.WishDraft{Title: "Диван", Price: &price}, testNow)
	bike := mustWish(t, db, domain.WishDraft{Title: "Велосипед"}, testNow.Add(time.Minute))
	add := func(id domain.WishID, minor int64, user domain.UserID, at time.Time) (domain.Wish, domain.Saving, error) {
		return db.InsertSaving(ctx, id, func(w *domain.Wish) (domain.Saving, error) {
			return domain.NewSaving(*w, domain.Money{Minor: minor, Currency: "RUB"}, user, "с зарплаты", at)
		})
	}

	w, s1, err := add(sofa.ID, 500_000, 111, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if s1.ID == 0 || s1.WishID != sofa.ID || w.Saved == nil || *w.Saved != (domain.Money{Minor: 500_000, Currency: "RUB"}) {
		t.Fatalf("InsertSaving = %+v, %+v", w, s1)
	}
	_, s2, err := add(sofa.ID, 1_200_000, 222, testNow.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	lamp := mustWish(t, db, domain.WishDraft{Title: "Лампа"}, testNow.Add(2*time.Minute))
	if _, _, err := add(lamp.ID, 77_700, 111, testNow.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, _ := db.GetWish(ctx, sofa.ID)
	if got.Saved == nil || got.Saved.Minor != 1_700_000 {
		t.Errorf("GetWish Saved = %+v, want 17 000 ₽", got.Saved)
	}
	list, _ := db.ListWishes(ctx, domain.WishFilter{})
	for _, x := range list {
		switch x.ID {
		case sofa.ID:
			if x.Saved == nil || x.Saved.Minor != 1_700_000 {
				t.Errorf("ListWishes Saved of the sofa = %+v", x.Saved)
			}
		case lamp.ID:
			if x.Saved == nil || x.Saved.Minor != 77_700 {
				t.Errorf("ListWishes Saved of the lamp = %+v", x.Saved)
			}
		case bike.ID:
			if x.Saved != nil {
				t.Errorf("a wish without savings has Saved %+v", x.Saved)
			}
		}
	}
	if len(list) != 3 {
		t.Errorf("ListWishes returned %d wishes, want 3", len(list))
	}

	savings, err := db.ListSavings(ctx, sofa.ID)
	if err != nil || len(savings) != 2 || savings[0].ID != s2.ID || savings[1].ID != s1.ID {
		t.Fatalf("ListSavings = %+v, %v; want newest first", savings, err)
	}
	if s := savings[1]; s.UserID != 111 || s.Note != "с зарплаты" || !s.CreatedAt.Equal(testNow.Add(time.Hour)) || s.Amount.Currency != "RUB" {
		t.Errorf("stored saving = %+v", s)
	}
	if empty, err := db.ListSavings(ctx, bike.ID); err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("ListSavings(no savings) = %v, %v; want empty", empty, err)
	}
	if _, err := db.ListSavings(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ListSavings(missing wish) = %v, want ErrNotFound", err)
	}

	// fn sees the stored total; its error rolls everything back.
	boom := errors.New("boom")
	if _, _, err := db.InsertSaving(ctx, sofa.ID, func(w *domain.Wish) (domain.Saving, error) {
		if w.Saved == nil || w.Saved.Minor != 1_700_000 {
			t.Errorf("fn saw Saved = %+v", w.Saved)
		}
		w.Title = "changed"
		return domain.Saving{}, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("InsertSaving error = %v, want boom", err)
	}
	if got, _ := db.GetWish(ctx, sofa.ID); got.Title != "Диван" {
		t.Error("failed saving changed the wish")
	}
	if _, _, err := add(9999, 100, 111, testNow); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("InsertSaving(missing wish) = %v, want ErrNotFound", err)
	}

	// A saving is removed only through its own wish.
	if err := db.DeleteSaving(ctx, bike.ID, s1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("DeleteSaving via another wish = %v, want ErrNotFound", err)
	}
	if err := db.DeleteSaving(ctx, sofa.ID, s1.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetWish(ctx, sofa.ID); got.Saved == nil || got.Saved.Minor != 1_200_000 {
		t.Errorf("Saved after removing a saving = %+v", got.Saved)
	}
	if _, err := db.DeleteWish(ctx, sofa.ID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, db, "wish_savings"); n != 1 {
		t.Errorf("%d savings left after deleting the sofa, want only the lamp's", n)
	}
}

func TestShoppingStorage(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	recipe := mustRecipe(t, db, domain.RecipeDraft{Title: "Блины"}, testNow)
	at := testNow
	item := func(name string, q *domain.Quantity, recipeID *domain.RecipeID) domain.ShoppingItem {
		at = at.Add(time.Second)
		it, err := domain.NewShoppingItem(domain.ShoppingDraft{Name: name, Quantity: q, RecipeID: recipeID}, 111, at)
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	unitOf := func(q *domain.Quantity) domain.Unit {
		if q == nil {
			return ""
		}
		return q.Unit
	}
	// merge also checks what storage offers: unchecked items with the same
	// folded name and the same or the related unit only.
	merge := func(stored *domain.ShoppingItem, added domain.ShoppingItem) bool {
		if stored.Checked || !strings.EqualFold(stored.Name, added.Name) || unitOf(stored.Quantity).Related() != unitOf(added.Quantity) &&
			unitOf(stored.Quantity) != unitOf(added.Quantity) {
			t.Errorf("offered %q %s for %q %s", stored.Name, qs(stored.Quantity), added.Name, qs(added.Quantity))
		}
		return stored.MergeInto(added.Quantity, added.CreatedAt)
	}
	add := func(limit int, items ...domain.ShoppingItem) ([]domain.ShoppingItem, error) {
		return db.AddShoppingItems(ctx, items, limit, merge)
	}

	first, err := add(10, item("Молоко", qty(t, "500", "мл"), &recipe.ID), item("Хлеб", nil, nil))
	if err != nil || len(first) != 2 || first[0].ID == 0 || *first[0].RecipeID != recipe.ID {
		t.Fatalf("AddShoppingItems = %+v, %v", first, err)
	}
	milk := first[0]

	// Same folded name and unit merges; items of one call merge together.
	merged, err := add(10, item("молоко", qty(t, "250", "мл"), nil), item("Яйца", qty(t, "2", "шт"), nil), item("ЯЙЦА", qty(t, "3", "шт"), nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 || merged[0].ID != milk.ID || merged[0].Name != "Молоко" || qs(merged[0].Quantity) != "750 мл" ||
		qs(merged[1].Quantity) != "5 шт" {
		t.Fatalf("merged = %+v (%s, %s)", merged, qs(merged[0].Quantity), qs(merged[len(merged)-1].Quantity))
	}
	eggs := merged[1]

	// A related unit merges (750 мл + 0,25 л = 1 л); an unrelated one and a
	// checked item never absorb anything.
	liter, err := add(10, item("Молоко", qty(t, "0,25", "л"), nil))
	if err != nil || liter[0].ID != milk.ID || qs(liter[0].Quantity) != "1 л" {
		t.Fatalf("related unit did not merge: %+v, %v", liter, err)
	}
	pack, err := add(10, item("Молоко", qty(t, "1", "упаковка"), nil))
	if err != nil || pack[0].ID == milk.ID {
		t.Fatalf("different unit merged: %+v, %v", pack, err)
	}
	if _, err := db.ModifyShoppingItem(ctx, eggs.ID, func(it *domain.ShoppingItem) error {
		return it.Apply(domain.ShoppingPatch{Checked: domain.Some(true)}, testNow)
	}); err != nil {
		t.Fatal(err)
	}
	// Storage itself never offers a checked item, whatever merge would say.
	fresh, err := db.AddShoppingItems(ctx, []domain.ShoppingItem{item("Яйца", qty(t, "1", "шт"), nil)}, 10,
		func(stored *domain.ShoppingItem, _ domain.ShoppingItem) bool {
			t.Errorf("checked item %d offered as a merge candidate", stored.ID)
			return true
		})
	if err != nil || fresh[0].ID == eggs.ID || qs(fresh[0].Quantity) != "1 шт" {
		t.Fatalf("checked item absorbed a new one: %+v, %v", fresh, err)
	}

	items, err := db.ListShoppingItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, it := range items {
		line := it.Name
		if it.Quantity != nil {
			line += " " + qs(it.Quantity)
		}
		if it.Checked {
			line += " ✓"
		}
		names = append(names, line)
	}
	if want := []string{"Молоко 1 л", "Хлеб", "Молоко 1 упаковка", "Яйца 1 шт", "Яйца 5 шт ✓"}; !slices.Equal(names, want) {
		t.Errorf("list order = %q, want %q", names, want)
	}

	// The limit counts every row, applies to inserts only and rolls back the
	// whole call.
	if _, err := add(5, item("Сахар", nil, nil)); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("insert over the limit = %v, want ErrLimitExceeded", err)
	}
	if _, err := add(5, item("Молоко", qty(t, "100", "мл"), nil), item("Соль", nil, nil)); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("batch over the limit = %v, want ErrLimitExceeded", err)
	}
	if got, _ := db.ListShoppingItems(ctx); qs(got[0].Quantity) != "1 л" || len(got) != 5 {
		t.Errorf("a failed batch left changes: %+v", got)
	}
	if atLimit, err := add(5, item("Молоко", qty(t, "50", "мл"), nil)); err != nil || qs(atLimit[0].Quantity) != "1.05 л" {
		t.Errorf("merging into a full list = %+v, %v", atLimit, err)
	}

	missing := domain.RecipeID(9999)
	if _, err := add(10, item("Мука", nil, &missing)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("item of a missing recipe = %v, want ErrNotFound", err)
	}

	// Renaming updates the merge key.
	bread := first[1]
	if _, err := db.ModifyShoppingItem(ctx, bread.ID, func(it *domain.ShoppingItem) error {
		return it.Apply(domain.ShoppingPatch{Name: domain.Some("Батон")}, testNow)
	}); err != nil {
		t.Fatal(err)
	}
	if again, err := add(10, item("батон", nil, nil)); err != nil || again[0].ID != bread.ID {
		t.Errorf("renamed item did not merge: %+v, %v", again, err)
	}
	if _, err := db.ModifyShoppingItem(ctx, 9999, func(*domain.ShoppingItem) error { return nil }); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ModifyShoppingItem(missing) = %v, want ErrNotFound", err)
	}

	// Deleting the recipe keeps its items.
	if _, err := db.DeleteRecipe(ctx, recipe.ID); err != nil {
		t.Fatal(err)
	}
	items, _ = db.ListShoppingItems(ctx)
	if items[0].ID != milk.ID || items[0].RecipeID != nil {
		t.Errorf("item of a deleted recipe = %+v", items[0])
	}

	if n, err := db.DeleteCheckedShoppingItems(ctx); err != nil || n != 1 {
		t.Errorf("DeleteCheckedShoppingItems = %d, %v; want 1", n, err)
	}
	if err := db.DeleteShoppingItem(ctx, eggs.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("DeleteShoppingItem(cleared) = %v, want ErrNotFound", err)
	}
	if err := db.DeleteShoppingItem(ctx, milk.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.ListShoppingItems(ctx); len(got) != 3 || slices.ContainsFunc(got, func(it domain.ShoppingItem) bool {
		return it.Checked || strings.EqualFold(it.Name, "молоко") && it.Quantity.Unit == domain.UnitMilliliter
	}) {
		t.Errorf("list after deletes = %+v", got)
	}
}

// Two partners saving at the same moment must not both pass the
// one-currency rule for a wish without a price.
func TestConcurrentFirstSavingsKeepOneCurrency(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	w := mustWish(t, db, domain.WishDraft{Title: "Без цены"}, testNow)
	currencies := []domain.Currency{"RUB", "EUR", "USD", "GBP", "RUB", "EUR", "USD", "GBP"}
	errs := make(chan error, len(currencies))
	for _, c := range currencies {
		go func() {
			_, _, err := db.InsertSaving(ctx, w.ID, func(w *domain.Wish) (domain.Saving, error) {
				return domain.NewSaving(*w, domain.Money{Minor: 100, Currency: c}, 111, "", testNow)
			})
			errs <- err
		}()
	}
	for range currencies {
		if err := <-errs; err != nil {
			if _, ok := domain.AsValidation(err); !ok {
				t.Errorf("unexpected error (busy database?): %v", err)
			}
		}
	}
	var distinct int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(DISTINCT currency) FROM wish_savings WHERE wish_id = ?`, int64(w.ID)).Scan(&distinct); err != nil {
		t.Fatal(err)
	}
	if distinct != 1 {
		t.Errorf("savings of one wish in %d currencies", distinct)
	}
}

func TestPerEntityCaps(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	fill := func(query string, n int, args ...any) {
		t.Helper()
		if _, err := db.sql.ExecContext(ctx, `WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < ?) `+query,
			append([]any{n}, args...)...); err != nil {
			t.Fatal(err)
		}
	}

	// Tags: the cap is per kind, counting the seeded defaults.
	seeded := 0
	for _, tag := range domain.DefaultRecipeTags() {
		if tag.Kind == domain.TagCuisine {
			seeded++
		}
	}
	fill(`INSERT INTO recipe_tags (kind, name, name_key, emoji, position, created_at)
		SELECT 'cuisine', 'Кухня ' || i, 'кухня ' || i, '', 100 + i, 0 FROM seq`, domain.MaxRecipeTagsPerKind-seeded)
	extra, _ := domain.NewRecipeTag(domain.TagCuisine, "Лишняя", "🍽", testNow)
	if _, err := db.InsertRecipeTag(ctx, extra); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("tag over the cap = %v, want ErrLimitExceeded", err)
	}
	course, _ := domain.NewRecipeTag(domain.TagCourse, "Полдник", "🍪", testNow)
	if _, err := db.InsertRecipeTag(ctx, course); err != nil {
		t.Fatalf("the cap counts each kind separately: %v", err)
	}

	// Savings and cooks.
	price := domain.Money{Minor: 1_000_000, Currency: "RUB"}
	wish := mustWish(t, db, domain.WishDraft{Title: "Диван", Price: &price}, testNow)
	fill(`INSERT INTO wish_savings (wish_id, amount_minor, currency, user_id, note, created_at)
		SELECT ?, 100, 'RUB', 111, '', 0 FROM seq`, domain.MaxSavingsPerWish, wish.ID)
	if _, _, err := db.InsertSaving(ctx, wish.ID, func(w *domain.Wish) (domain.Saving, error) {
		return domain.NewSaving(*w, domain.Money{Minor: 100, Currency: "RUB"}, 111, "", testNow)
	}); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("saving over the cap = %v, want ErrLimitExceeded", err)
	}
	recipe := mustRecipe(t, db, domain.RecipeDraft{Title: "Борщ"}, testNow)
	fill(`INSERT INTO recipe_cooks (recipe_id, cooked_by, cooked_at) SELECT ?, 111, 0 FROM seq`, domain.MaxCooksPerRecipe, recipe.ID)
	if _, err := db.InsertCook(ctx, domain.Cook{RecipeID: recipe.ID, CookedBy: 111, CookedAt: testNow}); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("cook over the cap = %v, want ErrLimitExceeded", err)
	}
}
