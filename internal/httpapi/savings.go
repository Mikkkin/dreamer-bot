package httpapi

import (
	"net/http"
	"strings"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func (s *server) listSavings(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	id, err := pathID[domain.WishID](r, "id")
	if err != nil {
		return err
	}
	savings, err := s.svc.Wishes.ListSavings(r.Context(), id)
	if err != nil {
		return err
	}
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, struct {
		Savings []savingJSON `json:"savings"`
	}{p.savings(savings)})
	return nil
}

// addSaving records money put aside for the wish. Without a currency the
// wish's savings currency is used (then the configured default).
func (s *server) addSaving(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.WishID](r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Amount moneyInput `json:"amount"`
		Note   string     `json:"note"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Amount.Currency) == "" {
		wish, err := s.svc.Wishes.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if c, ok := wish.SavingsCurrency(); ok {
			in.Amount.Currency = string(c)
		}
	}
	amount, err := s.money(in.Amount)
	if err != nil {
		return savingError(err)
	}
	saving, err := s.svc.Wishes.AddSaving(r.Context(), u.ID, id, amount, in.Note)
	if err != nil {
		return savingError(err)
	}
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, p.saving(saving))
	return nil
}

// savingError reports money errors on the "amount" field: the shared money
// parser names it "price", which is the wish's own field.
func savingError(err error) error {
	if v, ok := domain.AsValidation(err); ok && v.Field == "price" {
		return badRequest("amount", capitalize(v.Message))
	}
	return err
}

func (s *server) removeSaving(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.WishID](r, "id")
	if err != nil {
		return err
	}
	saving, err := pathID[domain.SavingID](r, "savingId")
	if err != nil {
		return err
	}
	if err := s.svc.Wishes.RemoveSaving(r.Context(), u.ID, id, saving); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
