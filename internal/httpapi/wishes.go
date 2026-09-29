package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// maxQueryLen bounds search strings; titles are at most 120 characters.
const maxQueryLen = 100

type moneyInput struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type wishInput struct {
	Title      string             `json:"title"`
	Note       string             `json:"note"`
	CategoryID *domain.CategoryID `json:"category_id"`
	Link       *string            `json:"link"`
	Price      *moneyInput        `json:"price"`
	Hot        bool               `json:"hot"`
}

func (s *server) listWishes(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	filter, err := wishFilter(r.URL.Query())
	if err != nil {
		return err
	}
	wishes, err := s.svc.Wishes.List(r.Context(), filter)
	if err != nil {
		return err
	}
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, struct {
		Wishes []wishJSON `json:"wishes"`
	}{p.wishes(wishes)})
	return nil
}

func (s *server) createWish(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	var in wishInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	draft := domain.WishDraft{Title: in.Title, Note: in.Note, CategoryID: in.CategoryID, Link: in.Link, Hot: in.Hot}
	if in.Price != nil {
		price, err := s.money(*in.Price)
		if err != nil {
			return err
		}
		draft.Price = &price
	}
	wish, err := s.svc.Wishes.Create(r.Context(), u.ID, draft)
	if err != nil {
		return err
	}
	return s.writeWish(w, r, http.StatusCreated, wish)
}

func (s *server) getWish(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	id, err := pathID[domain.WishID](r, "id")
	if err != nil {
		return err
	}
	wish, err := s.svc.Wishes.Get(r.Context(), id)
	if err != nil {
		return err
	}
	return s.writeWish(w, r, http.StatusOK, wish)
}

func (s *server) patchWish(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.WishID](r, "id")
	if err != nil {
		return err
	}
	fields, err := decodePatch(w, r, "title", "note", "category_id", "link", "price", "hot")
	if err != nil {
		return err
	}
	patch, err := s.wishPatch(fields)
	if err != nil {
		return err
	}
	wish, err := s.svc.Wishes.Update(r.Context(), u.ID, id, patch)
	if err != nil {
		return err
	}
	return s.writeWish(w, r, http.StatusOK, wish)
}

func (s *server) wishPatch(fields patchFields) (domain.WishPatch, error) {
	var (
		p   domain.WishPatch
		err error
	)
	for _, key := range fields.keys() {
		raw := fields[key]
		switch key {
		case "title":
			p.Title, err = patchValue[string](raw, key)
		case "note":
			p.Note, err = patchValue[string](raw, key)
		case "category_id":
			p.CategoryID, err = patchNullable[domain.CategoryID](raw, key)
		case "link":
			p.Link, err = patchNullable[string](raw, key)
		case "hot":
			p.Hot, err = patchValue[bool](raw, key)
		case "price":
			p.Price, err = s.patchPrice(raw)
		}
		if err != nil {
			return domain.WishPatch{}, err
		}
	}
	return p, nil
}

func (s *server) patchPrice(raw json.RawMessage) (domain.Optional[*domain.Money], error) {
	in, err := patchNullable[moneyInput](raw, "price")
	switch {
	case err != nil:
		return domain.Optional[*domain.Money]{}, err
	case in.Value == nil:
		return domain.Some[*domain.Money](nil), nil
	}
	price, err := s.money(*in.Value)
	if err != nil {
		return domain.Optional[*domain.Money]{}, err
	}
	return domain.Some(&price), nil
}

// money parses a price with the same routine the bot uses. A missing
// currency means the configured default.
func (s *server) money(in moneyInput) (domain.Money, error) {
	currency := s.currency
	if strings.TrimSpace(in.Currency) != "" {
		c, err := domain.ParseCurrency(in.Currency)
		if err != nil {
			return domain.Money{}, err
		}
		currency = c
	}
	return domain.ParseAmount(in.Amount, currency)
}

func (s *server) setWishStatus(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.WishID](r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	status, err := domain.ParseStatus(in.Status)
	if err != nil {
		return err
	}
	wish, err := s.svc.Wishes.SetStatus(r.Context(), u.ID, id, status)
	if err != nil {
		return err
	}
	return s.writeWish(w, r, http.StatusOK, wish)
}

func (s *server) deleteWish(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.WishID](r, "id")
	if err != nil {
		return err
	}
	if err := s.svc.Wishes.Delete(r.Context(), u.ID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) addWishImage(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.WishID](r, "id")
	if err != nil {
		return err
	}
	return s.receiveImage(w, r,
		func(src io.Reader) (domain.Image, error) { return s.svc.Wishes.AddImage(r.Context(), u.ID, id, src) },
		func(img domain.ImageID) error { return s.svc.Wishes.RemoveImage(r.Context(), u.ID, id, img) },
	)
}

func (s *server) removeWishImage(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	id, err := pathID[domain.WishID](r, "id")
	if err != nil {
		return err
	}
	img, err := pathID[domain.ImageID](r, "imageId")
	if err != nil {
		return err
	}
	if err := s.svc.Wishes.RemoveImage(r.Context(), u.ID, id, img); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// writeWish renders a single wish. Unlike lists, it also fills saved.count:
// the wish carries only the aggregated total, so the contributions are
// counted with one extra query. The count is best effort: a failure is
// logged and leaves it null, because a mutation has already been committed
// when its response is written.
func (s *server) writeWish(w http.ResponseWriter, r *http.Request, status int, wish domain.Wish) error {
	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	out := p.wish(wish)
	if out.Saved != nil {
		savings, err := s.svc.Wishes.ListSavings(r.Context(), wish.ID)
		if err != nil {
			s.log.ErrorContext(r.Context(), "count savings", "wish_id", int64(wish.ID), "err", err)
		} else {
			n := len(savings)
			out.Saved.Count = &n
		}
	}
	writeJSON(w, status, out)
	return nil
}

// wishFilter reads ?status=&category=&q=; category=none selects wishes
// without a category.
func wishFilter(q url.Values) (domain.WishFilter, error) {
	var f domain.WishFilter
	if raw := q.Get("status"); raw != "" {
		status, err := domain.ParseStatus(raw)
		if err != nil {
			return f, err
		}
		f.Status = &status
	}
	switch raw := q.Get("category"); raw {
	case "":
	case "none":
		id := domain.Uncategorized
		f.CategoryID = &id
	default:
		n, ok := parseID(raw)
		if !ok {
			return f, badRequest("category", "Неизвестная категория.")
		}
		id := domain.CategoryID(n)
		f.CategoryID = &id
	}
	query, err := searchQuery(q.Get("q"))
	f.Query = query
	return f, err
}

func searchQuery(raw string) (string, error) {
	q := strings.TrimSpace(raw)
	if utf8.RuneCountInString(q) > maxQueryLen {
		return "", badRequest("q", "Слишком длинный поисковый запрос.")
	}
	return q, nil
}
