package domain

import (
	"time"
	"unicode/utf8"
)

// RecipeID identifies a recipe.
type RecipeID int64

const (
	// MaxRecipeBodyLen caps a hand-written recipe.
	MaxRecipeBodyLen = 10000
	// MaxImagesPerRecipe caps screenshots and photos of one recipe.
	MaxImagesPerRecipe = 10
)

// Recipe is something to cook together: a link to a recipe, a hand-written
// recipe, screenshots, or any combination of them. Recipes are a flat list
// with no categories or statuses.
type Recipe struct {
	ID        RecipeID
	Title     string
	Link      *string
	Body      string
	AuthorID  UserID
	Images    []Image
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RecipeDraft is the input for a new recipe. Only Title is required.
type RecipeDraft struct {
	Title string
	Link  *string
	Body  string
}

// NewRecipe validates the draft and builds a recipe authored by author.
func NewRecipe(d RecipeDraft, author UserID, now time.Time) (Recipe, error) {
	title, err := NormalizeTitle(d.Title)
	if err != nil {
		return Recipe{}, err
	}
	link, err := normalizeOptionalLink(d.Link)
	if err != nil {
		return Recipe{}, err
	}
	body, err := NormalizeRecipeBody(d.Body)
	if err != nil {
		return Recipe{}, err
	}
	now = now.UTC()
	return Recipe{
		Title:     title,
		Link:      link,
		Body:      body,
		AuthorID:  author,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// RecipePatch is a partial update; only fields with Set=true change.
type RecipePatch struct {
	Title Optional[string]
	Link  Optional[*string]
	Body  Optional[string]
}

// Apply validates every set field first and only then mutates the recipe.
func (r *Recipe) Apply(p RecipePatch, now time.Time) error {
	next := *r
	if p.Title.Set {
		title, err := NormalizeTitle(p.Title.Value)
		if err != nil {
			return err
		}
		next.Title = title
	}
	if p.Link.Set {
		link, err := normalizeOptionalLink(p.Link.Value)
		if err != nil {
			return err
		}
		next.Link = link
	}
	if p.Body.Set {
		body, err := NormalizeRecipeBody(p.Body.Value)
		if err != nil {
			return err
		}
		next.Body = body
	}
	next.UpdatedAt = now.UTC()
	*r = next
	return nil
}

// CanAddImage reports whether one more image fits into the recipe.
func (r *Recipe) CanAddImage() bool { return len(r.Images) < MaxImagesPerRecipe }

// NormalizeRecipeBody validates the optional hand-written recipe text.
func NormalizeRecipeBody(raw string) (string, error) {
	s := cleanText(raw, true)
	if utf8.RuneCountInString(s) > MaxRecipeBodyLen {
		return "", invalid("body", "рецепт слишком длинный (максимум 10000 символов)")
	}
	return s, nil
}

// RecipeFilter narrows a recipe listing. Query is a case-insensitive
// substring of the title or the body.
type RecipeFilter struct {
	Query string
}
