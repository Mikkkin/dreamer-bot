package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// recipeColumns selects from `recipes r`. The last four columns are the
// cooking summary (number of cooks, last cooking, sum and number of all
// ratings), aggregated in the same statement so that listing recipes never
// runs a query per recipe.
const recipeColumns = `r.id, r.title, r.link, r.body, r.author_id, r.created_at, r.updated_at,
	r.cuisine_id, r.kcal_tenths, r.protein_tenths, r.fat_tenths, r.carbs_tenths, r.weight_g, r.servings,
	(SELECT COUNT(*) FROM recipe_cooks k WHERE k.recipe_id = r.id),
	(SELECT MAX(k.cooked_at) FROM recipe_cooks k WHERE k.recipe_id = r.id),
	(SELECT COALESCE(SUM(g.stars), 0) FROM recipe_cooks k JOIN recipe_ratings g ON g.cook_id = k.id WHERE k.recipe_id = r.id),
	(SELECT COUNT(*) FROM recipe_cooks k JOIN recipe_ratings g ON g.cook_id = k.id WHERE k.recipe_id = r.id)`

// listRecipesQuery is static: every filter is a parameter that disables
// itself when unset (?1 folded query matched against the title or the body,
// ?2 cuisine tag, ?3 course tag).
const listRecipesQuery = `SELECT ` + recipeColumns + ` FROM recipes r
	WHERE (?1 = '' OR instr(r.title_key, ?1) > 0 OR instr(r.body_key, ?1) > 0)
	  AND (?2 = 0 OR r.cuisine_id = ?2)
	  AND (?3 = 0 OR EXISTS (SELECT 1 FROM recipe_courses c WHERE c.recipe_id = r.id AND c.tag_id = ?3))
	ORDER BY r.created_at DESC, r.id DESC`

const (
	recipeCoursesQuery = `SELECT recipe_id, tag_id FROM recipe_courses
		WHERE recipe_id IN (SELECT value FROM json_each(?)) ORDER BY recipe_id, position`
	recipeIngredientsQuery = `SELECT recipe_id, name, qty_hundredths, unit FROM recipe_ingredients
		WHERE recipe_id IN (SELECT value FROM json_each(?)) ORDER BY recipe_id, position`
)

func scanRecipe(s scanner) (domain.Recipe, error) {
	var (
		r                         domain.Recipe
		link                      sql.Null[string]
		cuisine                   sql.Null[domain.RecipeTagID]
		kcal, protein, fat, carbs sql.Null[int]
		weight, servings          sql.Null[int]
		lastCooked                sql.Null[int64]
		created, updated          int64
	)
	if err := s.Scan(&r.ID, &r.Title, &link, &r.Body, &r.AuthorID, &created, &updated,
		&cuisine, &kcal, &protein, &fat, &carbs, &weight, &servings,
		&r.Cooking.Count, &lastCooked, &r.Cooking.RatingSum, &r.Cooking.RatingCount); err != nil {
		return domain.Recipe{}, err
	}
	r.Link = stringPtr(link)
	if cuisine.Valid {
		r.CuisineID = &cuisine.V
	}
	// The servings belong to the recipe; the КБЖУ mirrors them.
	if servings.Valid {
		r.Servings = &servings.V
	}
	// The schema sets the four values together; kcal stands for all of them.
	if kcal.Valid {
		r.Nutrition = &domain.Nutrition{
			KcalPer100:    kcal.V,
			ProteinPer100: protein.V,
			FatPer100:     fat.V,
			CarbsPer100:   carbs.V,
			WeightGrams:   weight.V,
			Servings:      servings.V,
		}
	}
	r.Cooking.LastCookedAt = timePtr(lastCooked)
	r.CreatedAt = fromMillis(created)
	r.UpdatedAt = fromMillis(updated)
	return r, nil
}

// recipeArgs returns the values of the mutable columns in the order
// title, title_key, link, body, body_key, cuisine_id, kcal_tenths,
// protein_tenths, fat_tenths, carbs_tenths, weight_g, servings, updated_at.
// servings is the recipe's, with or without КБЖУ; weight_g belongs to the
// КБЖУ.
func recipeArgs(r domain.Recipe) []any {
	var cuisine, kcal, protein, fat, carbs, weight, servings any
	if r.CuisineID != nil {
		cuisine = int64(*r.CuisineID)
	}
	if r.Servings != nil {
		servings = int64(*r.Servings)
	}
	if n := r.Nutrition; n != nil {
		kcal, protein, fat, carbs = int64(n.KcalPer100), int64(n.ProteinPer100), int64(n.FatPer100), int64(n.CarbsPer100)
		weight = nullableInt(n.WeightGrams)
	}
	return []any{
		r.Title, searchKey(r.Title), nullableString(r.Link), r.Body, searchKey(r.Body),
		cuisine, kcal, protein, fat, carbs, weight, servings, toMillis(r.UpdatedAt),
	}
}

