package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migration is one schema step. seed, when set, runs in the same
// transaction right after the SQL.
type migration struct {
	version int
	name    string
	sql     string
	seed    func(ctx context.Context, tx *sql.Tx) error
}

// seeds attaches data changes to schema versions. The default categories
// belong to the first migration only, so categories the users delete never
// come back on a restart.
func seeds() map[int]func(context.Context, *sql.Tx) error {
	return map[int]func(context.Context, *sql.Tx) error{
		1: seedDefaultCategories,
	}
}

// loadMigrations reads NNNN_name.sql files in version order and checks that
// the versions are contiguous from 1.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("sqlite: read migrations: %w", err)
	}
	hooks := seeds()
	var out []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if !ok || err != nil || version <= 0 {
			return nil, fmt.Errorf("sqlite: bad migration file name %q", e.Name())
		}
		body, err := fs.ReadFile(migrationFiles, path.Join("migrations", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("sqlite: read migration %q: %w", e.Name(), err)
		}
		out = append(out, migration{version: version, name: e.Name(), sql: string(body), seed: hooks[version]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("sqlite: migrations are not contiguous at %q", m.name)
		}
	}
	return out, nil
}

// migrate applies every pending migration, each in its own transaction.
// Checking the version inside the write transaction makes it safe even if two
// processes start at once.
func (db *DB) migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := db.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return fmt.Errorf("sqlite: create schema_migrations: %w", err)
	}

	var current int
	if err := db.sql.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("sqlite: read schema version: %w", err)
	}
	if current > len(migrations) {
		return fmt.Errorf("sqlite: database schema version %d is newer than this binary supports (%d)", current, len(migrations))
	}

	for _, m := range migrations {
		applied, err := db.applyMigration(ctx, m)
		if err != nil {
			return err
		}
		if applied {
			db.log.Info("database migration applied", slog.Int("version", m.version), slog.String("name", m.name))
		}
	}
	return nil
}

func (db *DB) applyMigration(ctx context.Context, m migration) (applied bool, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version = ?`, m.version).Scan(&one)
		switch {
		case err == nil:
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return err
		}
		if m.seed != nil {
			if err := m.seed(ctx, tx); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.version, toMillis(time.Now())); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("sqlite: migration %q: %w", m.name, err)
	}
	return applied, nil
}

func seedDefaultCategories(ctx context.Context, tx *sql.Tx) error {
	now := toMillis(time.Now())
	for _, c := range domain.DefaultCategories() {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO categories (name, name_key, emoji, position, created_at) VALUES (?, ?, ?, ?, ?)`,
			c.Name, searchKey(c.Name), c.Emoji, c.Position, now); err != nil {
			return fmt.Errorf("seed category %q: %w", c.Name, err)
		}
	}
	return nil
}
