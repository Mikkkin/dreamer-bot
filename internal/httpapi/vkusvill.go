package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/vkusvill"
)

// Vkusvill builds ВкусВилл baskets (internal/vkusvill). Its errors wrap
// vkusvill.ErrUnavailable or vkusvill.ErrInvalidCart.
type Vkusvill interface {
	// Search returns up to three products for an item name, best first.
	Search(ctx context.Context, query string) ([]vkusvill.Product, error)
	// CreateCart returns the link of a shared basket with the lines.
	CreateCart(ctx context.Context, lines []vkusvill.Line) (string, error)
	// Estimate prices the lines from recent searches; false when a price
	// is unknown.
	Estimate(lines []vkusvill.Line) (domain.Money, bool)
}

const (
	// matchLimit is the most items one match covers, as a basket holds at
	// most that many lines.
	matchLimit = vkusvill.MaxCartLines
	// matchWorkers bounds concurrent searches; the client's global rate
	// limit paces them further.
	matchWorkers = 3
	// matchDeadline bounds a whole match. Searches still running then
	// count as failed, and their items get no candidates.
	matchDeadline = 20 * time.Second
	// cartDeadline covers the rate-limit wait plus the client's own 8 s.
	cartDeadline = 10 * time.Second
)

type vkusvillCandidateJSON struct {
	XMLID  int        `json:"xml_id"`
	Name   string     `json:"name"`
	Price  *priceJSON `json:"price"`
	Unit   *string    `json:"unit"`
	Weight *string    `json:"weight"`
}

type vkusvillMatchJSON struct {
	ItemID     int64                   `json:"item_id"`
	Query      string                  `json:"query"`
	Candidates []vkusvillCandidateJSON `json:"candidates"`
}

