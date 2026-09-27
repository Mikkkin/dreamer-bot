package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// Wire formats of docs/API.md. IDs are JSON numbers: Telegram user IDs and
// SQLite row IDs stay below 2^53, so JavaScript reads them exactly.

type personJSON struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type priceJSON struct {
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	Formatted string `json:"formatted"`
}

type imageJSON struct {
	ID       int64  `json:"id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	ThumbURL string `json:"thumb_url"`
	FullURL  string `json:"full_url"`
}

type wishJSON struct {
	ID          int64       `json:"id"`
	Title       string      `json:"title"`
	Note        string      `json:"note"`
	CategoryID  *int64      `json:"category_id"`
	Link        *string     `json:"link"`
	Price       *priceJSON  `json:"price"`
	Status      string      `json:"status"`
	Hot         bool        `json:"hot"`
	Author      personJSON  `json:"author"`
	Images      []imageJSON `json:"images"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
	FulfilledAt *string     `json:"fulfilled_at"`
}

type recipeJSON struct {
	ID        int64       `json:"id"`
	Title     string      `json:"title"`
	Link      *string     `json:"link"`
	Body      string      `json:"body"`
	Author    personJSON  `json:"author"`
	Images    []imageJSON `json:"images"`
	CreatedAt string      `json:"created_at"`
	UpdatedAt string      `json:"updated_at"`
}

type categoryJSON struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Emoji    string `json:"emoji"`
	Position int    `json:"position"`
}

type totalsJSON struct {
	Count int         `json:"count"`
	Sums  []priceJSON `json:"sums"`
}

type categoryStatsJSON struct {
	Category *categoryJSON         `json:"category"`
	ByStatus map[string]totalsJSON `json:"by_status"`
}

type statsJSON struct {
	Overall           map[string]totalsJSON `json:"overall"`
	Categories        []categoryStatsJSON   `json:"categories"`
	FulfilledThisYear int                   `json:"fulfilled_this_year"`
	Recipes           int                   `json:"recipes"`
}

// presenter renders domain objects for one request. Author names are
// resolved from a single Users.List call per request.
type presenter struct {
	signer *auth.MediaSigner
	names  map[domain.UserID]string
}

func (s *server) presenter(ctx context.Context) (presenter, error) {
	users, err := s.svc.Users.List(ctx)
	if err != nil {
		return presenter{}, fmt.Errorf("list users: %w", err)
	}
	names := make(map[domain.UserID]string, len(users))
	for _, u := range users {
		names[u.ID] = u.DisplayName()
	}
	return presenter{signer: s.signer, names: names}, nil
}

func (p presenter) author(id domain.UserID) personJSON {
	name, ok := p.names[id]
	if !ok {
		name = domain.User{ID: id}.DisplayName()
	}
	return personJSON{ID: int64(id), Name: name}
}

func (p presenter) wish(w domain.Wish) wishJSON {
	out := wishJSON{
		ID:        int64(w.ID),
		Title:     w.Title,
		Note:      w.Note,
		Link:      w.Link,
		Status:    string(w.Status),
		Hot:       w.Hot,
		Author:    p.author(w.AuthorID),
		Images:    p.images(w.Images),
		CreatedAt: timestamp(w.CreatedAt),
		UpdatedAt: timestamp(w.UpdatedAt),
	}
	if w.CategoryID != nil {
		id := int64(*w.CategoryID)
		out.CategoryID = &id
	}
	if w.Price != nil {
		price := priceOf(*w.Price)
		out.Price = &price
	}
	if w.FulfilledAt != nil {
		at := timestamp(*w.FulfilledAt)
		out.FulfilledAt = &at
	}
	return out
}

func (p presenter) wishes(ws []domain.Wish) []wishJSON {
	out := make([]wishJSON, len(ws))
	for i, w := range ws {
		out[i] = p.wish(w)
	}
	return out
}

func (p presenter) recipe(r domain.Recipe) recipeJSON {
	return recipeJSON{
		ID:        int64(r.ID),
		Title:     r.Title,
		Link:      r.Link,
		Body:      r.Body,
		Author:    p.author(r.AuthorID),
		Images:    p.images(r.Images),
		CreatedAt: timestamp(r.CreatedAt),
		UpdatedAt: timestamp(r.UpdatedAt),
	}
}

func (p presenter) recipes(rs []domain.Recipe) []recipeJSON {
	out := make([]recipeJSON, len(rs))
	for i, r := range rs {
		out[i] = p.recipe(r)
	}
	return out
}

func (p presenter) images(imgs []domain.Image) []imageJSON {
	out := make([]imageJSON, len(imgs))
	for i, img := range imgs {
		out[i] = imageOf(p.signer, img)
	}
	return out
}

func imageOf(signer *auth.MediaSigner, img domain.Image) imageJSON {
	return imageJSON{
		ID:       int64(img.ID),
		Width:    img.Width,
		Height:   img.Height,
		ThumbURL: signer.URL(img.ID, string(service.VariantThumb)),
		FullURL:  signer.URL(img.ID, string(service.VariantFull)),
	}
}

func categoryOf(c domain.Category) categoryJSON {
	return categoryJSON{ID: int64(c.ID), Name: c.Name, Emoji: c.Emoji, Position: c.Position}
}

func priceOf(m domain.Money) priceJSON {
	return priceJSON{Amount: m.Decimal(), Currency: string(m.Currency), Formatted: m.Format()}
}

func statsOf(st domain.Stats) statsJSON {
	out := statsJSON{
		Overall:           byStatus(st.Overall),
		Categories:        make([]categoryStatsJSON, len(st.Categories)),
		FulfilledThisYear: st.FulfilledThisYear,
		Recipes:           st.Recipes,
	}
	for i, c := range st.Categories {
		entry := categoryStatsJSON{ByStatus: byStatus(c.ByStatus)}
		if c.Category != nil {
			cat := categoryOf(*c.Category)
			entry.Category = &cat
		}
		out.Categories[i] = entry
	}
	return out
}

// byStatus always emits every status key, with empty sums as [] not null.
func byStatus(m map[domain.Status]domain.StatusTotals) map[string]totalsJSON {
	out := make(map[string]totalsJSON, len(domain.Statuses))
	for _, st := range domain.Statuses {
		totals := m[st]
		sums := make([]priceJSON, len(totals.Sums))
		for i, sum := range totals.Sums {
			sums[i] = priceOf(sum)
		}
		out[string(st)] = totalsJSON{Count: totals.Count, Sums: sums}
	}
	return out
}

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
