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
	SavingRepository
	RecipeRepository
	RecipeTagRepository
	CookRepository
	ShoppingRepository
	CategoryRepository
	ImageRepository
	UserRepository
}

// WishRepository persists wishes. Every returned wish carries its images
// ordered by position and, in Saved, the sum of its savings.
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

// SavingRepository persists the savings of wishes («Копим»).
type SavingRepository interface {
	// InsertSaving loads the wish (with Saved), lets fn build the saving and
	// change the wish, then stores both in one transaction, so concurrent
	// savings cannot bypass the one-currency rule. It returns the wish as
	// stored afterwards (Saved includes the new saving) and the saving with
	// its ID. An error from fn aborts everything and is returned unchanged;
	// fn must not keep the pointer or touch storage.
	InsertSaving(ctx context.Context, id domain.WishID, fn func(*domain.Wish) (domain.Saving, error)) (domain.Wish, domain.Saving, error)
	// ListSavings returns the savings of the wish, newest first, or
	// domain.ErrNotFound when the wish does not exist.
	ListSavings(ctx context.Context, id domain.WishID) ([]domain.Saving, error)
	// DeleteSaving removes the saving only if it belongs to the wish;
	// otherwise it fails with domain.ErrNotFound.
	DeleteSaving(ctx context.Context, wish domain.WishID, id domain.SavingID) error
}

// RecipeRepository persists recipes. Every returned recipe carries its
// images ordered by position, its course tags and ingredients in their
// order (empty, never nil), its КБЖУ and its cooking summary.
type RecipeRepository interface {
	// InsertRecipe stores the recipe with its course tags and ingredients.
	// InsertRecipe and ModifyRecipe fail with domain.ErrConflict when the
	// recipe refers to a tag that no longer exists (deleted after the use
	// case checked it).
	InsertRecipe(ctx context.Context, r domain.Recipe) (domain.Recipe, error)
	GetRecipe(ctx context.Context, id domain.RecipeID) (domain.Recipe, error)
	// ListRecipes returns the recipes matching f, newest first. Query is a
	// Unicode-aware case-insensitive substring of the title or the body;
	// CuisineID and CourseID select recipes carrying that tag.
	ListRecipes(ctx context.Context, f domain.RecipeFilter) ([]domain.Recipe, error)
	// RandomRecipe returns a uniformly random recipe, or domain.ErrNotFound
	// when there are none.
	RandomRecipe(ctx context.Context) (domain.Recipe, error)
	// ModifyRecipe has the same contract as WishRepository.ModifyWish.
	ModifyRecipe(ctx context.Context, id domain.RecipeID, fn func(*domain.Recipe) error) (domain.Recipe, error)
	DeleteRecipe(ctx context.Context, id domain.RecipeID) (imageKeys []string, err error)
	CountRecipes(ctx context.Context) (int, error)
}

// RecipeTagRepository persists the cuisine and course tags of recipes.
// Names are unique per kind ignoring case (Unicode-aware); a clash is
// reported as domain.ErrConflict.
type RecipeTagRepository interface {
	// ListRecipeTags returns every tag: cuisines first, then courses, each
	// in display order.
	ListRecipeTags(ctx context.Context) ([]domain.RecipeTag, error)
	// InsertRecipeTag stores t at the end of its kind's display order.
	InsertRecipeTag(ctx context.Context, t domain.RecipeTag) (domain.RecipeTag, error)
	// ModifyRecipeTag loads the tag, applies fn and saves the name and the
	// emoji in one transaction.
	ModifyRecipeTag(ctx context.Context, id domain.RecipeTagID, fn func(*domain.RecipeTag) error) (domain.RecipeTag, error)
	// DeleteRecipeTag removes the tag; recipes lose it and stay.
	DeleteRecipeTag(ctx context.Context, id domain.RecipeTagID) error
}

