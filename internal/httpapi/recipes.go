package httpapi

import (
	"io"
	"net/http"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type recipeInput struct {
	Title string  `json:"title"`
	Link  *string `json:"link"`
	Body  string  `json:"body"`
}

func (s *server) listRecipes(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	query, err := searchQuery(r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	recipes, err := s.svc.Recipes.List(r.Context(), domain.RecipeFilter{Query: query})
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
	recipe, err := s.svc.Recipes.Create(r.Context(), u.ID, domain.RecipeDraft{Title: in.Title, Link: in.Link, Body: in.Body})
	if err != nil {
		return err
	}
	return s.writeRecipe(w, r, http.StatusCreated, recipe)
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
	fields, err := decodePatch(w, r, "title", "link", "body")
	if err != nil {
		return err
	}
	var p domain.RecipePatch
	for _, key := range fields.keys() {
		raw := fields[key]
		switch key {
		case "title":
			p.Title, err = patchValue[string](raw, key)
		case "link":
			p.Link, err = patchNullable[string](raw, key)
		case "body":
			p.Body, err = patchValue[string](raw, key)
		}
		if err != nil {
			return err
		}
	}
	recipe, err := s.svc.Recipes.Update(r.Context(), u.ID, id, p)
	if err != nil {
		return err
	}
	return s.writeRecipe(w, r, http.StatusOK, recipe)
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
