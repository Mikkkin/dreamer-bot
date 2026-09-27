package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

const imageColumns = `id, file_key, width, height, bytes, position, created_at`

// Statements that exist once per owner kind. InsertImage and DeleteImage
// instead serve both kinds by matching both owner columns with IS (see
// ownerArgs).
const (
	wishImagesQuery = `SELECT wish_id, ` + imageColumns + ` FROM images
		WHERE wish_id IN (SELECT value FROM json_each(?)) ORDER BY position, id`
	recipeImagesQuery = `SELECT recipe_id, ` + imageColumns + ` FROM images
		WHERE recipe_id IN (SELECT value FROM json_each(?)) ORDER BY position, id`
	wishImageKeysQuery   = `SELECT file_key FROM images WHERE wish_id = ?`
	recipeImageKeysQuery = `SELECT file_key FROM images WHERE recipe_id = ?`
	wishExistsQuery      = `SELECT 1 FROM wishes WHERE id = ?`
	recipeExistsQuery    = `SELECT 1 FROM recipes WHERE id = ?`
)

func scanImage(s scanner, extra ...any) (domain.Image, error) {
	var (
		img     domain.Image
		created int64
	)
	dest := append(extra, &img.ID, &img.Key, &img.Width, &img.Height, &img.Bytes, &img.Position, &created)
	if err := s.Scan(dest...); err != nil {
		return domain.Image{}, err
	}
	img.CreatedAt = fromMillis(created)
	return img, nil
}

// loadImages returns the images of every owner in ids with a single query,
// grouped by owner. query is wishImagesQuery or recipeImagesQuery.
func loadImages(ctx context.Context, q querier, query string, ids []int64) (map[int64][]domain.Image, error) {
	out := make(map[int64][]domain.Image, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, fmt.Errorf("sqlite: encode ids: %w", err)
	}
	rows, err := q.QueryContext(ctx, query, string(list))
	if err != nil {
		return nil, fmt.Errorf("sqlite: load images: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner int64
		img, err := scanImage(rows, &owner)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan image: %w", err)
		}
		out[owner] = append(out[owner], img)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: load images: %w", err)
	}
	return out, nil
}

// imageKeys returns the file keys of the images of one owner. query is
// wishImageKeysQuery or recipeImageKeysQuery.
func imageKeys(ctx context.Context, q querier, query string, owner int64) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, owner)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list image keys: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("sqlite: scan image key: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list image keys: %w", err)
	}
	return keys, nil
}

// ownerArgs returns the values of the wish_id and recipe_id columns for
// owner, the unused one being nil, so that "wish_id IS ? AND recipe_id IS ?"
// selects exactly that owner's images. It also returns the query that checks
// the owner exists.
func ownerArgs(o service.ImageOwner) (wish, recipe any, existsQuery string, err error) {
	if err := o.Validate(); err != nil {
		return nil, nil, "", err
	}
	if o.Wish != 0 {
		return int64(o.Wish), nil, wishExistsQuery, nil
	}
	return nil, int64(o.Recipe), recipeExistsQuery, nil
}

func ownerName(o service.ImageOwner) (string, int64) {
	if o.Wish != 0 {
		return "wish", int64(o.Wish)
	}
	return "recipe", int64(o.Recipe)
}

// GetImage returns one image.
func (db *DB) GetImage(ctx context.Context, id domain.ImageID) (domain.Image, error) {
	img, err := scanImage(db.sql.QueryRowContext(ctx, `SELECT `+imageColumns+` FROM images WHERE id = ?`, id))
	if err != nil {
		return domain.Image{}, notFound(err, "image", int64(id))
	}
	return img, nil
}

// InsertImage appends img to the images of owner, enforcing limit.
func (db *DB) InsertImage(ctx context.Context, owner service.ImageOwner, img domain.Image, limit int) (domain.Image, error) {
	wish, recipe, existsQuery, err := ownerArgs(owner)
	if err != nil {
		return domain.Image{}, err
	}
	what, id := ownerName(owner)
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRowContext(ctx, existsQuery, id).Scan(&one); err != nil {
			return notFound(err, what, id)
		}
		var count, lastPosition int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(MAX(position), -1) FROM images WHERE wish_id IS ? AND recipe_id IS ?`,
			wish, recipe,
		).Scan(&count, &lastPosition); err != nil {
			return fmt.Errorf("sqlite: count images: %w", err)
		}
		if count >= limit {
			return fmt.Errorf("%s %d has %d images: %w", what, id, count, domain.ErrLimitExceeded)
		}
		img.Position = lastPosition + 1
		res, err := tx.ExecContext(ctx, `
			INSERT INTO images (file_key, wish_id, recipe_id, width, height, bytes, position, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			img.Key, wish, recipe, img.Width, img.Height, img.Bytes, img.Position, toMillis(img.CreatedAt))
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("image key: %w", domain.ErrConflict)
			}
			return fmt.Errorf("sqlite: insert image: %w", err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("sqlite: insert image: %w", err)
		}
		img.ID = domain.ImageID(newID)
		return nil
	})
	if err != nil {
		return domain.Image{}, err
	}
	return img, nil
}

// DeleteImage removes an image of owner and returns the removed row.
func (db *DB) DeleteImage(ctx context.Context, owner service.ImageOwner, id domain.ImageID) (domain.Image, error) {
	wish, recipe, _, err := ownerArgs(owner)
	if err != nil {
		return domain.Image{}, err
	}
	img, err := scanImage(db.sql.QueryRowContext(ctx, `
		DELETE FROM images WHERE id = ? AND wish_id IS ? AND recipe_id IS ?
		RETURNING `+imageColumns,
		id, wish, recipe))
	if err != nil {
		return domain.Image{}, notFound(err, "image", int64(id))
	}
	return img, nil
}
