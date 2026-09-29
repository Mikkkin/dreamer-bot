package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const (
	cookColumns = `id, recipe_id, cooked_by, cooked_at`
	// cookRatingsQuery loads the ratings of many cooks at once, oldest first.
	cookRatingsQuery = `SELECT cook_id, user_id, stars, comment, rated_at FROM recipe_ratings
		WHERE cook_id IN (SELECT value FROM json_each(?)) ORDER BY rated_at, user_id`
	upsertRatingQuery = `INSERT INTO recipe_ratings (cook_id, user_id, stars, comment, rated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (cook_id, user_id) DO UPDATE SET
			stars    = excluded.stars,
			comment  = excluded.comment,
			rated_at = excluded.rated_at`
)

func scanCook(s scanner) (domain.Cook, error) {
	var (
		c      domain.Cook
		cooked int64
	)
	if err := s.Scan(&c.ID, &c.RecipeID, &c.CookedBy, &cooked); err != nil {
		return domain.Cook{}, err
	}
	c.CookedAt = fromMillis(cooked)
	return c, nil
}

// InsertCook stores a cooking of c.RecipeID with its ratings.
func (db *DB) InsertCook(ctx context.Context, c domain.Cook) (domain.Cook, error) {
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := requireRow(ctx, tx, recipeExistsQuery, "recipe", int64(c.RecipeID)); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM recipe_cooks WHERE recipe_id = ?`, c.RecipeID).Scan(&count); err != nil {
			return fmt.Errorf("sqlite: count cooks: %w", err)
		}
		if count >= domain.MaxCooksPerRecipe {
			return fmt.Errorf("recipe %d has %d cooks: %w", c.RecipeID, count, domain.ErrLimitExceeded)
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO recipe_cooks (recipe_id, cooked_by, cooked_at) VALUES (?, ?, ?) RETURNING id`,
			c.RecipeID, int64(c.CookedBy), toMillis(c.CookedAt)).Scan(&c.ID); err != nil {
			return fmt.Errorf("sqlite: insert cook: %w", err)
		}
		for _, r := range c.Ratings {
			if err := upsertRating(ctx, tx, c.ID, r); err != nil {
				return err
			}
		}
		var err error
		c, err = getCook(ctx, tx, c.ID)
		return err
	})
	if err != nil {
		return domain.Cook{}, err
	}
	return c, nil
}

func upsertRating(ctx context.Context, tx *sql.Tx, cook domain.CookID, r domain.Rating) error {
	if _, err := tx.ExecContext(ctx, upsertRatingQuery,
		cook, int64(r.UserID), r.Stars, r.Comment, toMillis(r.RatedAt)); err != nil {
		return fmt.Errorf("sqlite: save rating of cook %d: %w", cook, err)
	}
	return nil
}

// UpsertRating sets r.UserID's rating of a cooking of recipe.
func (db *DB) UpsertRating(ctx context.Context, recipe domain.RecipeID, cook domain.CookID, r domain.Rating) (domain.Cook, error) {
	var c domain.Cook
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM recipe_cooks WHERE id = ? AND recipe_id = ?`,
			cook, recipe).Scan(&one); err != nil {
			return notFound(err, "cook", int64(cook))
		}
		if err := upsertRating(ctx, tx, cook, r); err != nil {
			return err
		}
		var err error
		c, err = getCook(ctx, tx, cook)
		return err
	})
	if err != nil {
		return domain.Cook{}, err
	}
	return c, nil
}

func getCook(ctx context.Context, q querier, id domain.CookID) (domain.Cook, error) {
	c, err := scanCook(q.QueryRowContext(ctx, `SELECT `+cookColumns+` FROM recipe_cooks WHERE id = ?`, id))
	if err != nil {
		return domain.Cook{}, notFound(err, "cook", int64(id))
	}
	cooks := []domain.Cook{c}
	if err := loadRatings(ctx, q, cooks); err != nil {
		return domain.Cook{}, err
	}
	return cooks[0], nil
}

// loadRatings fills the ratings of every cook with one query.
func loadRatings(ctx context.Context, q querier, cooks []domain.Cook) error {
	if len(cooks) == 0 {
		return nil
	}
	ids := make([]int64, len(cooks))
	for i, c := range cooks {
		ids[i] = int64(c.ID)
	}
	list, err := idList(ids)
	if err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, cookRatingsQuery, list)
	if err != nil {
		return fmt.Errorf("sqlite: load ratings: %w", err)
	}
	defer rows.Close()
	byCook := make(map[domain.CookID][]domain.Rating, len(cooks))
	for rows.Next() {
		var (
			cook  domain.CookID
			r     domain.Rating
			rated int64
		)
		if err := rows.Scan(&cook, &r.UserID, &r.Stars, &r.Comment, &rated); err != nil {
			return fmt.Errorf("sqlite: scan rating: %w", err)
		}
		r.RatedAt = fromMillis(rated)
		byCook[cook] = append(byCook[cook], r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sqlite: load ratings: %w", err)
	}
	for i := range cooks {
		cooks[i].Ratings = orEmpty(byCook[cooks[i].ID])
	}
	return nil
}

// ListCooks returns the cooking history of a recipe, newest first.
func (db *DB) ListCooks(ctx context.Context, recipe domain.RecipeID) ([]domain.Cook, error) {
	if err := requireRow(ctx, db.sql, recipeExistsQuery, "recipe", int64(recipe)); err != nil {
		return nil, err
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+cookColumns+` FROM recipe_cooks
		WHERE recipe_id = ? ORDER BY cooked_at DESC, id DESC`, recipe)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list cooks: %w", err)
	}
	defer rows.Close()
	cooks := []domain.Cook{}
	for rows.Next() {
		c, err := scanCook(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan cook: %w", err)
		}
		cooks = append(cooks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list cooks: %w", err)
	}
	if err := loadRatings(ctx, db.sql, cooks); err != nil {
		return nil, err
	}
	return cooks, nil
}

// DeleteCook removes a cooking of recipe; its ratings go with it through
// the foreign key cascade.
func (db *DB) DeleteCook(ctx context.Context, recipe domain.RecipeID, cook domain.CookID) error {
	res, err := db.sql.ExecContext(ctx, `DELETE FROM recipe_cooks WHERE id = ? AND recipe_id = ?`, cook, recipe)
	if err != nil {
		return fmt.Errorf("sqlite: delete cook %d: %w", cook, err)
	}
	return requireAffected(res, "cook", int64(cook))
}

// CountCooks returns how many times any recipe was cooked.
func (db *DB) CountCooks(ctx context.Context) (int, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM recipe_cooks`).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: count cooks: %w", err)
	}
	return n, nil
}
