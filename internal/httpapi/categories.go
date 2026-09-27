package httpapi

import (
	"net/http"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func (s *server) listCategories(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	categories, err := s.svc.Categories.List(r.Context())
	if err != nil {
		return err
	}
	out := make([]categoryJSON, len(categories))
	for i, c := range categories {
		out[i] = categoryOf(c)
	}
	writeJSON(w, http.StatusOK, struct {
		Categories []categoryJSON `json:"categories"`
	}{out})
	return nil
}

func (s *server) createCategory(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	var in struct {
		Name  string `json:"name"`
		Emoji string `json:"emoji"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	c, err := s.svc.Categories.Create(r.Context(), u.ID, in.Name, in.Emoji)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, categoryOf(c))
	return nil
}

// patchCategory accepts name, emoji or both; an absent field keeps its value.
func (s *server) patchCategory(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.CategoryID](r, "id")
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
	var patch domain.CategoryPatch
	if in.Name != nil {
		patch.Name = domain.Some(*in.Name)
	}
	if in.Emoji != nil {
		patch.Emoji = domain.Some(*in.Emoji)
	}
	c, err := s.svc.Categories.Update(r.Context(), u.ID, id, patch)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, categoryOf(c))
	return nil
}

func (s *server) deleteCategory(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.CategoryID](r, "id")
	if err != nil {
		return err
	}
	if err := s.svc.Categories.Delete(r.Context(), u.ID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
