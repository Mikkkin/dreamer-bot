package httpapi

import (
	"net/http"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func (s *server) listRecipeTags(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	tags, err := s.svc.RecipeTags.List(r.Context())
	if err != nil {
		return err
	}
	out := make([]recipeTagJSON, len(tags))
	for i, t := range tags {
		out[i] = recipeTagOf(t)
	}
	writeJSON(w, http.StatusOK, struct {
		Tags []recipeTagJSON `json:"tags"`
	}{out})
	return nil
}

func (s *server) createRecipeTag(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	var in struct {
		Kind  string `json:"kind"`
		Name  string `json:"name"`
		Emoji string `json:"emoji"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	kind, err := domain.ParseTagKind(in.Kind)
	if err != nil {
		return err
	}
	t, err := s.svc.RecipeTags.Create(r.Context(), u.ID, kind, in.Name, in.Emoji)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, recipeTagOf(t))
	return nil
}

// patchRecipeTag accepts name, emoji or both, like categories; the kind of
// a tag never changes.
func (s *server) patchRecipeTag(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeTagID](r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Name  *string `json:"name"`
		Emoji *string `json:"emoji"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	var patch domain.RecipeTagPatch
	if in.Name != nil {
		patch.Name = domain.Some(*in.Name)
	}
	if in.Emoji != nil {
		patch.Emoji = domain.Some(*in.Emoji)
	}
	t, err := s.svc.RecipeTags.Update(r.Context(), u.ID, id, patch)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, recipeTagOf(t))
	return nil
}

func (s *server) deleteRecipeTag(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeTagID](r, "id")
	if err != nil {
		return err
	}
	if err := s.svc.RecipeTags.Delete(r.Context(), u.ID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
