package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type recipeInput struct {
	Title       string               `json:"title"`
	Link        *string              `json:"link"`
	Body        string               `json:"body"`
	CuisineID   *domain.RecipeTagID  `json:"cuisine_id"`
	CourseIDs   []domain.RecipeTagID `json:"course_ids"`
	Ingredients []itemInput          `json:"ingredients"`
	Servings    *int                 `json:"servings"`
	Nutrition   *nutritionInput      `json:"nutrition"`
}

// nutritionInput is КБЖУ per 100 g as decimal strings, plus the optional
// weight of the whole dish and the number of servings. The servings belong
// to the recipe: here they are only read when the top-level servings are
// absent (clients that predate them), see domain.RecipeDraft.
type nutritionInput struct {
	Kcal     string `json:"kcal"`
	Protein  string `json:"protein"`
	Fat      string `json:"fat"`
	Carbs    string `json:"carbs"`
	WeightG  *int   `json:"weight_g"`
	Servings *int   `json:"servings"`
}

func (s *server) listRecipes(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	filter, err := recipeFilter(r.URL.Query())
	if err != nil {
		return err
	}
	recipes, err := s.svc.Recipes.List(r.Context(), filter)
	if err != nil {
		return err
	}
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, struct {
		Recipes []recipeJSON `json:"recipes"`
	}{p.recipes(recipes)})
	return nil
}

func (s *server) randomRecipe(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	recipe, err := s.svc.Recipes.Random(r.Context())
	if err != nil {
		return err
	}
	return s.writeRecipe(w, r, http.StatusOK, recipe)
}

func (s *server) createRecipe(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	var in recipeInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	draft, err := recipeDraft(in)
	if err != nil {
		return err
	}
	recipe, err := s.svc.Recipes.Create(r.Context(), u.ID, draft)
	if err != nil {
		return err
	}
	return s.writeRecipe(w, r, http.StatusCreated, recipe)
}

func recipeDraft(in recipeInput) (domain.RecipeDraft, error) {
	d := domain.RecipeDraft{Title: in.Title, Link: in.Link, Body: in.Body, CuisineID: in.CuisineID, CourseIDs: in.CourseIDs, Servings: in.Servings}
	if err := checkCuisine(in.CuisineID); err != nil {
		return d, err
	}
	if err := checkCourses(in.CourseIDs); err != nil {
		return d, err
	}
	var err error
	if d.Ingredients, err = ingredientList.parse(in.Ingredients); err != nil {
		return d, err
	}
	d.Nutrition, err = parseNutrition(in.Nutrition)
	return d, err
}

func (s *server) getRecipe(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	recipe, err := s.svc.Recipes.Get(r.Context(), id)
	if err != nil {
		return err
	}
	return s.writeRecipe(w, r, http.StatusOK, recipe)
}

func (s *server) patchRecipe(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	fields, err := decodePatch(w, r, "title", "link", "body", "cuisine_id", "course_ids", "ingredients", "servings", "nutrition")
	if err != nil {
		return err
	}
	patch, err := recipePatch(fields)
	if err != nil {
		return err
	}
	recipe, err := s.svc.Recipes.Update(r.Context(), u.ID, id, patch)
	if err != nil {
		return err
	}
	return s.writeRecipe(w, r, http.StatusOK, recipe)
}

// recipePatch decodes a partial RecipeInput. null clears link, cuisine_id,
// servings and nutrition; for the lists (course_ids, ingredients) it means
// "empty".
func recipePatch(fields patchFields) (domain.RecipePatch, error) {
	var (
		p   domain.RecipePatch
		err error
	)
	for _, key := range fields.keys() {
		raw := fields[key]
		switch key {
		case "title":
			p.Title, err = patchValue[string](raw, key)
		case "link":
			p.Link, err = patchNullable[string](raw, key)
		case "body":
			p.Body, err = patchValue[string](raw, key)
		case "cuisine_id":
			if p.CuisineID, err = patchNullable[domain.RecipeTagID](raw, key); err == nil {
				err = checkCuisine(p.CuisineID.Value)
			}
		case "course_ids":
			if p.CourseIDs, err = patchValue[[]domain.RecipeTagID](raw, key); err == nil {
				err = checkCourses(p.CourseIDs.Value)
			}
		case "ingredients":
			p.Ingredients, err = patchIngredients(raw)
		case "servings":
			p.Servings, err = patchNullable[int](raw, key)
		case "nutrition":
			p.Nutrition, err = patchNutrition(raw)
		}
		if err != nil {
			return domain.RecipePatch{}, err
		}
	}
	return p, nil
}