// CookRepository persists the cooking history of recipes and its ratings.
// Every returned cook carries its ratings, oldest first.
type CookRepository interface {
	// InsertCook stores a cooking of c.RecipeID together with c.Ratings, or
	// fails with domain.ErrNotFound when the recipe does not exist.
	InsertCook(ctx context.Context, c domain.Cook) (domain.Cook, error)
	// UpsertRating sets (or replaces) r.UserID's rating of the cooking and
	// returns the cooking. It fails with domain.ErrNotFound unless the
	// cooking exists and belongs to the recipe.
	UpsertRating(ctx context.Context, recipe domain.RecipeID, cook domain.CookID, r domain.Rating) (domain.Cook, error)
	// ListCooks returns the cooking history of the recipe, newest first, or
	// domain.ErrNotFound when the recipe does not exist.
	ListCooks(ctx context.Context, recipe domain.RecipeID) ([]domain.Cook, error)
	// DeleteCook removes the cooking with its ratings only if it belongs to
	// the recipe; otherwise it fails with domain.ErrNotFound.
	DeleteCook(ctx context.Context, recipe domain.RecipeID, cook domain.CookID) error
	// CountCooks returns how many times any recipe was cooked.
	CountCooks(ctx context.Context) (int, error)
}

// ShoppingRepository persists the shared shopping list.
type ShoppingRepository interface {
	// ListShoppingItems returns unchecked items first, then checked ones,
	// each oldest first.
	ListShoppingItems(ctx context.Context) ([]domain.ShoppingItem, error)
	// AddShoppingItems stores items in one transaction. For every item,
	// merge is offered each unchecked stored item with the same case-folded
	// name and the same unit, oldest first; when merge reports true, that
	// stored item is saved instead of inserting a new one. Items added
	// earlier in the same call are candidates too. It fails with
	// domain.ErrLimitExceeded when an insert would grow the list beyond
	// limit, and with domain.ErrNotFound when an item names a recipe that
	// does not exist. It returns every added or merged item once, in the
	// order they were first touched, in their final state.
	AddShoppingItems(ctx context.Context, items []domain.ShoppingItem, limit int,
		merge func(stored *domain.ShoppingItem, added domain.ShoppingItem) bool) ([]domain.ShoppingItem, error)
	// ModifyShoppingItem loads the item, applies fn and saves it in one
	// transaction.
	ModifyShoppingItem(ctx context.Context, id domain.ShoppingItemID, fn func(*domain.ShoppingItem) error) (domain.ShoppingItem, error)
	DeleteShoppingItem(ctx context.Context, id domain.ShoppingItemID) error
	// DeleteCheckedShoppingItems removes every checked item and reports how
	// many were removed.
	DeleteCheckedShoppingItems(ctx context.Context) (int, error)
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

// RecipeImporter reads recipes from outside the service: Instagram posts
// and pasted captions (internal/recipeimport behind an adapter). It only
// builds drafts; storing them is the use case's job.
type RecipeImporter interface {
	// Canonical returns the canonical link of the Instagram post or reel
	// named in raw, or false when raw holds no such link. Two links of the
	// same post give the same canonical link.
	Canonical(raw string) (string, bool)
	// PostKey identifies the post named in raw regardless of the link's
	// form: /p/ and /reel/ links of one post share it. False when raw
	// holds no post link.
	PostKey(raw string) (string, bool)
	// FromURL imports the post behind an Instagram link for actor. When
	// the caption holds no usable recipe, a model may watch the reel's
	// video, within actor's daily quota of such imports. Errors wrap
	// ErrNotARecipe (ErrRecipeInVideo when the recipe is probably only in
	// a video nobody can watch) or domain.ErrExternalUnavailable.
	FromURL(ctx context.Context, actor domain.UserID, raw string) (ImportResult, error)
	// FromText imports a pasted caption or recipe text; errors wrap
	// ErrNotARecipe.
	FromText(ctx context.Context, text string) (ImportResult, error)
}

// ImportResult is a draft for the normal recipe validation, the cover
// picture as downloaded (nil when there is none) and the report. The
// report's Image tells whether a cover was downloaded.
type ImportResult struct {
	Draft  domain.RecipeDraft
	Image  []byte
	Report ImportReport
}

// StoredImage describes the files written by MediaStore.Save. Width, Height
// and Bytes describe the full variant.
type StoredImage struct {
	Key    string
	Width  int
	Height int
	Bytes  int64
}
