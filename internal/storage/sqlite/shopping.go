package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const shoppingColumns = `id, name, qty_hundredths, unit, checked, recipe_id, added_by, created_at, updated_at`

// mergeCandidatesQuery finds the unchecked items an added item may merge
// into: same folded name and the same or the related unit (г↔кг, мл↔л;
// NULL-safe), oldest first.
const mergeCandidatesQuery = `SELECT ` + shoppingColumns + ` FROM shopping_items
	WHERE checked = 0 AND name_key = ? AND (unit IS ? OR unit IS ?) ORDER BY created_at, id`

func scanShoppingItem(s scanner) (domain.ShoppingItem, error) {
	var (
		it               domain.ShoppingItem
		hundredths       sql.Null[int64]
		unit             sql.Null[string]
		recipe           sql.Null[domain.RecipeID]
		created, updated int64
	)
	if err := s.Scan(&it.ID, &it.Name, &hundredths, &unit, &it.Checked, &recipe, &it.AddedBy, &created, &updated); err != nil {
		return domain.ShoppingItem{}, err
	}
	it.Quantity = quantityFrom(hundredths, unit)
	if recipe.Valid {
		it.RecipeID = &recipe.V
	}
	it.CreatedAt = fromMillis(created)
	it.UpdatedAt = fromMillis(updated)
	return it, nil
}

func queryShoppingItems(ctx context.Context, q querier, query string, args ...any) ([]domain.ShoppingItem, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list shopping items: %w", err)
	}
	defer rows.Close()
	out := []domain.ShoppingItem{}
	for rows.Next() {
		it, err := scanShoppingItem(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan shopping item: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list shopping items: %w", err)
	}
	return out, nil
}

// ListShoppingItems returns unchecked items first, then checked ones, each
// oldest first.
func (db *DB) ListShoppingItems(ctx context.Context) ([]domain.ShoppingItem, error) {
	return queryShoppingItems(ctx, db.sql, `SELECT `+shoppingColumns+` FROM shopping_items
		ORDER BY checked, created_at, id`)
}

