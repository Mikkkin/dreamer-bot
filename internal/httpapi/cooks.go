package httpapi

import (
	"net/http"
	"strings"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// cookRecipe records «Приготовили». Stars are optional: {} or
// {"stars":null} cooks without a rating.
func (s *server) cookRecipe(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Stars   *int   `json:"stars"`
		Comment string `json:"comment"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	var rating *service.RatingInput
	switch {
	case in.Stars != nil:
		ri, err := s.rating(u.ID, *in.Stars, in.Comment)
		if err != nil {
			return err
		}
		rating = &ri
	case strings.TrimSpace(in.Comment) != "":
		return badRequest("stars", "Поставьте оценку, чтобы оставить комментарий.")
	}
	cook, err := s.svc.Recipes.Cook(r.Context(), u.ID, id, rating)
	if err != nil {
		return err
	}
	return s.writeCook(w, r, http.StatusCreated, cook)
}

func (s *server) listCooks(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	cooks, err := s.svc.Recipes.ListCooks(r.Context(), id)
	if err != nil {
		return err
	}
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, struct {
		Cooks []cookJSON `json:"cooks"`
	}{p.cooks(cooks)})
	return nil
}

// rateCook sets or replaces the caller's rating of one cooking.
func (s *server) rateCook(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	cookID, err := pathID[domain.CookID](r, "cookId")
	if err != nil {
		return err
	}
	var in struct {
		Stars   int    `json:"stars"`
		Comment string `json:"comment"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	rating, err := s.rating(u.ID, in.Stars, in.Comment)
	if err != nil {
		return err
	}
	cook, err := s.svc.Recipes.Rate(r.Context(), u.ID, id, cookID, rating)
	if err != nil {
		return err
	}
	return s.writeCook(w, r, http.StatusOK, cook)
}

func (s *server) removeCook(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.RecipeID](r, "id")
	if err != nil {
		return err
	}
	cookID, err := pathID[domain.CookID](r, "cookId")
	if err != nil {
		return err
	}
	if err := s.svc.Recipes.RemoveCook(r.Context(), u.ID, id, cookID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// rating checks stars (1..5) and the comment with the domain constructor
// before any use case runs; the service applies the same rule again.
func (s *server) rating(user domain.UserID, stars int, comment string) (service.RatingInput, error) {
	if _, err := domain.NewRating(user, stars, comment, s.now()); err != nil {
		return service.RatingInput{}, err
	}
	return service.RatingInput{Stars: stars, Comment: comment}, nil
}

func (s *server) writeCook(w http.ResponseWriter, r *http.Request, status int, cook domain.Cook) error {
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, status, p.cook(cook))
	return nil
}