// vkusvillMatch offers ВкусВилл products for unchecked shopping-list items:
// all of them, or those named in item_ids (unknown or checked ones are
// skipped, as the list may have changed meanwhile). Only the item name is
// searched for, never the quantity.
func (s *server) vkusvillMatch(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	if s.vkusvill == nil {
		return errVkusvillOff
	}
	var in struct {
		ItemIDs *[]int64 `json:"item_ids"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	wanted, err := matchIDs(in.ItemIDs)
	if err != nil {
		return err
	}
	list, err := s.svc.Shopping.List(r.Context())
	if err != nil {
		return err
	}
	var items []domain.ShoppingItem
	for _, it := range list {
		if it.Checked || (wanted != nil && !wanted[it.ID]) {
			continue
		}
		if items = append(items, it); len(items) == matchLimit {
			break
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), matchDeadline)
	defer cancel()
	found, err := s.searchAll(ctx, items)
	if err != nil {
		return err
	}
	out := make([]vkusvillMatchJSON, len(items))
	for i, it := range items {
		products := found[matchKey(it.Name)]
		candidates := make([]vkusvillCandidateJSON, len(products))
		for j, p := range products {
			candidates[j] = candidateOf(p)
		}
		out[i] = vkusvillMatchJSON{ItemID: int64(it.ID), Query: it.Name, Candidates: candidates}
	}
	writeJSON(w, http.StatusOK, struct {
		Matches []vkusvillMatchJSON `json:"matches"`
	}{out})
	return nil
}

// matchIDs checks the optional item_ids filter; nil means all items.
func matchIDs(ids *[]int64) (map[domain.ShoppingItemID]bool, error) {
	if ids == nil {
		return nil, nil
	}
	switch {
	case len(*ids) == 0:
		return nil, badRequest("item_ids", "Выберите хотя бы одну позицию.")
	case len(*ids) > domain.MaxShoppingItems:
		return nil, badRequest("item_ids", "Слишком много позиций.")
	}
	out := make(map[domain.ShoppingItemID]bool, len(*ids))
	for i, id := range *ids {
		if id <= 0 {
			return nil, badRequest("item_ids."+strconv.Itoa(i), "Неверный номер позиции.")
		}
		out[domain.ShoppingItemID(id)] = true
	}
	return out, nil
}

// matchKey groups items that need the same search.
func matchKey(name string) string { return strings.ToLower(name) }

// searchAll searches every distinct item name, a few at a time. A failed
// search leaves its items without candidates; only when every search
// failed is ВкусВилл reported as unavailable.
func (s *server) searchAll(ctx context.Context, items []domain.ShoppingItem) (map[string][]vkusvill.Product, error) {
	var queries []string
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		if key := matchKey(it.Name); !seen[key] {
			seen[key] = true
			queries = append(queries, it.Name)
		}
	}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		found    = make(map[string][]vkusvill.Product, len(queries))
		failures []error
		next     = make(chan string)
	)
	for range min(matchWorkers, len(queries)) {
		wg.Go(func() {
			for q := range next {
				products, err := s.vkusvill.Search(ctx, q)
				mu.Lock()
				if err != nil {
					failures = append(failures, err)
				} else {
					found[matchKey(q)] = products
				}
				mu.Unlock()
			}
		})
	}
	for _, q := range queries {
		next <- q
	}
	close(next)
	wg.Wait()

	if len(failures) > 0 {
		if len(failures) == len(queries) {
			return nil, failures[0]
		}
		s.log.WarnContext(ctx, "vkusvill searches partly failed",
			"failed", len(failures), "searches", len(queries), "err", failures[0])
	}
	return found, nil
}

func candidateOf(p vkusvill.Product) vkusvillCandidateJSON {
	c := vkusvillCandidateJSON{XMLID: p.XMLID, Name: p.Name, Unit: nonEmpty(p.Unit), Weight: nonEmpty(p.Weight)}
	if p.Price != nil {
		price := priceOf(*p.Price)
		c.Price = &price
	}
	return c
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type cartLineInput struct {
	XMLID    int64  `json:"xml_id"`
	Quantity string `json:"quantity"`
}

// vkusvillCart turns the confirmed products into a shared ВкусВилл basket.
// The estimate uses the prices the last matches showed, so it matches what
// the user saw; it is null when any price is unknown.
func (s *server) vkusvillCart(w http.ResponseWriter, r *http.Request, _ auth.WebAppUser) error {
	if s.vkusvill == nil {
		return errVkusvillOff
	}
	var in struct {
		Lines []cartLineInput `json:"lines"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	lines, err := cartLines(in.Lines)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), cartDeadline)
	defer cancel()
	link, err := s.vkusvill.CreateCart(ctx, lines)
	if err != nil {
		return err
	}
	var total *priceJSON
	if m, ok := s.vkusvill.Estimate(lines); ok {
		p := priceOf(m)
		total = &p
	}
	writeJSON(w, http.StatusOK, struct {
		URL            string     `json:"url"`
		EstimatedTotal *priceJSON `json:"estimated_total"`
	}{link, total})
	return nil
}

// cartLines validates 1–30 lines of a product ID and a quantity 0.01–40.
func cartLines(in []cartLineInput) ([]vkusvill.Line, error) {
	switch {
	case len(in) == 0:
		return nil, badRequest("lines", "Выберите хотя бы один товар.")
	case len(in) > vkusvill.MaxCartLines:
		return nil, badRequest("lines", "Не больше "+strconv.Itoa(vkusvill.MaxCartLines)+" товаров за раз.")
	}
	out := make([]vkusvill.Line, len(in))
	for i, l := range in {
		field := "lines." + strconv.Itoa(i) + "."
		if l.XMLID <= 0 || l.XMLID > vkusvill.MaxXMLID {
			return nil, badRequest(field+"xml_id", "Неверный товар.")
		}
		if _, err := vkusvill.ParseQuantity(l.Quantity); err != nil {
			return nil, badRequest(field+"quantity", "Количество — число от 0,01 до 40.")
		}
		out[i] = vkusvill.Line{XMLID: int(l.XMLID), Quantity: l.Quantity}
	}
	return out, nil
}
