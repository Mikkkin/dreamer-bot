package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const recipeTagColumns = `id, kind, name, emoji, position, created_at`

func scanRecipeTag(s scanner) (domain.RecipeTag, error) {
	var (
		t       domain.RecipeTag
		created int64
	)
	if err := s.Scan(&t.ID, &t.Kind, &t.Name, &t.Emoji, &t.Position, &created); err != nil {
		return domain.RecipeTag{}, err
	}
	t.CreatedAt = fromMillis(created)
	return t, nil
}

// ListRecipeTags returns every tag: cuisines first, then courses, each in
// display order.
func (db *DB) ListRecipeTags(ctx context.Context) ([]domain.RecipeTag, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+recipeTagColumns+` FROM recipe_tags
		ORDER BY CASE kind WHEN 'cuisine' THEN 0 ELSE 1 END, position, id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list recipe tags: %w", err)
	}
	defer rows.Close()
	var out []domain.RecipeTag
	for rows.Next() {
		t, err := scanRecipeTag(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan recipe tag: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list recipe tags: %w", err)
	}
	return out, nil
}

// InsertRecipeTag appends t to the display order of its kind, or returns
// domain.ErrLimitExceeded when the kind already has MaxRecipeTagsPerKind tags.
func (db *DB) InsertRecipeTag(ctx context.Context, t domain.RecipeTag) (domain.RecipeTag, error) {
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM recipe_tags WHERE kind = ?`, string(t.Kind)).Scan(&count); err != nil {
			return fmt.Errorf("sqlite: count recipe tags: %w", err)
		}
		if count >= domain.MaxRecipeTagsPerKind {
			return fmt.Errorf("%d %s tags: %w", count, t.Kind, domain.ErrLimitExceeded)
		}
		return tx.QueryRowContext(ctx, `
		INSERT INTO recipe_tags (kind, name, name_key, emoji, position, created_at)
		VALUES (?1, ?2, ?3, ?4, (SELECT COALESCE(MAX(position), -1) + 1 FROM recipe_tags WHERE kind = ?1), ?5)
		RETURNING id, position`,
			string(t.Kind), t.Name, searchKey(t.Name), t.Emoji, toMillis(t.CreatedAt),
		).Scan(&t.ID, &t.Position)
	})
	if err != nil {
		if errors.Is(err, domain.ErrLimitExceeded) {
			return domain.RecipeTag{}, err
		}
		if isUniqueViolation(err) {
			return domain.RecipeTag{}, fmt.Errorf("recipe tag name: %w", domain.ErrConflict)
		}
		return domain.RecipeTag{}, fmt.Errorf("sqlite: insert recipe tag: %w", err)
	}
	return t, nil
}

// ModifyRecipeTag loads a tag, lets fn change it and saves the name and the
// emoji in one transaction. The kind and the position never change.
func (db *DB) ModifyRecipeTag(ctx context.Context, id domain.RecipeTagID, fn func(*domain.RecipeTag) error) (domain.RecipeTag, error) {
	var t domain.RecipeTag
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		t, err = scanRecipeTag(tx.QueryRowContext(ctx, `SELECT `+recipeTagColumns+` FROM recipe_tags WHERE id = ?`, id))
		if err != nil {
			return notFound(err, "recipe tag", int64(id))
		}
		kind, position := t.Kind, t.Position
		if err := fn(&t); err != nil {
			return err
		}
		t.ID, t.Kind, t.Position = id, kind, position
		res, err := tx.ExecContext(ctx, `UPDATE recipe_tags SET name = ?, name_key = ?, emoji = ? WHERE id = ?`,
			t.Name, searchKey(t.Name), t.Emoji, id)
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("recipe tag name: %w", domain.ErrConflict)
			}
			return fmt.Errorf("sqlite: update recipe tag %d: %w", id, err)
		}
		return requireAffected(res, "recipe tag", int64(id))
	})
	if err != nil {
		return domain.RecipeTag{}, err
	}
	return t, nil
}

// DeleteRecipeTag removes a tag. The foreign keys clear the cuisine of its
// recipes and drop it from their courses; the recipes themselves stay.
func (db *DB) DeleteRecipeTag(ctx context.Context, id domain.RecipeTagID) error {
	res, err := db.sql.ExecContext(ctx, `DELETE FROM recipe_tags WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete recipe tag %d: %w", id, err)
	}
	return requireAffected(res, "recipe tag", int64(id))
}
