package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const recipeColumns = `id, title, link, body, author_id, created_at, updated_at`

// listRecipesQuery matches the folded query (?1) against the title or the
// body; an empty query disables the filter.
const listRecipesQuery = `SELECT ` + recipeColumns + ` FROM recipes
	WHERE ?1 = '' OR instr(title_key, ?1) > 0 OR instr(body_key, ?1) > 0
	ORDER BY created_at DESC, id DESC`

func scanRecipe(s scanner) (domain.Recipe, error) {
	var (
		r                domain.Recipe
		link             sql.Null[string]
		created, updated int64
	)
	if err := s.Scan(&r.ID, &r.Title, &link, &r.Body, &r.AuthorID, &created, &updated); err != nil {
		return domain.Recipe{}, err
	}
	r.Link = stringPtr(link)
	r.CreatedAt = fromMillis(created)
	r.UpdatedAt = fromMillis(updated)
	return r, nil
}

// InsertRecipe stores a new recipe.
func (db *DB) InsertRecipe(ctx context.Context, r domain.Recipe) (domain.Recipe, error) {
	res, err := db.sql.ExecContext(ctx, `
		INSERT INTO recipes (title, title_key, link, body, body_key, author_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Title, searchKey(r.Title), nullableString(r.Link), r.Body, searchKey(r.Body),
		int64(r.AuthorID), toMillis(r.CreatedAt), toMillis(r.UpdatedAt))
	if err != nil {
		return domain.Recipe{}, fmt.Errorf("sqlite: insert recipe: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Recipe{}, fmt.Errorf("sqlite: insert recipe: %w", err)
	}
	r.ID = domain.RecipeID(id)
	r.Images = nil
	return r, nil
}

// GetRecipe returns one recipe with its images.
func (db *DB) GetRecipe(ctx context.Context, id domain.RecipeID) (domain.Recipe, error) {
	return getRecipe(ctx, db.sql, id)
}

func getRecipe(ctx context.Context, q querier, id domain.RecipeID) (domain.Recipe, error) {
	r, err := scanRecipe(q.QueryRowContext(ctx, `SELECT `+recipeColumns+` FROM recipes WHERE id = ?`, id))
	if err != nil {
		return domain.Recipe{}, notFound(err, "recipe", int64(id))
	}
	return withRecipeImages(ctx, q, r)
}

func withRecipeImages(ctx context.Context, q querier, r domain.Recipe) (domain.Recipe, error) {
	images, err := loadImages(ctx, q, recipeImagesQuery, []int64{int64(r.ID)})
	if err != nil {
		return domain.Recipe{}, err
	}
	r.Images = images[int64(r.ID)]
	return r, nil
}

// ListRecipes returns the recipes matching f, newest first, with their images.
func (db *DB) ListRecipes(ctx context.Context, f domain.RecipeFilter) ([]domain.Recipe, error) {
	rows, err := db.sql.QueryContext(ctx, listRecipesQuery, searchKey(f.Query))
	if err != nil {
		return nil, fmt.Errorf("sqlite: list recipes: %w", err)
	}
	defer rows.Close()
	var (
		recipes []domain.Recipe
		ids     []int64
	)
	for rows.Next() {
		r, err := scanRecipe(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan recipe: %w", err)
		}
		recipes = append(recipes, r)
		ids = append(ids, int64(r.ID))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list recipes: %w", err)
	}

	images, err := loadImages(ctx, db.sql, recipeImagesQuery, ids)
	if err != nil {
		return nil, err
	}
	for i := range recipes {
		recipes[i].Images = images[int64(recipes[i].ID)]
	}
	return recipes, nil
}

// RandomRecipe returns a random recipe with its images.
func (db *DB) RandomRecipe(ctx context.Context) (domain.Recipe, error) {
	r, err := scanRecipe(db.sql.QueryRowContext(ctx, `SELECT `+recipeColumns+` FROM recipes ORDER BY random() LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Recipe{}, fmt.Errorf("no recipes: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Recipe{}, fmt.Errorf("sqlite: random recipe: %w", err)
	}
	return withRecipeImages(ctx, db.sql, r)
}

// ModifyRecipe applies fn to the stored recipe inside a write transaction.
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
		res, err := tx.ExecContext(ctx, `
			UPDATE recipes SET title = ?, title_key = ?, link = ?, body = ?, body_key = ?, updated_at = ?
			WHERE id = ?`,
			r.Title, searchKey(r.Title), nullableString(r.Link), r.Body, searchKey(r.Body),
			toMillis(r.UpdatedAt), id)
		if err != nil {
			return fmt.Errorf("sqlite: update recipe %d: %w", id, err)
		}
		return requireAffected(res, "recipe", int64(id))
	})
	if err != nil {
		return domain.Recipe{}, err
	}
	return r, nil
}

// DeleteRecipe removes a recipe and, through the cascade, its image rows.
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
