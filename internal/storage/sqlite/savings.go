package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const savingColumns = `id, wish_id, amount_minor, currency, user_id, note, created_at`

func scanSaving(s scanner) (domain.Saving, error) {
	var (
		sv      domain.Saving
		created int64
	)
	if err := s.Scan(&sv.ID, &sv.WishID, &sv.Amount.Minor, &sv.Amount.Currency, &sv.UserID, &sv.Note, &created); err != nil {
		return domain.Saving{}, err
	}
	sv.CreatedAt = fromMillis(created)
	return sv, nil
}

// InsertSaving builds a saving with fn against the stored wish and stores
// it, together with fn's changes to the wish, in one IMMEDIATE transaction.
// Because the wish (with its Saved total) is read inside the transaction,
// two concurrent first savings in different currencies cannot both pass
// the one-currency rule.
func (db *DB) InsertSaving(ctx context.Context, id domain.WishID, fn func(*domain.Wish) (domain.Saving, error)) (domain.Wish, domain.Saving, error) {
	var (
		w  domain.Wish
		sv domain.Saving
	)
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		if w, err = getWish(ctx, tx, id); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM wish_savings WHERE wish_id = ?`, id).Scan(&count); err != nil {
			return fmt.Errorf("sqlite: count savings: %w", err)
		}
		if count >= domain.MaxSavingsPerWish {
			return fmt.Errorf("wish %d has %d savings: %w", id, count, domain.ErrLimitExceeded)
		}
		if sv, err = fn(&w); err != nil {
			return err
		}
		sv.WishID = id
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO wish_savings (wish_id, amount_minor, currency, user_id, note, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			RETURNING id`,
			id, sv.Amount.Minor, string(sv.Amount.Currency), int64(sv.UserID), sv.Note, toMillis(sv.CreatedAt),
		).Scan(&sv.ID); err != nil {
			return fmt.Errorf("sqlite: insert saving: %w", err)
		}
		if err := updateWish(ctx, tx, id, w); err != nil {
			return err
		}
		w, err = getWish(ctx, tx, id)
		return err
	})
	if err != nil {
		return domain.Wish{}, domain.Saving{}, err
	}
	return w, sv, nil
}

// ListSavings returns the savings of a wish, newest first.
func (db *DB) ListSavings(ctx context.Context, id domain.WishID) ([]domain.Saving, error) {
	if err := requireRow(ctx, db.sql, wishExistsQuery, "wish", int64(id)); err != nil {
		return nil, err
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+savingColumns+` FROM wish_savings
		WHERE wish_id = ? ORDER BY created_at DESC, id DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list savings: %w", err)
	}
	defer rows.Close()
	out := []domain.Saving{}
	for rows.Next() {
		sv, err := scanSaving(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan saving: %w", err)
		}
		out = append(out, sv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list savings: %w", err)
	}
	return out, nil
}

// DeleteSaving removes a saving of wish.
func (db *DB) DeleteSaving(ctx context.Context, wish domain.WishID, id domain.SavingID) error {
	res, err := db.sql.ExecContext(ctx, `DELETE FROM wish_savings WHERE id = ? AND wish_id = ?`, id, wish)
	if err != nil {
		return fmt.Errorf("sqlite: delete saving %d: %w", id, err)
	}
	return requireAffected(res, "saving", int64(id))
}
