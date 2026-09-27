package sqlite

import (
	"context"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const userColumns = `id, first_name, last_name, username, has_chat, updated_at`

func scanUser(s scanner) (domain.User, error) {
	var (
		u       domain.User
		updated int64
	)
	if err := s.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Username, &u.HasChat, &updated); err != nil {
		return domain.User{}, err
	}
	u.UpdatedAt = fromMillis(updated)
	return u, nil
}

// GetUser returns one stored profile.
func (db *DB) GetUser(ctx context.Context, id domain.UserID) (domain.User, error) {
	u, err := scanUser(db.sql.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if err != nil {
		return domain.User{}, notFound(err, "user", int64(id))
	}
	return u, nil
}

// ListUsers returns every stored profile ordered by ID.
func (db *DB) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list users: %w", err)
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan user: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list users: %w", err)
	}
	return out, nil
}

// UpsertUser stores a profile. has_chat is merged in SQL so that it stays
// sticky even when two updates race.
func (db *DB) UpsertUser(ctx context.Context, u domain.User) error {
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO users (id, first_name, last_name, username, has_chat, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			first_name = excluded.first_name,
			last_name  = excluded.last_name,
			username   = excluded.username,
			has_chat   = MAX(users.has_chat, excluded.has_chat),
			updated_at = excluded.updated_at`,
		int64(u.ID), u.FirstName, u.LastName, u.Username, u.HasChat, toMillis(u.UpdatedAt))
	if err != nil {
		return fmt.Errorf("sqlite: upsert user %d: %w", u.ID, err)
	}
	return nil
}
