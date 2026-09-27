package service

import (
	"context"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type statsService struct{ *core }

func (s statsService) Compute(ctx context.Context) (domain.Stats, error) {
	categories, err := s.repos.ListCategories(ctx)
	if err != nil {
		return domain.Stats{}, err
	}
	wishes, err := s.repos.ListWishes(ctx, domain.WishFilter{})
	if err != nil {
		return domain.Stats{}, err
	}
	recipes, err := s.repos.CountRecipes(ctx)
	if err != nil {
		return domain.Stats{}, err
	}
	return summarize(categories, wishes, recipes, s.yearStart()), nil
}

// yearStart is January 1st of the current year in the service location.
func (s statsService) yearStart() time.Time {
	now := s.clock().In(s.location)
	return time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, s.location)
}

// summarize aggregates wishes per category (in category order, followed by
// an uncategorized bucket when it has wishes) and overall.
func summarize(categories []domain.Category, wishes []domain.Wish, recipes int, yearStart time.Time) domain.Stats {
	overall := newTally()
	uncategorized := newTally()
	byCategory := make(map[domain.CategoryID]*tally, len(categories))
	for _, c := range categories {
		byCategory[c.ID] = newTally()
	}

	fulfilledThisYear := 0
	for _, w := range wishes {
		overall.add(w)
		bucket := uncategorized
		if w.CategoryID != nil {
			if t, ok := byCategory[*w.CategoryID]; ok {
				bucket = t
			}
		}
		bucket.add(w)
		if w.Status == domain.StatusDone && w.FulfilledAt != nil && !w.FulfilledAt.Before(yearStart) {
			fulfilledThisYear++
		}
	}

	out := domain.Stats{
		Categories:        make([]domain.CategoryStats, 0, len(categories)+1),
		Overall:           overall.totals(),
		Recipes:           recipes,
		FulfilledThisYear: fulfilledThisYear,
	}
	for _, c := range categories {
		out.Categories = append(out.Categories, domain.CategoryStats{Category: &c, ByStatus: byCategory[c.ID].totals()})
	}
	if uncategorized.count > 0 {
		out.Categories = append(out.Categories, domain.CategoryStats{ByStatus: uncategorized.totals()})
	}
	return out
}

// tally accumulates wish counts and prices per status.
type tally struct {
	count    int
	byStatus map[domain.Status]*statusTally
}

type statusTally struct {
	count  int
	prices []domain.Money
}

func newTally() *tally {
	t := &tally{byStatus: make(map[domain.Status]*statusTally, len(domain.Statuses))}
	for _, st := range domain.Statuses {
		t.byStatus[st] = &statusTally{}
	}
	return t
}

func (t *tally) add(w domain.Wish) {
	st, ok := t.byStatus[w.Status]
	if !ok {
		return
	}
	t.count++
	st.count++
	if w.Price != nil {
		st.prices = append(st.prices, *w.Price)
	}
}

// totals has an entry for every status, with non-nil sums, so that clients
// always see all three statuses.
func (t *tally) totals() map[domain.Status]domain.StatusTotals {
	out := make(map[domain.Status]domain.StatusTotals, len(t.byStatus))
	for status, st := range t.byStatus {
		out[status] = domain.StatusTotals{Count: st.count, Sums: domain.SumByCurrency(st.prices)}
	}
	return out
}
