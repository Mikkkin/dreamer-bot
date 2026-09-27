package service

import (
	"context"
	"errors"
	"io"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Repositories is the storage port of the use cases. One SQLite database
// implements all of it (internal/storage/sqlite).
//
// Implementations map missing rows to domain.ErrNotFound and uniqueness
// violations to domain.ErrConflict.
type Repositories interface {
	WishRepository
	RecipeRepository
	CategoryRepository
	ImageRepository
	UserRepository
}

// WishRepository persists wishes. Every returned wish carries its images
// ordered by position.
type WishRepository interface {
	// InsertWish stores a new wish and returns it with its ID.
	InsertWish(ctx context.Context, w domain.Wish) (domain.Wish, error)
	GetWish(ctx context.Context, id domain.WishID) (domain.Wish, error)
	// ListWishes returns the wishes matching f, newest first. Query is a
	// Unicode-aware case-insensitive substring of the title.
	ListWishes(ctx context.Context, f domain.WishFilter) ([]domain.Wish, error)
	// ModifyWish loads the wish, lets fn change it and saves the result in
	// one transaction, so concurrent edits cannot interleave. An error from
	// fn aborts the change and is returned unchanged. fn must not keep the
	// pointer or touch storage.
	ModifyWish(ctx context.Context, id domain.WishID, fn func(*domain.Wish) error) (domain.Wish, error)
	// DeleteWish removes the wish with its image rows and returns the keys
	// of the removed images so that their files can be deleted afterwards.
	DeleteWish(ctx context.Context, id domain.WishID) (imageKeys []string, err error)
}

// RecipeRepository persists recipes. Every returned recipe carries its
// images ordered by position.
type RecipeRepository interface {
	InsertRecipe(ctx context.Context, r domain.Recipe) (domain.Recipe, error)
	GetRecipe(ctx context.Context, id domain.RecipeID) (domain.Recipe, error)
	// ListRecipes returns the recipes matching f, newest first. Query is a
	// Unicode-aware case-insensitive substring of the title or the body.
	ListRecipes(ctx context.Context, f domain.RecipeFilter) ([]domain.Recipe, error)
	// RandomRecipe returns a uniformly random recipe, or domain.ErrNotFound
	// when there are none.
	RandomRecipe(ctx context.Context) (domain.Recipe, error)
	// ModifyRecipe has the same contract as WishRepository.ModifyWish.
	ModifyRecipe(ctx context.Context, id domain.RecipeID, fn func(*domain.Recipe) error) (domain.Recipe, error)
	DeleteRecipe(ctx context.Context, id domain.RecipeID) (imageKeys []string, err error)
	CountRecipes(ctx context.Context) (int, error)
}

// CategoryRepository persists categories. Names are unique ignoring case
// (Unicode-aware); a clash is reported as domain.ErrConflict.
type CategoryRepository interface {
	// ListCategories returns every category in display order.
	ListCategories(ctx context.Context) ([]domain.Category, error)
	GetCategory(ctx context.Context, id domain.CategoryID) (domain.Category, error)
	// InsertCategory stores c at the end of the display order and returns
	// it with its ID and position.
	InsertCategory(ctx context.Context, c domain.Category) (domain.Category, error)
	// ModifyCategory loads the category, applies fn and saves the result in
	// one transaction.
	ModifyCategory(ctx context.Context, id domain.CategoryID, fn func(*domain.Category) error) (domain.Category, error)
	// DeleteCategory removes the category; its wishes become uncategorized.
	DeleteCategory(ctx context.Context, id domain.CategoryID) error
}

// ImageRepository persists image metadata; the files live in MediaStore.
type ImageRepository interface {
	GetImage(ctx context.Context, id domain.ImageID) (domain.Image, error)
	// InsertImage appends img to the owner's images. It fails with
	// domain.ErrNotFound when the owner does not exist and with
	// domain.ErrLimitExceeded when the owner already has limit images. The
	// check and the insert are atomic.
	InsertImage(ctx context.Context, owner ImageOwner, img domain.Image, limit int) (domain.Image, error)
	// DeleteImage removes the image only if it belongs to owner and returns
	// the removed row; otherwise it fails with domain.ErrNotFound.
	DeleteImage(ctx context.Context, owner ImageOwner, id domain.ImageID) (domain.Image, error)
}

// UserRepository persists the profiles of Telegram users.
type UserRepository interface {
	GetUser(ctx context.Context, id domain.UserID) (domain.User, error)
	// ListUsers returns every stored user ordered by ID.
	ListUsers(ctx context.Context) ([]domain.User, error)
	// UpsertUser stores u. HasChat never goes back from true to false.
	UpsertUser(ctx context.Context, u domain.User) error
}

// ImageOwner names the wish or the recipe an image belongs to. Exactly one
// of the IDs is set; build it with WishImages or RecipeImages.
type ImageOwner struct {
	Wish   domain.WishID
	Recipe domain.RecipeID
}

// WishImages is the owner value for the images of a wish.
func WishImages(id domain.WishID) ImageOwner { return ImageOwner{Wish: id} }

// RecipeImages is the owner value for the images of a recipe.
func RecipeImages(id domain.RecipeID) ImageOwner { return ImageOwner{Recipe: id} }

// Validate reports whether exactly one owner is set.
func (o ImageOwner) Validate() error {
	if (o.Wish != 0) == (o.Recipe != 0) {
		return errors.New("image owner must be exactly one wish or one recipe")
	}
	return nil
}

// MediaStore keeps processed image files. Keys are generated by the store
// and never derived from user input.
type MediaStore interface {
	// Save validates src, re-encodes it into every ImageVariant and returns
	// the new key. Unacceptable input fails with domain.ErrImageTooLarge or
	// domain.ErrImageUnsupported.
	Save(ctx context.Context, src io.Reader) (StoredImage, error)
	// Open returns one stored variant, or domain.ErrNotFound.
	Open(key string, v ImageVariant) (ImageFile, error)
	// Delete removes every variant of key. Missing files are not an error.
	Delete(key string) error
}

// StoredImage describes the files written by MediaStore.Save. Width, Height
// and Bytes describe the full variant.
type StoredImage struct {
	Key    string
	Width  int
	Height int
	Bytes  int64
}