// InsertRecipe stores a new recipe with its course tags and ingredients.
func (db *DB) InsertRecipe(ctx context.Context, r domain.Recipe) (domain.Recipe, error) {
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		args := append(recipeArgs(r), int64(r.AuthorID), toMillis(r.CreatedAt))
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO recipes (title, title_key, link, body, body_key, cuisine_id, kcal_tenths,
				protein_tenths, fat_tenths, carbs_tenths, weight_g, servings, updated_at, author_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			RETURNING id`, args...).Scan(&r.ID); err != nil {
			return fmt.Errorf("sqlite: insert recipe: %w", err)
		}
		return saveRecipeLists(ctx, tx, r)
	})
	if err != nil {
		return domain.Recipe{}, tagGone(err)
	}
	r.Images = nil
	r.Cooking = domain.CookingSummary{}
	r.CourseIDs = orEmpty(r.CourseIDs)
	r.Ingredients = orEmpty(r.Ingredients)
	return r, nil
}

// tagGone reports a write that referenced a tag deleted in the meantime
// (the service checks the tags right before writing) as a validation error
// instead of an internal one.
func tagGone(err error) error {
	if isForeignKeyViolation(err) {
		return domain.ErrRecipeTagGone
	}
	return err
}

// saveRecipeLists replaces the course tags and the ingredients of r with
// the ones it carries, keeping their order.
func saveRecipeLists(ctx context.Context, tx *sql.Tx, r domain.Recipe) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recipe_courses WHERE recipe_id = ?`, r.ID); err != nil {
		return fmt.Errorf("sqlite: clear courses of recipe %d: %w", r.ID, err)
	}
	for i, tag := range r.CourseIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recipe_courses (recipe_id, tag_id, position) VALUES (?, ?, ?)`,
			r.ID, tag, i); err != nil {
			return fmt.Errorf("sqlite: save courses of recipe %d: %w", r.ID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM recipe_ingredients WHERE recipe_id = ?`, r.ID); err != nil {
		return fmt.Errorf("sqlite: clear ingredients of recipe %d: %w", r.ID, err)
	}
	for i, ing := range r.Ingredients {
		hundredths, unit := quantityArgs(ing.Quantity)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO recipe_ingredients (recipe_id, position, name, qty_hundredths, unit) VALUES (?, ?, ?, ?, ?)`,
			r.ID, i, ing.Name, hundredths, unit); err != nil {
			return fmt.Errorf("sqlite: save ingredients of recipe %d: %w", r.ID, err)
		}
	}
	return nil
}

// GetRecipe returns one recipe with everything it carries.
func (db *DB) GetRecipe(ctx context.Context, id domain.RecipeID) (domain.Recipe, error) {
	return getRecipe(ctx, db.sql, id)
}

func getRecipe(ctx context.Context, q querier, id domain.RecipeID) (domain.Recipe, error) {
	r, err := scanRecipe(q.QueryRowContext(ctx, `SELECT `+recipeColumns+` FROM recipes r WHERE r.id = ?`, id))
	if err != nil {
		return domain.Recipe{}, notFound(err, "recipe", int64(id))
	}
	recipes := []domain.Recipe{r}
	if err := hydrateRecipes(ctx, q, recipes); err != nil {
		return domain.Recipe{}, err
	}
	return recipes[0], nil
}

// hydrateRecipes loads the images, course tags and ingredients of every
// recipe with one query each.
func hydrateRecipes(ctx context.Context, q querier, recipes []domain.Recipe) error {
	if len(recipes) == 0 {
		return nil
	}
	ids := make([]int64, len(recipes))
	for i, r := range recipes {
		ids[i] = int64(r.ID)
	}
	images, err := loadImages(ctx, q, recipeImagesQuery, ids)
	if err != nil {
		return err
	}
	courses, err := loadRecipeCourses(ctx, q, ids)
	if err != nil {
		return err
	}
	ingredients, err := loadRecipeIngredients(ctx, q, ids)
	if err != nil {
		return err
	}
	for i := range recipes {
		id := int64(recipes[i].ID)
		recipes[i].Images = images[id]
		recipes[i].CourseIDs = orEmpty(courses[id])
		recipes[i].Ingredients = orEmpty(ingredients[id])
	}
	return nil
}

func loadRecipeCourses(ctx context.Context, q querier, ids []int64) (map[int64][]domain.RecipeTagID, error) {
	list, err := idList(ids)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, recipeCoursesQuery, list)
	if err != nil {
		return nil, fmt.Errorf("sqlite: load recipe courses: %w", err)
	}
	defer rows.Close()
	out := make(map[int64][]domain.RecipeTagID, len(ids))
	for rows.Next() {
		var (
			recipe int64
			tag    domain.RecipeTagID
		)
		if err := rows.Scan(&recipe, &tag); err != nil {
			return nil, fmt.Errorf("sqlite: scan recipe course: %w", err)
		}
		out[recipe] = append(out[recipe], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: load recipe courses: %w", err)
	}
	return out, nil
}

func loadRecipeIngredients(ctx context.Context, q querier, ids []int64) (map[int64][]domain.Ingredient, error) {
	list, err := idList(ids)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, recipeIngredientsQuery, list)
	if err != nil {
		return nil, fmt.Errorf("sqlite: load recipe ingredients: %w", err)
	}
	defer rows.Close()
	out := make(map[int64][]domain.Ingredient, len(ids))
	for rows.Next() {
		var (
			recipe     int64
			ing        domain.Ingredient
			hundredths sql.Null[int64]
			unit       sql.Null[string]
		)
		if err := rows.Scan(&recipe, &ing.Name, &hundredths, &unit); err != nil {
			return nil, fmt.Errorf("sqlite: scan recipe ingredient: %w", err)
		}
		ing.Quantity = quantityFrom(hundredths, unit)
		out[recipe] = append(out[recipe], ing)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: load recipe ingredients: %w", err)
	}
	return out, nil
}

// quantityArgs returns the qty_hundredths and unit column values of q; an
// absent amount or unit is NULL.
func quantityArgs(q *domain.Quantity) (hundredths, unit any) {
	if q == nil {
		return nil, nil
	}
	if q.Unit != "" {
		unit = string(q.Unit)
	}
	return nullableInt(q.Hundredths), unit
}

// quantityFrom is the inverse of quantityArgs.
func quantityFrom(hundredths sql.Null[int64], unit sql.Null[string]) *domain.Quantity {
	if !hundredths.Valid && !unit.Valid {
		return nil
	}
	return &domain.Quantity{Hundredths: hundredths.V, Unit: domain.Unit(unit.V)}
}

// orEmpty turns a nil slice into an empty one, so that list fields always
// encode as [] rather than null.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ListRecipes returns the recipes matching f, newest first, with everything
// they carry.
func (db *DB) ListRecipes(ctx context.Context, f domain.RecipeFilter) ([]domain.Recipe, error) {
	rows, err := db.sql.QueryContext(ctx, listRecipesQuery, searchKey(f.Query), int64(f.CuisineID), int64(f.CourseID))
	if err != nil {
		return nil, fmt.Errorf("sqlite: list recipes: %w", err)
	}
	defer rows.Close()
	var recipes []domain.Recipe
	for rows.Next() {
		r, err := scanRecipe(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan recipe: %w", err)
		}
		recipes = append(recipes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list recipes: %w", err)
	}
	if err := hydrateRecipes(ctx, db.sql, recipes); err != nil {
		return nil, err
	}
	return recipes, nil
}

// RandomRecipe returns a random recipe with everything it carries.
func (db *DB) RandomRecipe(ctx context.Context) (domain.Recipe, error) {
	r, err := scanRecipe(db.sql.QueryRowContext(ctx, `SELECT `+recipeColumns+` FROM recipes r
		WHERE r.id = (SELECT id FROM recipes ORDER BY random() LIMIT 1)`))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Recipe{}, fmt.Errorf("no recipes: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Recipe{}, fmt.Errorf("sqlite: random recipe: %w", err)
	}
	recipes := []domain.Recipe{r}
	if err := hydrateRecipes(ctx, db.sql, recipes); err != nil {
		return domain.Recipe{}, err
	}
	return recipes[0], nil
}

// ModifyRecipe applies fn to the stored recipe inside a write transaction.
// The course tags and the ingredients are replaced by the ones fn leaves.
func (db *DB) ModifyRecipe(ctx context.Context, id domain.RecipeID, fn func(*domain.Recipe) error) (domain.Recipe, error) {
	var r domain.Recipe
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		if r, err = getRecipe(ctx, tx, id); err != nil {
			return err
		}
		if err := fn(&r); err != nil {
			return err
		}
		r.ID = id
		res, err := tx.ExecContext(ctx, `
			UPDATE recipes SET title = ?, title_key = ?, link = ?, body = ?, body_key = ?, cuisine_id = ?,
				kcal_tenths = ?, protein_tenths = ?, fat_tenths = ?, carbs_tenths = ?, weight_g = ?, servings = ?,
				updated_at = ?
			WHERE id = ?`, append(recipeArgs(r), int64(id))...)
		if err != nil {
			return fmt.Errorf("sqlite: update recipe %d: %w", id, err)
		}
		if err := requireAffected(res, "recipe", int64(id)); err != nil {
			return err
		}
		return saveRecipeLists(ctx, tx, r)
	})
	if err != nil {
		return domain.Recipe{}, tagGone(err)
	}
	r.CourseIDs = orEmpty(r.CourseIDs)
	r.Ingredients = orEmpty(r.Ingredients)
	return r, nil
}

// DeleteRecipe removes a recipe. Its images, course tags, ingredients and
// cooking history go with it through the foreign key cascade; shopping-list
// items added from it stay and forget it.
func (db *DB) DeleteRecipe(ctx context.Context, id domain.RecipeID) ([]string, error) {
	var keys []string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		if keys, err = imageKeys(ctx, tx, recipeImageKeysQuery, int64(id)); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM recipes WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("sqlite: delete recipe %d: %w", id, err)
		}
		return requireAffected(res, "recipe", int64(id))
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// CountRecipes returns the number of recipes.
func (db *DB) CountRecipes(ctx context.Context) (int, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM recipes`).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: count recipes: %w", err)
	}
	return n, nil
}
