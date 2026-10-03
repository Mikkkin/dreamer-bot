package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Before servings became a field of the recipe, recipes.servings was
// written only together with the КБЖУ. Such rows must come back with the
// same servings, now on the recipe as well.
func TestStoredServingsSurvive(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	res, err := db.sql.ExecContext(ctx, `
		INSERT INTO recipes (title, title_key, body, body_key, author_id, created_at, updated_at,
			kcal_tenths, protein_tenths, fat_tenths, carbs_tenths, weight_g, servings)
		VALUES ('Плов', 'плов', '', '', 111, ?1, ?1, 1500, 125, 60, 104, 800, 4)`, toMillis(testNow))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	got, err := db.GetRecipe(ctx, domain.RecipeID(id))
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Nutrition{KcalPer100: 1500, ProteinPer100: 125, FatPer100: 60, CarbsPer100: 104, WeightGrams: 800, Servings: 4}
	if got.Servings == nil || *got.Servings != 4 || got.Nutrition == nil || *got.Nutrition != want {
		t.Fatalf("servings = %v, nutrition = %+v", got.Servings, got.Nutrition)
	}
	if per, ok := got.Nutrition.PerServing(); !ok || per.Kcal != 3000 {
		t.Errorf("per serving = %+v, %v", per, ok)
	}

	// An edit of anything else keeps them, and so does removing the КБЖУ.
	if _, err := db.ModifyRecipe(ctx, got.ID, func(r *domain.Recipe) error {
		return r.Apply(domain.RecipePatch{Title: domain.Some("Плов узбекский"), Nutrition: domain.Some[*domain.Nutrition](nil)}, testNow.Add(time.Hour))
	}); err != nil {
		t.Fatal(err)
	}
	if n := servingsColumn(t, db, got.ID); !n.Valid || n.V != 4 {
		t.Errorf("servings column after removing the КБЖУ = %+v", n)
	}
	list, err := db.ListRecipes(ctx, domain.RecipeFilter{})
	if err != nil || len(list) != 1 || list[0].Servings == nil || *list[0].Servings != 4 || list[0].Nutrition != nil {
		t.Fatalf("listed = %+v, %v", list, err)
	}
}

func TestServingsWithoutNutrition(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	three := 3
	r := mustRecipe(t, db, domain.RecipeDraft{Title: "Сырники", Servings: &three}, testNow)
	if r.Servings == nil || *r.Servings != 3 || r.Nutrition != nil {
		t.Fatalf("inserted = %v, %+v", r.Servings, r.Nutrition)
	}
	got, err := db.GetRecipe(ctx, r.ID)
	if err != nil || got.Servings == nil || *got.Servings != 3 || got.Nutrition != nil {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	random, err := db.RandomRecipe(ctx)
	if err != nil || random.Servings == nil || *random.Servings != 3 {
		t.Fatalf("random = %+v, %v", random, err)
	}

	// КБЖУ added later takes the recipe's servings.
	per100, _ := domain.NewNutrition("220", "15", "9", "20", 600, 0)
	updated, err := db.ModifyRecipe(ctx, r.ID, func(r *domain.Recipe) error {
		return r.Apply(domain.RecipePatch{Nutrition: domain.Some(&per100)}, testNow.Add(time.Hour))
	})
	if err != nil || updated.Nutrition == nil || updated.Nutrition.Servings != 3 {
		t.Fatalf("updated = %+v, %v", updated.Nutrition, err)
	}
	if got, _ := db.GetRecipe(ctx, r.ID); got.Nutrition == nil || got.Nutrition.Servings != 3 || got.Nutrition.WeightGrams != 600 {
		t.Errorf("stored nutrition = %+v", got.Nutrition)
	}

	// null clears them: the column goes back to NULL.
	cleared, err := db.ModifyRecipe(ctx, r.ID, func(r *domain.Recipe) error {
		return r.Apply(domain.RecipePatch{Servings: domain.Some[*int](nil)}, testNow.Add(2*time.Hour))
	})
	if err != nil || cleared.Servings != nil || cleared.Nutrition.Servings != 0 {
		t.Fatalf("cleared = %+v, %v", cleared, err)
	}
	if n := servingsColumn(t, db, r.ID); n.Valid {
		t.Errorf("servings column = %+v, want NULL", n)
	}
	if got, _ := db.GetRecipe(ctx, r.ID); got.Servings != nil || got.Nutrition == nil || got.Nutrition.Servings != 0 {
		t.Errorf("stored after clearing = %v, %+v", got.Servings, got.Nutrition)
	}
}

func servingsColumn(t *testing.T, db *DB, id domain.RecipeID) sql.Null[int64] {
	t.Helper()
	var n sql.Null[int64]
	if err := db.sql.QueryRow(`SELECT servings FROM recipes WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
