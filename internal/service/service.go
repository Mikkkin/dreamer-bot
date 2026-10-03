package service

import (
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Deps are the collaborators of the use cases.
type Deps struct {
	Repos    Repositories
	Media    MediaStore
	Notifier Notifier // nil => no-op
	// Importer reads recipes from Instagram posts and pasted text for
	// Recipes.Import; nil => imports fail with domain.ErrExternalUnavailable.
	Importer RecipeImporter
	// Whitelist lists every allowed user. Partners of a user are the other
	// whitelisted users who have an open chat with the bot.
	Whitelist []domain.UserID
	Now       func() time.Time // nil => time.Now
	Log       *slog.Logger     // nil => discard
}

// Services bundles every use case for the adapters (bot and HTTP API).
type Services struct {
	Wishes     Wishes
	Recipes    Recipes
	RecipeTags RecipeTags
	Shopping   Shopping
	Categories Categories
	Images     Images
	Stats      Stats
	Users      Users
}

// New validates the dependencies and builds the use cases.
func New(d Deps) (*Services, error) {
	if d.Repos == nil {
		return nil, errors.New("service: Repos is required")
	}
	if d.Media == nil {
		return nil, errors.New("service: Media is required")
	}
	for _, id := range d.Whitelist {
		if id <= 0 {
			return nil, errors.New("service: whitelist contains a non-positive user ID")
		}
	}
	if d.Notifier == nil {
		d.Notifier = noopNotifier{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}

	c := &core{
		repos:     d.Repos,
		media:     d.Media,
		notifier:  d.Notifier,
		importer:  d.Importer,
		importing: newKeyedLocks(),
		imports:   newImportLimits(),
		whitelist: slices.Clone(d.Whitelist),
		clock:     d.Now,
		location:  time.Local,
		log:       d.Log,
	}
	return &Services{
		Wishes:     wishService{c},
		Recipes:    recipeService{c},
		RecipeTags: recipeTagService{c},
		Shopping:   shoppingService{c},
		Categories: categoryService{c},
		Images:     imageService{c},
		Stats:      statsService{c},
		Users:      userService{c},
	}, nil
}

// core holds the state shared by every use case.
type core struct {
	repos    Repositories
	media    MediaStore
	notifier Notifier
	importer RecipeImporter
	// importing serializes imports of one post.
	importing *keyedLocks
	// imports throttles imports per user and in total (both front ends).
	imports   *importLimits
	whitelist []domain.UserID
	clock     func() time.Time
	// location defines "this year" for the statistics.
	location *time.Location
	log      *slog.Logger
}

// now returns the current time truncated to the millisecond precision of
// storage, so that an entity returned by a use case is identical to what a
// later read returns.
func (c *core) now() time.Time { return c.clock().UTC().Truncate(time.Millisecond) }

func (c *core) whitelisted(id domain.UserID) bool { return slices.Contains(c.whitelist, id) }

// deleteFiles removes stored image files. It runs after the database change
// has been committed, so a failure only leaves an orphan file behind and is
// logged instead of failing the use case.
func (c *core) deleteFiles(keys ...string) {
	for _, key := range keys {
		if err := c.media.Delete(key); err != nil {
			c.log.Error("delete image files", slog.String("image_key", key), slog.Any("error", err))
		}
	}
}
