package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const categoryColumns = `id, name, emoji, position, created_at`

func scanCategory(s scanner) (domain.Category, error) {
	var (
		c       domain.Category
		created int64
	)
	if err := s.Scan(&c.ID, &c.Name, &c.Emoji, &c.Position, &created); err != nil {
		return domain.Category{}, err
	}
	c.CreatedAt = fromMillis(created)
	return c, nil
}

// ListCategories returns every category in display order.
func (db *DB) ListCategories(ctx context.Context) ([]domain.Category, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+categoryColumns+` FROM categories ORDER BY position, id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list categories: %w", err)
	}
	defer rows.Close()
	var out []domain.Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan category: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list categories: %w", err)
	}
	return out, nil
}

// GetCategory returns one category.
func (db *DB) GetCategory(ctx context.Context, id domain.CategoryID) (domain.Category, error) {
	c, err := scanCategory(db.sql.QueryRowContext(ctx, `SELECT `+categoryColumns+` FROM categories WHERE id = ?`, id))
	if err != nil {
		return domain.Category{}, notFound(err, "category", int64(id))
	}
	return c, nil
}

// InsertCategory appends c to the display order.
func (db *DB) InsertCategory(ctx context.Context, c domain.Category) (domain.Category, error) {
	err := db.sql.QueryRowContext(ctx, `
		INSERT INTO categories (name, name_key, emoji, position, created_at)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM categories), ?)
		RETURNING id, position`,
		c.Name, searchKey(c.Name), c.Emoji, toMillis(c.CreatedAt),
	).Scan(&c.ID, &c.Position)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Category{}, fmt.Errorf("category name: %w", domain.ErrConflict)
		}
		return domain.Category{}, fmt.Errorf("sqlite: insert category: %w", err)
	}
	return c, nil
}

// ModifyCategory loads a category, lets fn change it and saves the name and
// the emoji, all in one IMMEDIATE transaction, so concurrent partial edits
// (one partner renames, the other changes the emoji) cannot lose each other.
func (db *DB) ModifyCategory(ctx context.Context, id domain.CategoryID, fn func(*domain.Category) error) (domain.Category, error) {
	var c domain.Category
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		c, err = scanCategory(tx.QueryRowContext(ctx, `SELECT `+categoryColumns+` FROM categories WHERE id = ?`, id))
		if err != nil {
			return notFound(err, "category", int64(id))
		}
		if err := fn(&c); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE categories SET name = ?, name_key = ?, emoji = ? WHERE id = ?`,
			c.Name, searchKey(c.Name), c.Emoji, id)
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("category name: %w", domain.ErrConflict)
			}
			return fmt.Errorf("sqlite: update category %d: %w", id, err)
		}
		return requireAffected(res, "category", int64(id))
	})
	if err != nil {
		return domain.Category{}, err
	}
	return c, nil
}

// DeleteCategory removes a category; the foreign key turns the category of
// its wishes into NULL.
func (db *DB) DeleteCategory(ctx context.Context, id domain.CategoryID) error {
	res, err := db.sql.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete category %d: %w", id, err)
	}
	return requireAffected(res, "category", int64(id))
}
