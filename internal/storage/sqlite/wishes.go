package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const wishColumns = `id, title, note, category_id, link, price_minor, price_currency,
	status, hot, author_id, created_at, updated_at, fulfilled_at`

// listWishesQuery is static: every filter is a parameter that disables
// itself when unset (?1 status, ?2 category, ?3 uncategorized flag, ?4 folded
// title query).
const listWishesQuery = `SELECT ` + wishColumns + ` FROM wishes
	WHERE (?1 IS NULL OR status = ?1)
	  AND (?2 IS NULL OR category_id = ?2)
	  AND (?3 = 0 OR category_id IS NULL)
	  AND (?4 = '' OR instr(title_key, ?4) > 0)
	ORDER BY created_at DESC, id DESC`

func scanWish(s scanner) (domain.Wish, error) {
	var (
		w                domain.Wish
		category         sql.Null[domain.CategoryID]
		link, currency   sql.Null[string]
		minor, fulfilled sql.Null[int64]
		created, updated int64
	)
	if err := s.Scan(&w.ID, &w.Title, &w.Note, &category, &link, &minor, &currency,
		&w.Status, &w.Hot, &w.AuthorID, &created, &updated, &fulfilled); err != nil {
		return domain.Wish{}, err
	}
	if category.Valid {
		w.CategoryID = &category.V
	}
	w.Link = stringPtr(link)
	if minor.Valid && currency.Valid {
		w.Price = &domain.Money{Minor: minor.V, Currency: domain.Currency(currency.V)}
	}
	w.CreatedAt = fromMillis(created)
	w.UpdatedAt = fromMillis(updated)
	w.FulfilledAt = timePtr(fulfilled)
	return w, nil
}

// wishArgs returns the values of the mutable columns in the order
// title, title_key, note, category_id, link, price_minor, price_currency,
// status, hot, updated_at, fulfilled_at.
func wishArgs(w domain.Wish) []any {
	var category, minor, currency any
	if w.CategoryID != nil {
		category = int64(*w.CategoryID)
	}
	if w.Price != nil {
		minor, currency = w.Price.Minor, string(w.Price.Currency)
	}
	return []any{
		w.Title, searchKey(w.Title), w.Note, category, nullableString(w.Link), minor, currency,
		string(w.Status), w.Hot, toMillis(w.UpdatedAt), nullableMillis(w.FulfilledAt),
	}
}

// InsertWish stores a new wish.
func (db *DB) InsertWish(ctx context.Context, w domain.Wish) (domain.Wish, error) {
	args := append(wishArgs(w), int64(w.AuthorID), toMillis(w.CreatedAt))
	res, err := db.sql.ExecContext(ctx, `
		INSERT INTO wishes (title, title_key, note, category_id, link, price_minor, price_currency,
			status, hot, updated_at, fulfilled_at, author_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...)
	if err != nil {
		return domain.Wish{}, fmt.Errorf("sqlite: insert wish: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Wish{}, fmt.Errorf("sqlite: insert wish: %w", err)
	}
	w.ID = domain.WishID(id)
	w.Images = nil
	return w, nil
}

// GetWish returns one wish with its images.
func (db *DB) GetWish(ctx context.Context, id domain.WishID) (domain.Wish, error) {
	return getWish(ctx, db.sql, id)
}

func getWish(ctx context.Context, q querier, id domain.WishID) (domain.Wish, error) {
	w, err := scanWish(q.QueryRowContext(ctx, `SELECT `+wishColumns+` FROM wishes WHERE id = ?`, id))
	if err != nil {
		return domain.Wish{}, notFound(err, "wish", int64(id))
	}
	images, err := loadImages(ctx, q, wishImagesQuery, []int64{int64(id)})
	if err != nil {
		return domain.Wish{}, err
	}
	w.Images = images[int64(id)]
	return w, nil
}

// ListWishes returns the wishes matching f, newest first, with their images.
func (db *DB) ListWishes(ctx context.Context, f domain.WishFilter) ([]domain.Wish, error) {
	var status, category any
	uncategorized := 0
	if f.Status != nil {
		status = string(*f.Status)
	}
	if f.CategoryID != nil {
		if *f.CategoryID == domain.Uncategorized {
			uncategorized = 1
		} else {
			category = int64(*f.CategoryID)
		}
	}

	rows, err := db.sql.QueryContext(ctx, listWishesQuery, status, category, uncategorized, searchKey(f.Query))
	if err != nil {
		return nil, fmt.Errorf("sqlite: list wishes: %w", err)
	}
	defer rows.Close()
	var (
		wishes []domain.Wish
		ids    []int64
	)
	for rows.Next() {
		w, err := scanWish(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan wish: %w", err)
		}
		wishes = append(wishes, w)
		ids = append(ids, int64(w.ID))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list wishes: %w", err)
	}

	images, err := loadImages(ctx, db.sql, wishImagesQuery, ids)
	if err != nil {
		return nil, err
	}
	for i := range wishes {
		wishes[i].Images = images[int64(wishes[i].ID)]
	}
	return wishes, nil
}

// ModifyWish applies fn to the stored wish inside a write transaction.
func (db *DB) ModifyWish(ctx context.Context, id domain.WishID, fn func(*domain.Wish) error) (domain.Wish, error) {
	var w domain.Wish
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		if w, err = getWish(ctx, tx, id); err != nil {
			return err
		}
		if err := fn(&w); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE wishes SET title = ?, title_key = ?, note = ?, category_id = ?, link = ?,
				price_minor = ?, price_currency = ?, status = ?, hot = ?, updated_at = ?, fulfilled_at = ?
			WHERE id = ?`, append(wishArgs(w), int64(id))...)
		if err != nil {
			return fmt.Errorf("sqlite: update wish %d: %w", id, err)
		}
		return requireAffected(res, "wish", int64(id))
	})
	if err != nil {
		return domain.Wish{}, err
	}
	return w, nil
}

// DeleteWish removes a wish; its image rows go with it through the foreign
// key cascade. The keys are read first so that the files can be removed.
func (db *DB) DeleteWish(ctx context.Context, id domain.WishID) ([]string, error) {
	var keys []string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		if keys, err = imageKeys(ctx, tx, wishImageKeysQuery, int64(id)); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM wishes WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("sqlite: delete wish %d: %w", id, err)
		}
		return requireAffected(res, "wish", int64(id))
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}
