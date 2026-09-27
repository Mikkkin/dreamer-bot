-- Times are INTEGER unix milliseconds (UTC). *_key columns hold the
-- case-folded text computed in Go: SQLite's lower(), LIKE and NOCASE only fold
-- ASCII, which would break case-insensitive matching of Cyrillic text.

CREATE TABLE users (
    id         INTEGER PRIMARY KEY,
    first_name TEXT    NOT NULL DEFAULT '',
    last_name  TEXT    NOT NULL DEFAULT '',
    username   TEXT    NOT NULL DEFAULT '',
    has_chat   INTEGER NOT NULL DEFAULT 0 CHECK (has_chat IN (0, 1)),
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE categories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    name_key   TEXT    NOT NULL UNIQUE,
    emoji      TEXT    NOT NULL,
    position   INTEGER NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE wishes (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    title          TEXT    NOT NULL,
    title_key      TEXT    NOT NULL,
    note           TEXT    NOT NULL DEFAULT '',
    category_id    INTEGER REFERENCES categories (id) ON DELETE SET NULL,
    link           TEXT,
    price_minor    INTEGER CHECK (price_minor > 0),
    price_currency TEXT,
    status         TEXT    NOT NULL CHECK (status IN ('want', 'progress', 'done')),
    hot            INTEGER NOT NULL DEFAULT 0 CHECK (hot IN (0, 1)),
    author_id      INTEGER NOT NULL,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    fulfilled_at   INTEGER,
    CHECK ((price_minor IS NULL) = (price_currency IS NULL))
) STRICT;

CREATE INDEX wishes_by_created  ON wishes (created_at DESC, id DESC);
CREATE INDEX wishes_by_category ON wishes (category_id);
CREATE INDEX wishes_by_status   ON wishes (status);

CREATE TABLE recipes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    title_key  TEXT    NOT NULL,
    link       TEXT,
    body       TEXT    NOT NULL DEFAULT '',
    body_key   TEXT    NOT NULL DEFAULT '',
    author_id  INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX recipes_by_created ON recipes (created_at DESC, id DESC);

CREATE TABLE images (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    file_key   TEXT    NOT NULL UNIQUE,
    wish_id    INTEGER REFERENCES wishes (id) ON DELETE CASCADE,
    recipe_id  INTEGER REFERENCES recipes (id) ON DELETE CASCADE,
    width      INTEGER NOT NULL CHECK (width > 0),
    height     INTEGER NOT NULL CHECK (height > 0),
    bytes      INTEGER NOT NULL CHECK (bytes >= 0),
    position   INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    CHECK ((wish_id IS NULL) <> (recipe_id IS NULL))
) STRICT;

CREATE INDEX images_by_wish   ON images (wish_id, position);
CREATE INDEX images_by_recipe ON images (recipe_id, position);