// AddShoppingItems merges or inserts every item in one transaction, so a
// failure (the limit, a vanished recipe) leaves the list untouched.
func (db *DB) AddShoppingItems(ctx context.Context, items []domain.ShoppingItem, limit int,
	merge func(stored *domain.ShoppingItem, added domain.ShoppingItem) bool,
) ([]domain.ShoppingItem, error) {
	var out []domain.ShoppingItem
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		out = make([]domain.ShoppingItem, 0, len(items))
		position := make(map[domain.ShoppingItemID]int, len(items))
		for _, it := range items {
			if it.RecipeID != nil {
				if err := requireRow(ctx, tx, recipeExistsQuery, "recipe", int64(*it.RecipeID)); err != nil {
					return err
				}
			}
			stored, err := mergeShoppingItem(ctx, tx, it, merge)
			if err != nil {
				return err
			}
			if stored == nil {
				added, err := insertShoppingItem(ctx, tx, it, limit)
				if err != nil {
					return err
				}
				stored = &added
			}
			if i, ok := position[stored.ID]; ok {
				out[i] = *stored
			} else {
				position[stored.ID] = len(out)
				out = append(out, *stored)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// mergeShoppingItem offers it to every merge candidate and saves the first
// one merge accepts. It returns nil when no candidate accepted.
func mergeShoppingItem(ctx context.Context, tx *sql.Tx, it domain.ShoppingItem,
	merge func(*domain.ShoppingItem, domain.ShoppingItem) bool,
) (*domain.ShoppingItem, error) {
	_, unit := quantityArgs(it.Quantity)
	related := unit
	if it.Quantity != nil && it.Quantity.Unit != "" {
		related = string(it.Quantity.Unit.Related())
	}
	// The candidates are read completely before any update runs on the
	// same connection.
	candidates, err := queryShoppingItems(ctx, tx, mergeCandidatesQuery, searchKey(it.Name), unit, related)
	if err != nil {
		return nil, err
	}
	for _, c := range candidates {
		id := c.ID
		if !merge(&c, it) {
			continue
		}
		c.ID = id
		if err := updateShoppingItem(ctx, tx, c); err != nil {
			return nil, err
		}
		return &c, nil
	}
	return nil, nil
}

func insertShoppingItem(ctx context.Context, tx *sql.Tx, it domain.ShoppingItem, limit int) (domain.ShoppingItem, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM shopping_items`).Scan(&count); err != nil {
		return domain.ShoppingItem{}, fmt.Errorf("sqlite: count shopping items: %w", err)
	}
	if count >= limit {
		return domain.ShoppingItem{}, fmt.Errorf("shopping list has %d items: %w", count, domain.ErrLimitExceeded)
	}
	hundredths, unit := quantityArgs(it.Quantity)
	var recipe any
	if it.RecipeID != nil {
		recipe = int64(*it.RecipeID)
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO shopping_items (name, name_key, qty_hundredths, unit, checked, recipe_id, added_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id`,
		it.Name, searchKey(it.Name), hundredths, unit, it.Checked, recipe, int64(it.AddedBy),
		toMillis(it.CreatedAt), toMillis(it.UpdatedAt),
	).Scan(&it.ID); err != nil {
		return domain.ShoppingItem{}, fmt.Errorf("sqlite: insert shopping item: %w", err)
	}
	return it, nil
}

// updateShoppingItem saves the mutable columns of it: the name, the
// quantity, the checkbox and the modification time.
func updateShoppingItem(ctx context.Context, tx *sql.Tx, it domain.ShoppingItem) error {
	hundredths, unit := quantityArgs(it.Quantity)
	res, err := tx.ExecContext(ctx, `
		UPDATE shopping_items SET name = ?, name_key = ?, qty_hundredths = ?, unit = ?, checked = ?, updated_at = ?
		WHERE id = ?`,
		it.Name, searchKey(it.Name), hundredths, unit, it.Checked, toMillis(it.UpdatedAt), it.ID)
	if err != nil {
		return fmt.Errorf("sqlite: update shopping item %d: %w", it.ID, err)
	}
	return requireAffected(res, "shopping item", int64(it.ID))
}

// ModifyShoppingItem applies fn to the stored item inside a write
// transaction.
func (db *DB) ModifyShoppingItem(ctx context.Context, id domain.ShoppingItemID, fn func(*domain.ShoppingItem) error) (domain.ShoppingItem, error) {
	var it domain.ShoppingItem
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		it, err = scanShoppingItem(tx.QueryRowContext(ctx, `SELECT `+shoppingColumns+` FROM shopping_items WHERE id = ?`, id))
		if err != nil {
			return notFound(err, "shopping item", int64(id))
		}
		if err := fn(&it); err != nil {
			return err
		}
		it.ID = id
		return updateShoppingItem(ctx, tx, it)
	})
	if err != nil {
		return domain.ShoppingItem{}, err
	}
	return it, nil
}

// DeleteShoppingItem removes one item.
func (db *DB) DeleteShoppingItem(ctx context.Context, id domain.ShoppingItemID) error {
	res, err := db.sql.ExecContext(ctx, `DELETE FROM shopping_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete shopping item %d: %w", id, err)
	}
	return requireAffected(res, "shopping item", int64(id))
}

// DeleteCheckedShoppingItems removes every checked item.
func (db *DB) DeleteCheckedShoppingItems(ctx context.Context) (int, error) {
	res, err := db.sql.ExecContext(ctx, `DELETE FROM shopping_items WHERE checked = 1`)
	if err != nil {
		return 0, fmt.Errorf("sqlite: clear checked shopping items: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlite: clear checked shopping items: %w", err)
	}
	return int(n), nil
}
