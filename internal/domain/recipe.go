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
// recipe, screenshots, or any combination of them. It is grouped by cuisine
// and course tags, may carry ingredients and КБЖУ, and keeps a cooking
// history with ratings; cooking never removes it from the list.
type Recipe struct {
	ID          RecipeID
	Title       string
	Link        *string
	Body        string
	CuisineID   *RecipeTagID  // at most one cuisine
	CourseIDs   []RecipeTagID // meals / courses, any number up to MaxCoursesPerRecipe
	Ingredients []Ingredient
	// Servings is how many portions the recipe yields (1..MaxServings), nil
	// when unknown. The ingredient amounts are for all of them.
	Servings  *int
	Nutrition *Nutrition // nil = КБЖУ not specified
	AuthorID  UserID
	Images    []Image
	// Cooking is filled by storage from the cooking history; it is never
	// set by callers and survives edits.
	Cooking   CookingSummary
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RecipeDraft is the input for a new recipe. Only Title is required. When
// Servings is nil, the servings of Nutrition (if any) are used, as clients
// that predate recipe-level servings send them there.
type RecipeDraft struct {
	Title       string
	Link        *string
	Body        string
	CuisineID   *RecipeTagID
	CourseIDs   []RecipeTagID
	Ingredients []Ingredient
	Servings    *int
	Nutrition   *Nutrition
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
	courses, err := normalizeCourses(d.CourseIDs)
	if err != nil {
		return Recipe{}, err
	}
	ingredients, err := NormalizeIngredients(d.Ingredients)
	if err != nil {
		return Recipe{}, err
	}
	nutrition, err := validateOptionalNutrition(d.Nutrition)
	if err != nil {
		return Recipe{}, err
	}
	servings := d.Servings
	if servings == nil && nutrition != nil && nutrition.Servings > 0 {
		servings = &nutrition.Servings
	}
	if servings, err = normalizeServings(servings); err != nil {
		return Recipe{}, err
	}
	now = now.UTC()
	return Recipe{
		Title:       title,
		Link:        link,
		Body:        body,
		CuisineID:   d.CuisineID,
		CourseIDs:   courses,
		Ingredients: ingredients,
		Servings:    servings,
		Nutrition:   withServings(nutrition, servings),
		AuthorID:    author,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// RecipePatch is a partial update; only fields with Set=true change. A
// Nutrition patch without servings keeps the recipe's servings; with
// servings and no Servings patch, it sets them (see RecipeDraft).
type RecipePatch struct {
	Title       Optional[string]
	Link        Optional[*string]
	Body        Optional[string]
	CuisineID   Optional[*RecipeTagID]
	CourseIDs   Optional[[]RecipeTagID]
	Ingredients Optional[[]Ingredient]
	Servings    Optional[*int]
	Nutrition   Optional[*Nutrition]
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
	if p.CuisineID.Set {
		next.CuisineID = p.CuisineID.Value
	}
	if p.CourseIDs.Set {
		courses, err := normalizeCourses(p.CourseIDs.Value)
		if err != nil {
			return err
		}
		next.CourseIDs = courses
	}
	if p.Ingredients.Set {
		ingredients, err := NormalizeIngredients(p.Ingredients.Value)
		if err != nil {
			return err
		}
		next.Ingredients = ingredients
	}
	servings := p.Servings
	if p.Nutrition.Set {
		nutrition, err := validateOptionalNutrition(p.Nutrition.Value)
		if err != nil {
			return err
		}
		next.Nutrition = nutrition
		if !servings.Set && nutrition != nil && nutrition.Servings > 0 {
			servings = Some(&nutrition.Servings)
		}
	}
	if servings.Set {
		s, err := normalizeServings(servings.Value)
		if err != nil {
			return err
		}
		next.Servings = s
	}
	next.Nutrition = withServings(next.Nutrition, next.Servings)
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
// substring of the title or the body. CuisineID and CourseID select recipes
// with that tag; zero values mean "no filter".
type RecipeFilter struct {
	Query     string
	CuisineID RecipeTagID
	CourseID  RecipeTagID
}

// normalizeServings validates the optional number of servings and copies
// it, so a recipe never shares it with its input.
func normalizeServings(s *int) (*int, error) {
	if s == nil {
		return nil, nil
	}
	if *s < 1 || *s > MaxServings {
		return nil, invalid("servings", "порций от 1 до 50")
	}
	v := *s
	return &v, nil
}

// withServings returns a copy of n whose servings mirror the recipe's.
func withServings(n *Nutrition, servings *int) *Nutrition {
	if n == nil {
		return nil
	}
	v := *n
	v.Servings = 0
	if servings != nil {
		v.Servings = *servings
	}
	return &v
}

func validateOptionalNutrition(n *Nutrition) (*Nutrition, error) {
	if n == nil {
		return nil, nil
	}
	if err := n.Validate(); err != nil {
		return nil, err
	}
	v := *n
	return &v, nil
}