func patchIngredients(raw json.RawMessage) (domain.Optional[[]domain.Ingredient], error) {
	in, err := patchValue[[]itemInput](raw, "ingredients")
	if err != nil {
		return domain.Optional[[]domain.Ingredient]{}, err
	}
	ings, err := ingredientList.parse(in.Value)
	if err != nil {
		return domain.Optional[[]domain.Ingredient]{}, err
	}
	return domain.Some(ings), nil
}

func patchNutrition(raw json.RawMessage) (domain.Optional[*domain.Nutrition], error) {
	in, err := patchNullable[nutritionInput](raw, "nutrition")
	if err != nil {
		return domain.Optional[*domain.Nutrition]{}, err
	}
	n, err := parseNutrition(in.Value)
	if err != nil {
		return domain.Optional[*domain.Nutrition]{}, err
	}
	return domain.Some(n), nil
}

// parseNutrition parses the decimals with the domain parser; nil stays nil
// (КБЖУ not specified).
func parseNutrition(in *nutritionInput) (*domain.Nutrition, error) {
	if in == nil {
		return nil, nil
	}
	weight, servings := 0, 0
	if in.WeightG != nil {
		weight = *in.WeightG
	}
	if in.Servings != nil {
		servings = *in.Servings
	}
	n, err := domain.NewNutrition(in.Kcal, in.Protein, in.Fat, in.Carbs, weight, servings)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// checkCuisine rejects ids that cannot name a tag; whether the tag exists
// and is a cuisine is checked by the service.
func checkCuisine(id *domain.RecipeTagID) error {
	if id != nil && *id <= 0 {
		return badRequest("cuisine_id", "Неизвестная кухня.")
	}
	return nil
}

// checkCourses bounds the list before the domain de-duplicates it, so a
// huge array of repeated ids cannot slip through.
func checkCourses(ids []domain.RecipeTagID) error {
	if len(ids) > domain.MaxCoursesPerRecipe {
		return badRequest("course_ids", "Не больше "+strconv.Itoa(domain.MaxCoursesPerRecipe)+" типов блюда на рецепт.")
	}
	return nil
}

func (s *server) deleteRecipe(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	if err := s.svc.Recipes.Delete(r.Context(), u.ID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) addRecipeImage(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	return s.receiveImage(w, r,
		func(src io.Reader) (domain.Image, error) { return s.svc.Recipes.AddImage(r.Context(), u.ID, id, src) },
		func(img domain.ImageID) error { return s.svc.Recipes.RemoveImage(r.Context(), u.ID, id, img) },
	)
}

func (s *server) removeRecipeImage(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	img, err := pathID[domain.ImageID](r, "imageId")
	if err != nil {
		return err
	}
	if err := s.svc.Recipes.RemoveImage(r.Context(), u.ID, id, img); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) writeRecipe(w http.ResponseWriter, r *http.Request, status int, recipe domain.Recipe) error {
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, status, p.recipe(recipe))
	return nil
}

// recipeFilter reads ?q=&cuisine=&course=; the tags are ids.
func recipeFilter(q url.Values) (domain.RecipeFilter, error) {
	var (
		f   domain.RecipeFilter
		err error
	)
	if f.Query, err = searchQuery(q.Get("q")); err != nil {
		return f, err
	}
	if f.CuisineID, err = tagParam(q, "cuisine", "Неизвестная кухня."); err != nil {
		return f, err
	}
	f.CourseID, err = tagParam(q, "course", "Неизвестный тип блюда.")
	return f, err
}

// tagParam reads an optional tag id from the query; absent means 0 ("no
// filter").
func tagParam(q url.Values, name, message string) (domain.RecipeTagID, error) {
	raw := q.Get(name)
	if raw == "" {
		return 0, nil
	}
	id, ok := parseID(raw)
	if !ok {
		return 0, badRequest(name, message)
	}
	return domain.RecipeTagID(id), nil
}
