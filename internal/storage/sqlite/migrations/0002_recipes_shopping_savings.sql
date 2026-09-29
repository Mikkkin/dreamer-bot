-- Recipe tags, ingredients, КБЖУ, the cooking history with ratings, savings
-- for wishes and the shared shopping list.
--
-- This migration is additive only: it never rewrites or drops an existing
-- table, so every row written by version 1 survives unchanged. New columns
-- of existing tables are nullable. Quantities are INTEGER hundredths and
-- КБЖУ values INTEGER tenths, as in internal/domain.

CREATE TABLE recipe_tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT    NOT NULL CHECK (kind IN ('cuisine', 'course')),
    name       TEXT    NOT NULL,
    name_key   TEXT    NOT NULL,
    emoji      TEXT    NOT NULL,
    position   INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (kind, name_key)
) STRICT;

-- A recipe has at most one cuisine; deleting the tag leaves the recipe
-- without a cuisine.
ALTER TABLE recipes ADD COLUMN cuisine_id INTEGER REFERENCES recipe_tags (id) ON DELETE SET NULL;

-- КБЖУ per 100 g in tenths. The four values are set together or not at all
-- (NULL = КБЖУ not specified); weight and servings are optional.
ALTER TABLE recipes ADD COLUMN kcal_tenths    INTEGER CHECK (kcal_tenths BETWEEN 0 AND 9000);
ALTER TABLE recipes ADD COLUMN protein_tenths INTEGER CHECK (protein_tenths BETWEEN 0 AND 1000);
ALTER TABLE recipes ADD COLUMN fat_tenths     INTEGER CHECK (fat_tenths BETWEEN 0 AND 1000);
ALTER TABLE recipes ADD COLUMN carbs_tenths   INTEGER CHECK (carbs_tenths BETWEEN 0 AND 1000)
    CHECK ((kcal_tenths IS NULL) = (protein_tenths IS NULL)
       AND (kcal_tenths IS NULL) = (fat_tenths IS NULL)
       AND (kcal_tenths IS NULL) = (carbs_tenths IS NULL));
ALTER TABLE recipes ADD COLUMN weight_g INTEGER CHECK (weight_g BETWEEN 1 AND 20000);
ALTER TABLE recipes ADD COLUMN servings INTEGER CHECK (servings BETWEEN 1 AND 50);

CREATE INDEX recipes_by_cuisine ON recipes (cuisine_id);

-- Course tags of a recipe in the order the user picked them.
CREATE TABLE recipe_courses (
    recipe_id INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    tag_id    INTEGER NOT NULL REFERENCES recipe_tags (id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    PRIMARY KEY (recipe_id, tag_id)
) STRICT;

CREATE INDEX recipe_courses_by_tag ON recipe_courses (tag_id);

-- NULL qty_hundredths and unit = no quantity; a unit without an amount is
-- e.g. «по вкусу».
CREATE TABLE recipe_ingredients (
    recipe_id      INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    position       INTEGER NOT NULL,
    name           TEXT    NOT NULL,
    qty_hundredths INTEGER CHECK (qty_hundredths BETWEEN 1 AND 10000000),
    unit           TEXT,
    PRIMARY KEY (recipe_id, position)
) STRICT;

CREATE TABLE recipe_cooks (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    recipe_id INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    cooked_by INTEGER NOT NULL,
    cooked_at INTEGER NOT NULL
) STRICT;

CREATE INDEX recipe_cooks_by_recipe ON recipe_cooks (recipe_id, cooked_at);

-- One rating per person and cooking; rating again replaces it.
CREATE TABLE recipe_ratings (
    cook_id  INTEGER NOT NULL REFERENCES recipe_cooks (id) ON DELETE CASCADE,
    user_id  INTEGER NOT NULL,
    stars    INTEGER NOT NULL CHECK (stars BETWEEN 1 AND 5),
    comment  TEXT    NOT NULL DEFAULT '',
    rated_at INTEGER NOT NULL,
    PRIMARY KEY (cook_id, user_id)
) STRICT;

-- Money put aside for a wish («Копим»). All savings of one wish share one
-- currency; the service enforces it inside the inserting transaction.
CREATE TABLE wish_savings (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    wish_id      INTEGER NOT NULL REFERENCES wishes (id) ON DELETE CASCADE,
    amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
    currency     TEXT    NOT NULL,
    user_id      INTEGER NOT NULL,
    note         TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX wish_savings_by_wish ON wish_savings (wish_id, created_at);

-- The shared shopping list. name_key is the case-folded name used to merge
-- a new item into an unchecked one with the same name and unit.
CREATE TABLE shopping_items (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL,
    name_key       TEXT    NOT NULL,
    qty_hundredths INTEGER CHECK (qty_hundredths BETWEEN 1 AND 10000000),
    unit           TEXT,
    checked        INTEGER NOT NULL DEFAULT 0 CHECK (checked IN (0, 1)),
    recipe_id      INTEGER REFERENCES recipes (id) ON DELETE SET NULL,
    added_by       INTEGER NOT NULL,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
) STRICT;

CREATE INDEX shopping_items_in_order ON shopping_items (checked, created_at, id);
CREATE INDEX shopping_items_by_name  ON shopping_items (name_key, unit) WHERE checked = 0;
CREATE INDEX shopping_items_by_recipe ON shopping_items (recipe_id);
