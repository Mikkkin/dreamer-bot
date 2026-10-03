// Package service implements the use cases of the wishlist. The bot and the
// HTTP API are thin adapters over the interfaces declared here, so every
// business rule lives exactly once.
package service

import (
	"context"
	"io"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Wishes is the use-case API for wishes. Every mutating method takes the
// acting user so that notifications and authorship are attributed correctly.
type Wishes interface {
	Create(ctx context.Context, actor domain.UserID, d domain.WishDraft) (domain.Wish, error)
	Get(ctx context.Context, id domain.WishID) (domain.Wish, error)
	// List returns wishes newest first, including their images.
	List(ctx context.Context, f domain.WishFilter) ([]domain.Wish, error)
	Update(ctx context.Context, actor domain.UserID, id domain.WishID, p domain.WishPatch) (domain.Wish, error)
	SetStatus(ctx context.Context, actor domain.UserID, id domain.WishID, s domain.Status) (domain.Wish, error)
	// Delete removes the wish together with its image files.
	Delete(ctx context.Context, actor domain.UserID, id domain.WishID) error
	// AddImage validates, re-encodes and stores an image read from src.
	AddImage(ctx context.Context, actor domain.UserID, id domain.WishID, src io.Reader) (domain.Image, error)
	RemoveImage(ctx context.Context, actor domain.UserID, id domain.WishID, img domain.ImageID) error
	// AddSaving records money put aside for the wish («Копим»). A wish that is
	// still «Хотим» moves to «Копим». Partners are notified.
	AddSaving(ctx context.Context, actor domain.UserID, id domain.WishID, amount domain.Money, note string) (domain.Saving, error)
	// ListSavings returns the wish's contributions, newest first.
	ListSavings(ctx context.Context, id domain.WishID) ([]domain.Saving, error)
	RemoveSaving(ctx context.Context, actor domain.UserID, id domain.WishID, saving domain.SavingID) error
}

// Recipes is the use-case API for the flat «Что приготовить» list.
type Recipes interface {
	Create(ctx context.Context, actor domain.UserID, d domain.RecipeDraft) (domain.Recipe, error)
	Get(ctx context.Context, id domain.RecipeID) (domain.Recipe, error)
	// List returns recipes newest first, including their images.
	List(ctx context.Context, f domain.RecipeFilter) ([]domain.Recipe, error)
	// Random picks one recipe for «что приготовить?»; ErrNotFound when empty.
	Random(ctx context.Context) (domain.Recipe, error)
	Update(ctx context.Context, actor domain.UserID, id domain.RecipeID, p domain.RecipePatch) (domain.Recipe, error)
	// Delete removes the recipe together with its image files.
	Delete(ctx context.Context, actor domain.UserID, id domain.RecipeID) error
	AddImage(ctx context.Context, actor domain.UserID, id domain.RecipeID, src io.Reader) (domain.Image, error)
	RemoveImage(ctx context.Context, actor domain.UserID, id domain.RecipeID, img domain.ImageID) error
	// Cook records that the recipe was cooked now, optionally with the
	// actor's rating. The recipe stays in the list. Partners are notified.
	Cook(ctx context.Context, actor domain.UserID, id domain.RecipeID, rating *RatingInput) (domain.Cook, error)
	// Rate sets (or replaces) the actor's rating of one cooking.
	Rate(ctx context.Context, actor domain.UserID, id domain.RecipeID, cook domain.CookID, in RatingInput) (domain.Cook, error)
	// ListCooks returns the cooking history, newest first, with ratings.
	ListCooks(ctx context.Context, id domain.RecipeID) ([]domain.Cook, error)
	RemoveCook(ctx context.Context, actor domain.UserID, id domain.RecipeID, cook domain.CookID) error
	// Import creates a recipe from an Instagram post link or a pasted
	// caption, exactly one of in.URL and in.Text. The draft goes through
	// the same validation and storage as Create, and the post's cover is
	// stored like an uploaded photo (a failure there is only a warning).
	// Partners are told through Notifier.RecipeImported. A post imported
	// before is not imported again: its recipe comes back with
	// ImportReport.Duplicate set. Errors: a validation error on field url
	// or text, ErrNotARecipe (ErrRecipeInVideo for a reel whose recipe is
	// probably only in the video), or domain.ErrExternalUnavailable when
	// Instagram does not give the post.
	Import(ctx context.Context, actor domain.UserID, in ImportInput) (domain.Recipe, ImportReport, error)
}

// ImportInput names what to import: the link of an Instagram post or reel
// (any form of it, with tracking parameters or text around it), or the
// text of a recipe. Exactly one is set.
type ImportInput struct {
	URL  string
	Text string
	// Link is optional with Text: the post the text was copied from (its
	// caption, when Instagram did not give the post). The recipe gets it,
	// and a post imported before is found as with URL.
	Link string
}

// ImportReport tells how an imported recipe was made. Source is
// ImportSourceInstagram or ImportSourceText; Parser is ImportParserRules,
// ImportParserLLM or ImportParserVideo; Confidence is the parser's score
// (0..1); Image reports that the recipe got the post's cover; Warnings are
// short Russian notes for the user (lines that were not read, a range
// shortened to its lower bound…). Duplicate means the post had been
// imported before: nothing was created, and the other fields describe the
// existing recipe only as far as Image goes.
type ImportReport struct {
	Source     string
	Parser     string
	Confidence float64
	Image      bool
	Warnings   []string
	Duplicate  bool
}

// RatingInput is a person's stars (1..5) and optional comment.
type RatingInput struct {
	Stars   int
	Comment string
}

// RecipeTags manages the cuisine and course tags of recipes.
type RecipeTags interface {
	// List returns all tags ordered by kind, then position.
	List(ctx context.Context) ([]domain.RecipeTag, error)
	Create(ctx context.Context, actor domain.UserID, kind domain.TagKind, name, emoji string) (domain.RecipeTag, error)
	Update(ctx context.Context, actor domain.UserID, id domain.RecipeTagID, p domain.RecipeTagPatch) (domain.RecipeTag, error)
	// Delete removes the tag; recipes keep existing without it.
	Delete(ctx context.Context, actor domain.UserID, id domain.RecipeTagID) error
}

// Shopping manages the couple's shared shopping list.
type Shopping interface {
	// List returns unchecked items first, then checked, oldest first.
	List(ctx context.Context) ([]domain.ShoppingItem, error)
	// Add appends items, merging each into an unchecked item with the same
	// name and unit when possible.
	Add(ctx context.Context, actor domain.UserID, drafts []domain.ShoppingDraft) ([]domain.ShoppingItem, error)
	// AddFromRecipe adds the recipe's ingredients at the given positions
	// (nil = all) and returns the added or merged items.
	AddFromRecipe(ctx context.Context, actor domain.UserID, id domain.RecipeID, positions []int) ([]domain.ShoppingItem, error)
	Update(ctx context.Context, actor domain.UserID, id domain.ShoppingItemID, p domain.ShoppingPatch) (domain.ShoppingItem, error)
	Delete(ctx context.Context, actor domain.UserID, id domain.ShoppingItemID) error
	// ClearChecked removes every checked item and reports how many.
	ClearChecked(ctx context.Context, actor domain.UserID) (int, error)
}

// Images serves stored images regardless of whether a wish or a recipe owns them.
type Images interface {
	// Open returns the stored bytes of one image variant.
	Open(ctx context.Context, id domain.ImageID, v ImageVariant) (ImageFile, error)
}

// Categories is the use-case API for categories.
type Categories interface {
	List(ctx context.Context) ([]domain.Category, error)
	Get(ctx context.Context, id domain.CategoryID) (domain.Category, error)
	Create(ctx context.Context, actor domain.UserID, name, emoji string) (domain.Category, error)
	// Update applies a partial change atomically (no lost update when both
	// partners edit the same category at once).
	Update(ctx context.Context, actor domain.UserID, id domain.CategoryID, p domain.CategoryPatch) (domain.Category, error)
	// Delete removes the category; its wishes stay and become uncategorized.
	Delete(ctx context.Context, actor domain.UserID, id domain.CategoryID) error
}

// Stats computes the per-category summary.
type Stats interface {
	Compute(ctx context.Context) (domain.Stats, error)
}

// Users keeps the profiles of whitelisted users seen by the bot or the API.
type Users interface {
	// Touch upserts the profile. HasChat is sticky: once true it stays true.
	Touch(ctx context.Context, u domain.User) error
	Get(ctx context.Context, id domain.UserID) (domain.User, error)
	// List returns every known whitelisted user (used to render author names).
	List(ctx context.Context) ([]domain.User, error)
	// Partners returns the other whitelisted users the bot can message.
	Partners(ctx context.Context, of domain.UserID) ([]domain.User, error)
}

// Recipients names who acted and who should be told about it.
type Recipients struct {
	Actor domain.User
	To    []domain.User
}

// Notifier tells partners about changes. The service resolves recipients
// (whitelisted partners with an open chat) and calls the notifier only when
// To is non-empty. Implementations must return quickly (deliver
// asynchronously) and must never fail the originating use case.
type Notifier interface {
	WishCreated(ctx context.Context, r Recipients, w domain.Wish)
	WishFulfilled(ctx context.Context, r Recipients, w domain.Wish)
	RecipeCreated(ctx context.Context, r Recipients, rec domain.Recipe)
	// RecipeImported announces a recipe made by Recipes.Import, cover
	// included. The importer reviews it right away, so implementations
	// hold the notice back until the recipe has stayed unchanged for a
	// while (edits restart the wait and are not announced on their own)
	// and drop it when the recipe is deleted meanwhile.
	RecipeImported(ctx context.Context, r Recipients, rec domain.Recipe)
	// RecipeUpdated fires on every change of a recipe's content (fields,
	// tags, ingredients, КБЖУ, photos). Implementations coalesce bursts (an
	// edit is several API calls) into one message per recipe.
	RecipeUpdated(ctx context.Context, r Recipients, rec domain.Recipe)
	RecipeCooked(ctx context.Context, r Recipients, rec domain.Recipe, cook domain.Cook)
	RecipeRated(ctx context.Context, r Recipients, rec domain.Recipe, cook domain.Cook, rating domain.Rating)
	WishSaved(ctx context.Context, r Recipients, w domain.Wish, s domain.Saving)
}

// ImageVariant selects the stored rendition of an image.
type ImageVariant string

const (
	VariantThumb ImageVariant = "thumb" // 480×600 (4:5) cover crop, for cards
	VariantFull  ImageVariant = "full"  // width ≤ 1600 px and ≤ 12 MP, so tall recipe screenshots stay readable
)

// ParseImageVariant validates a variant name coming from a URL.
func ParseImageVariant(raw string) (ImageVariant, bool) {
	switch v := ImageVariant(raw); v {
	case VariantThumb, VariantFull:
		return v, true
	default:
		return "", false
	}
}

// ImageFile is an opened stored image ready to be served.
type ImageFile struct {
	Content     io.ReadSeekCloser
	ContentType string
	ModTime     time.Time
	Size        int64
}
