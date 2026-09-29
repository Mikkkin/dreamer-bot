// Package sqlite is the SQLite implementation of the storage ports declared
// in internal/service. It uses the pure-Go modernc.org/sqlite driver, keeps
// the schema in embedded migrations and only ever runs static,
// parameterized SQL.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	modernc "modernc.org/sqlite" // registers the "sqlite" driver
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

var _ service.Repositories = (*DB)(nil)

// pragmas run on every new connection. busy_timeout lets writers queue
// instead of failing with SQLITE_BUSY, and _txlock=immediate takes the write
// lock at BEGIN, because a deferred transaction that upgrades from read to
// write can fail with SQLITE_BUSY regardless of the timeout.
const pragmas = "_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(1)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_txlock=immediate"

// maxOpenConns allows concurrent readers under WAL. A single connection
// would deadlock as soon as a query runs while a transaction is open.
const maxOpenConns = 4

// DB is the SQLite database. It implements service.Repositories.
type DB struct {
	sql *sql.DB
	log *slog.Logger
}

// Open opens (creating if needed) the database file at path and applies the
// pending migrations. The file is created with mode 0600 because it holds
// private data; SQLite gives the -wal and -shm files the same mode.
func Open(ctx context.Context, path string, log *slog.Logger) (*DB, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	dsn, err := prepareFile(path)
	if err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxOpenConns)
	sqlDB.SetConnMaxLifetime(0)
	sqlDB.SetConnMaxIdleTime(0)

	db := &DB{sql: sqlDB, log: log}
	if err := db.Ping(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := db.migrate(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// prepareFile creates the parent directory and the database file with
// private permissions and returns the DSN for it.
func prepareFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("sqlite: empty database path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("sqlite: resolve path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return "", fmt.Errorf("sqlite: create directory: %w", err)
	}
	f, err := os.OpenFile(abs, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return "", fmt.Errorf("sqlite: create database file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("sqlite: create database file: %w", err)
	}
	// A file: URI escapes '?', '#' and '%' in the path, which would
	// otherwise be parsed as URI syntax.
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: pragmas}
	return u.String(), nil
}

// Close closes the database.
func (db *DB) Close() error { return db.sql.Close() }

// Ping checks that the database is reachable.
func (db *DB) Ping(ctx context.Context) error {
	if err := db.sql.PingContext(ctx); err != nil {
		return fmt.Errorf("sqlite: ping: %w", err)
	}
	return nil
}

// querier is the subset shared by *sql.DB and *sql.Tx, so that read helpers
// work both inside and outside of transactions.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// scanner is implemented by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// withTx runs fn in a write transaction. An error from fn rolls it back and
// is returned unchanged.
func (db *DB) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit: %w", err)
	}
	return nil
}

// notFound maps sql.ErrNoRows to domain.ErrNotFound and wraps anything else.
func notFound(err error, what string, id int64) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %d: %w", what, id, domain.ErrNotFound)
	}
	return fmt.Errorf("sqlite: %s %d: %w", what, id, err)
}

// requireAffected turns an UPDATE or DELETE that matched no row into
// domain.ErrNotFound.
func requireAffected(res sql.Result, what string, id int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: %s %d: %w", what, id, err)
	}
	if n == 0 {
		return fmt.Errorf("%s %d: %w", what, id, domain.ErrNotFound)
	}
	return nil
}

// requireRow fails with domain.ErrNotFound when query (a "SELECT 1 … WHERE
// id = ?" statement) finds no row for id.
func requireRow(ctx context.Context, q querier, query, what string, id int64) error {
	var one int
	if err := q.QueryRowContext(ctx, query, id).Scan(&one); err != nil {
		return notFound(err, what, id)
	}
	return nil
}

// idList encodes ids as a JSON array for "IN (SELECT value FROM
// json_each(?))", which loads the children of many rows with one static
// statement.
func idList(ids []int64) (string, error) {
	b, err := json.Marshal(ids)
	if err != nil {
		return "", fmt.Errorf("sqlite: encode ids: %w", err)
	}
	return string(b), nil
}

// nullableInt stores 0 ("unknown") as NULL.
func nullableInt[T ~int | ~int64](v T) any {
	if v == 0 {
		return nil
	}
	return int64(v)
}

func isUniqueViolation(err error) bool {
	var se *modernc.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

func isForeignKeyViolation(err error) bool {
	var se *modernc.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
}

func toMillis(t time.Time) int64 { return t.UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func nullableMillis(t *time.Time) any {
	if t == nil {
		return nil
	}
	return toMillis(*t)
}

func timePtr(ms sql.Null[int64]) *time.Time {
	if !ms.Valid {
		return nil
	}
	t := fromMillis(ms.V)
	return &t
}

func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func stringPtr(s sql.Null[string]) *string {
	if !s.Valid {
		return nil
	}
	return &s.V
}

// searchKey folds s for case-insensitive, Unicode-aware substring matching.
// Mapping through upper case first also folds special lower-case forms such
// as the final sigma. «ё» is folded to «е» because Russian text often uses
// them interchangeably.
func searchKey(s string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(unicode.ToUpper(r))
		if r == 'ё' {
			return 'е'
		}
		return r
	}, s)
}
