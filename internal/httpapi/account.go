package httpapi

import (
	"net/http"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type currencyJSON struct {
	Code   string `json:"code"`
	Symbol string `json:"symbol"`
}

type limitsJSON struct {
	TitleMax             int   `json:"title_max"`
	NoteMax              int   `json:"note_max"`
	LinkMax              int   `json:"link_max"`
	ImagesPerWish        int   `json:"images_per_wish"`
	ImageMaxBytes        int64 `json:"image_max_bytes"`
	CategoryNameMax      int   `json:"category_name_max"`
	RecipeBodyMax        int   `json:"recipe_body_max"`
	ImagesPerRecipe      int   `json:"images_per_recipe"`
	IngredientsPerRecipe int   `json:"ingredients_per_recipe"`
	CoursesPerRecipe     int   `json:"courses_per_recipe"`
	ShoppingItemsMax     int   `json:"shopping_items_max"`
	ItemNameMax          int   `json:"item_name_max"`
	RatingCommentMax     int   `json:"rating_comment_max"`
	SavingNoteMax        int   `json:"saving_note_max"`
	DishWeightMaxG       int   `json:"dish_weight_max_g"`
	ServingsMax          int   `json:"servings_max"`
}

type meJSON struct {
	User            personJSON     `json:"user"`
	Partners        []personJSON   `json:"partners"`
	Currencies      []currencyJSON `json:"currencies"`
	DefaultCurrency string         `json:"default_currency"`
	Limits          limitsJSON     `json:"limits"`
	Units           []string       `json:"units"`
}

// me describes the signed-in user, the partners and the input limits the
// Mini App enforces client-side (the server enforces them again).
func (s *server) me(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	users, err := s.svc.Users.List(r.Context())
	if err != nil {
		return err
	}
	partners := []personJSON{}
	for _, other := range users {
		if other.ID != u.ID && s.whitelist.Allows(other.ID) {
			partners = append(partners, personJSON{ID: int64(other.ID), Name: other.DisplayName()})
		}
	}
	currencies := make([]currencyJSON, len(domain.Currencies))
	for i, c := range domain.Currencies {
		currencies[i] = currencyJSON{Code: string(c), Symbol: c.Symbol()}
	}
	self := domain.User{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Username: u.Username}
	writeJSON(w, http.StatusOK, meJSON{
		User:            personJSON{ID: int64(u.ID), Name: self.DisplayName()},
		Partners:        partners,
		Currencies:      currencies,
		DefaultCurrency: string(s.currency),
		Limits: limitsJSON{
			TitleMax:             domain.MaxTitleLen,
			NoteMax:              domain.MaxNoteLen,
			LinkMax:              domain.MaxLinkLen,
			ImagesPerWish:        domain.MaxImagesPerWish,
			ImageMaxBytes:        s.maxImage,
			CategoryNameMax:      domain.MaxCategoryNameLen,
			RecipeBodyMax:        domain.MaxRecipeBodyLen,
			ImagesPerRecipe:      domain.MaxImagesPerRecipe,
			IngredientsPerRecipe: domain.MaxIngredientsPerRecipe,
			CoursesPerRecipe:     domain.MaxCoursesPerRecipe,
			ShoppingItemsMax:     domain.MaxShoppingItems,
			ItemNameMax:          domain.MaxItemNameLen,
			RatingCommentMax:     domain.MaxRatingCommentLen,
			SavingNoteMax:        domain.MaxSavingNoteLen,
			DishWeightMaxG:       domain.MaxDishWeightGrams,
			ServingsMax:          domain.MaxServings,
		},
		Units: units(),
	})
	return nil
}

func (s *server) stats(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	st, err := s.svc.Stats.Compute(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, statsOf(st))
	return nil
}

// units lists the kitchen units in display order for the ingredient and
// shopping-list pickers.
func units() []string {
	out := make([]string, len(domain.Units))
	for i, u := range domain.Units {
		out[i] = string(u)
	}
	return out
}
