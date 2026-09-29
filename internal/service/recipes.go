package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type recipeService struct{ *core }

func (s recipeService) Create(ctx context.Context, actor domain.UserID, d domain.RecipeDraft) (domain.Recipe, error) {
	r, err := domain.NewRecipe(d, actor, s.now())
	if err != nil {
		return domain.Recipe{}, err
	}
	if err := s.checkTags(ctx, domain.Some(r.CuisineID), domain.Some(r.CourseIDs)); err != nil {
		return domain.Recipe{}, err
	}
	r, err = s.repos.InsertRecipe(ctx, r)
	if err != nil {
		return domain.Recipe{}, err
	}
	s.log.Info("recipe created", slog.Int64("recipe_id", int64(r.ID)), slog.Int64("user_id", int64(actor)))
	s.notify(ctx, "recipe_created", actor, func(ctx context.Context, rc Recipients) {
		s.notifier.RecipeCreated(ctx, rc, r)
	})
	return r, nil
}

func (s recipeService) Get(ctx context.Context, id domain.RecipeID) (domain.Recipe, error) {
	return s.repos.GetRecipe(ctx, id)
}

func (s recipeService) List(ctx context.Context, f domain.RecipeFilter) ([]domain.Recipe, error) {
	f.Query = strings.TrimSpace(f.Query)
	return s.repos.ListRecipes(ctx, f)
}

func (s recipeService) Random(ctx context.Context) (domain.Recipe, error) {
	return s.repos.RandomRecipe(ctx)
}

func (s recipeService) Update(ctx context.Context, actor domain.UserID, id domain.RecipeID, p domain.RecipePatch) (domain.Recipe, error) {
	if err := s.checkTags(ctx, p.CuisineID, p.CourseIDs); err != nil {
		return domain.Recipe{}, err
	}
	now := s.now()
	var unchanged domain.Recipe
	r, err := s.repos.ModifyRecipe(ctx, id, func(r *domain.Recipe) error {
		before := *r
		if err := r.Apply(p, now); err != nil {
			return err
		}
		if sameContent(before, *r) {
			unchanged = before
			return errUnchanged
		}
		return nil
	})
	if errors.Is(err, errUnchanged) {
		return unchanged, nil
	}
	if err != nil {
		return domain.Recipe{}, err
	}
	s.log.Info("recipe updated", slog.Int64("recipe_id", int64(id)), slog.Int64("user_id", int64(actor)))
	s.notify(ctx, "recipe_updated", actor, func(ctx context.Context, rc Recipients) {
		s.notifier.RecipeUpdated(ctx, rc, r)
	})
	return r, nil
}

// errUnchanged aborts ModifyRecipe when a patch changes nothing: the Mini
// App always sends the whole form, and saving it untouched must neither
// write nor tell the partner about a change.
var errUnchanged = errors.New("recipe unchanged")

// sameContent reports whether two versions of a recipe read the same.
// Photos, the cooking history and timestamps are not content here.
func sameContent(a, b domain.Recipe) bool {
	return a.Title == b.Title &&
		equalPtr(a.Link, b.Link) &&
		a.Body == b.Body &&
		equalPtr(a.CuisineID, b.CuisineID) &&
		slices.Equal(a.CourseIDs, b.CourseIDs) &&
		slices.EqualFunc(a.Ingredients, b.Ingredients, func(x, y domain.Ingredient) bool {
			return x.Name == y.Name && equalPtr(x.Quantity, y.Quantity)
		}) &&
		equalPtr(a.Nutrition, b.Nutrition)
}

// equalPtr compares optional values: both absent, or both present and equal.
func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s recipeService) Delete(ctx context.Context, actor domain.UserID, id domain.RecipeID) error {
	keys, err := s.repos.DeleteRecipe(ctx, id)
	if err != nil {
		return err
	}
	s.deleteFiles(keys...)
	s.log.Info("recipe deleted", slog.Int64("recipe_id", int64(id)), slog.Int64("user_id", int64(actor)))
	return nil
}

func (s recipeService) AddImage(ctx context.Context, actor domain.UserID, id domain.RecipeID, src io.Reader) (domain.Image, error) {
	r, err := s.repos.GetRecipe(ctx, id)
	if err != nil {
		return domain.Image{}, err
	}
	if !r.CanAddImage() {
		return domain.Image{}, domain.ErrLimitExceeded
	}
	img, err := s.storeImage(ctx, RecipeImages(id), domain.MaxImagesPerRecipe, src)
	if err != nil {
		return domain.Image{}, err
	}
	s.notifyUpdated(ctx, actor, id)
	return img, nil
}

func (s recipeService) RemoveImage(ctx context.Context, actor domain.UserID, id domain.RecipeID, img domain.ImageID) error {
	if err := s.removeImage(ctx, RecipeImages(id), img); err != nil {
		return err
	}
	s.notifyUpdated(ctx, actor, id)
	return nil
}

// notifyUpdated reports a change of the recipe's photos. The recipe is
// loaded only when somebody is to be told.
func (s recipeService) notifyUpdated(ctx context.Context, actor domain.UserID, id domain.RecipeID) {
	s.notify(ctx, "recipe_updated", actor, func(ctx context.Context, rc Recipients) {
		r, err := s.repos.GetRecipe(ctx, id)
		if err != nil {
			s.log.Warn("load recipe for notification", slog.Int64("recipe_id", int64(id)), slog.Any("error", err))
			return
		}
		s.notifier.RecipeUpdated(ctx, rc, r)
	})
}

func (s recipeService) Cook(ctx context.Context, actor domain.UserID, id domain.RecipeID, in *RatingInput) (domain.Cook, error) {
	now := s.now()
	c := domain.Cook{RecipeID: id, CookedBy: actor, CookedAt: now}
	if in != nil {
		rating, err := domain.NewRating(actor, in.Stars, in.Comment, now)
		if err != nil {
			return domain.Cook{}, err
		}
		c.Ratings = []domain.Rating{rating}
	}
	c, err := s.repos.InsertCook(ctx, c)
	if err != nil {
		return domain.Cook{}, err
	}
	s.log.Info("recipe cooked",
		slog.Int64("recipe_id", int64(id)),
		slog.Int64("cook_id", int64(c.ID)),
		slog.Int64("user_id", int64(actor)))
	s.notify(ctx, "recipe_cooked", actor, func(ctx context.Context, rc Recipients) {
		r, err := s.repos.GetRecipe(ctx, id)
		if err != nil {
			s.log.Warn("load recipe for notification", slog.Int64("recipe_id", int64(id)), slog.Any("error", err))
			return
		}
		s.notifier.RecipeCooked(ctx, rc, r, c)
	})
	return c, nil
}

func (s recipeService) Rate(ctx context.Context, actor domain.UserID, id domain.RecipeID, cook domain.CookID, in RatingInput) (domain.Cook, error) {
	rating, err := domain.NewRating(actor, in.Stars, in.Comment, s.now())
	if err != nil {
		return domain.Cook{}, err
	}
	c, err := s.repos.UpsertRating(ctx, id, cook, rating)
	if err != nil {
		return domain.Cook{}, err
	}
	s.log.Info("recipe rated",
		slog.Int64("recipe_id", int64(id)),
		slog.Int64("cook_id", int64(cook)),
		slog.Int("stars", rating.Stars),
		slog.Int64("user_id", int64(actor)))
	s.notify(ctx, "recipe_rated", actor, func(ctx context.Context, rc Recipients) {
		r, err := s.repos.GetRecipe(ctx, id)
		if err != nil {
			s.log.Warn("load recipe for notification", slog.Int64("recipe_id", int64(id)), slog.Any("error", err))
			return
		}
		s.notifier.RecipeRated(ctx, rc, r, c, rating)
	})
	return c, nil
}

func (s recipeService) ListCooks(ctx context.Context, id domain.RecipeID) ([]domain.Cook, error) {
	return s.repos.ListCooks(ctx, id)
}

func (s recipeService) RemoveCook(ctx context.Context, actor domain.UserID, id domain.RecipeID, cook domain.CookID) error {
	if err := s.repos.DeleteCook(ctx, id, cook); err != nil {
		return err
	}
	s.log.Info("recipe cook removed",
		slog.Int64("recipe_id", int64(id)),
		slog.Int64("cook_id", int64(cook)),
		slog.Int64("user_id", int64(actor)))
	return nil
}

// checkTags rejects a cuisine or courses that do not exist or have the
// wrong kind. Only the fields that are set are checked, and the tags are
// read once, only when there is something to check.
func (s recipeService) checkTags(ctx context.Context, cuisine domain.Optional[*domain.RecipeTagID], courses domain.Optional[[]domain.RecipeTagID]) error {
	checkCuisine := cuisine.Set && cuisine.Value != nil
	checkCourses := courses.Set && len(courses.Value) > 0
	if !checkCuisine && !checkCourses {
		return nil
	}
	tags, err := s.repos.ListRecipeTags(ctx)
	if err != nil {
		return err
	}
	kinds := make(map[domain.RecipeTagID]domain.TagKind, len(tags))
	for _, t := range tags {
		kinds[t.ID] = t.Kind
	}
	if checkCuisine && kinds[*cuisine.Value] != domain.TagCuisine {
		return &domain.ValidationError{Field: "cuisine_id", Message: "такой кухни нет"}
	}
	if checkCourses {
		for _, id := range courses.Value {
			if kinds[id] != domain.TagCourse {
				return &domain.ValidationError{Field: "course_ids", Message: "такого типа блюда нет"}
			}
		}
	}
	return nil
}
