package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func TestSavingsLifecycle(t *testing.T) {
	h := newHarness(t)
	expectStatus(t, h.call(http.MethodGet, "/api/me", bob, nil), http.StatusOK) // bob becomes known
	id := createWish(t, h, alice, map[string]any{"title": "Диван", "price": map[string]string{"amount": "45000", "currency": "RUB"}})
	path := "/api/wishes/" + id

	rec := h.call(http.MethodPost, path+"/savings", bob, `{"amount":{"amount":"12 000","currency":"rub"},"note":" с зарплаты "}`)
	expectStatus(t, rec, http.StatusCreated)
	rub := func(minor int64) priceJSON {
		m := domain.Money{Minor: minor, Currency: "RUB"}
		return priceJSON{Amount: m.Decimal(), Currency: "RUB", Formatted: m.Format()}
	}
	want := `{"id":2,"wish_id":` + id + `,"amount":` + mustJSON(t, rub(1_200_000)) +
		`,"user":{"id":222,"name":"Боб"},"note":"с зарплаты","created_at":"2026-09-27T10:05:00Z"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("saving =\n%s\nwant\n%s", got, want)
	}
	first := decode[savingJSON](t, rec)

	// The currency may be omitted: the wish's savings currency is used.
	expectStatus(t, h.call(http.MethodPost, path+"/savings", alice, `{"amount":{"amount":"5000"}}`), http.StatusCreated)
	if m := h.db.calls.savingAmount; m == nil || m.Currency != "RUB" || m.Minor != 500_000 {
		t.Fatalf("amount passed to the service: %+v", m)
	}

	var raw map[string]json.RawMessage
	_ = json.Unmarshal(h.call(http.MethodGet, path, alice, nil).Body.Bytes(), &raw)
	wantSaved := `{"total":` + mustJSON(t, rub(1_700_000)) + `,"percent":37,"count":2}`
	if got := string(raw["saved"]); got != wantSaved {
		t.Fatalf("saved =\n%s\nwant\n%s", got, wantSaved)
	}
	if string(raw["status"]) != `"progress"` {
		t.Fatalf("a saving moves the wish to «Копим»: %s", raw["status"])
	}
	// Lists aggregate only the total, so the count is null there.
	list := h.call(http.MethodGet, "/api/wishes", alice, nil)
	if !strings.Contains(list.Body.String(), `"saved":{"total":`+mustJSON(t, rub(1_700_000))+`,"percent":37,"count":null}`) {
		t.Fatalf("list saved: %s", list.Body.String())
	}

	savings := decode[struct {
		Savings []savingJSON `json:"savings"`
	}](t, h.call(http.MethodGet, path+"/savings", alice, nil)).Savings
	if len(savings) != 2 || savings[1].ID != first.ID || savings[0].User.Name != "Алиса" {
		t.Fatalf("savings must be newest first: %+v", savings)
	}

	savingPath := path + "/savings/" + strconv.FormatInt(first.ID, 10)
	expectStatus(t, h.call(http.MethodDelete, savingPath, alice, nil), http.StatusNoContent)
	expectError(t, h.call(http.MethodDelete, savingPath, alice, nil), http.StatusNotFound, "not_found", "")
	for _, p := range []string{path + "/savings/0", path + "/savings/abc", "/api/wishes/999/savings/1"} {
		expectError(t, h.call(http.MethodDelete, p, alice, nil), http.StatusNotFound, "not_found", "")
	}
	expectError(t, h.call(http.MethodGet, "/api/wishes/999/savings", alice, nil), http.StatusNotFound, "not_found", "")
	expectError(t, h.call(http.MethodPost, "/api/wishes/999/savings", alice, `{"amount":{"amount":"1"}}`), http.StatusNotFound, "not_found", "")
	expectError(t, h.call(http.MethodPost, "/api/wishes/999/savings", alice, `{"amount":{"amount":"1","currency":"RUB"}}`), http.StatusNotFound, "not_found", "")
}

func TestSavedPercent(t *testing.T) {
	h := newHarness(t)
	saved := func(id string) string {
		t.Helper()
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(h.call(http.MethodGet, "/api/wishes/"+id, alice, nil).Body.Bytes(), &raw)
		return string(raw["saved"])
	}
	noPrice := createWish(t, h, alice, map[string]any{"title": "Путешествие"})
	if got := saved(noPrice); got != "null" {
		t.Fatalf("nothing saved yet: %s", got)
	}
	expectStatus(t, h.call(http.MethodPost, "/api/wishes/"+noPrice+"/savings", alice, `{"amount":{"amount":"100","currency":"USD"}}`), http.StatusCreated)
	if got := saved(noPrice); !strings.Contains(got, `"percent":null,"count":1`) {
		t.Fatalf("no price means no percent: %s", got)
	}
	// Without a price the first saving fixes the currency.
	expectError(t, h.call(http.MethodPost, "/api/wishes/"+noPrice+"/savings", alice, `{"amount":{"amount":"1","currency":"EUR"}}`), 400, "validation", "currency")

	cheap := createWish(t, h, alice, map[string]any{"title": "Книга", "price": map[string]string{"amount": "10", "currency": "EUR"}})
	expectStatus(t, h.call(http.MethodPost, "/api/wishes/"+cheap+"/savings", alice, `{"amount":{"amount":"25.50"}}`), http.StatusCreated)
	if got := saved(cheap); !strings.Contains(got, `"percent":100,"count":1`) {
		t.Fatalf("percent is capped at 100: %s", got)
	}
}

func TestSavingValidation(t *testing.T) {
	h := newHarness(t)
	id := createWish(t, h, alice, map[string]any{"title": "Диван", "price": map[string]string{"amount": "450", "currency": "EUR"}})
	path := "/api/wishes/" + id + "/savings"
	cases := []struct {
		name  string
		body  string
		field string
	}{
		{"no amount", `{}`, "amount"},
		{"null amount", `{"amount":null}`, "amount"},
		{"empty amount", `{"amount":{"amount":""}}`, "amount"},
		{"zero", `{"amount":{"amount":"0"}}`, "amount"},
		{"negative", `{"amount":{"amount":"-5"}}`, "amount"},
		{"garbage", `{"amount":{"amount":"пять"}}`, "amount"},
		{"currency sign in amount", `{"amount":{"amount":"5000€"}}`, "amount"},
		{"too large", `{"amount":{"amount":"1000000000000"}}`, "amount"},
		{"number amount", `{"amount":{"amount":5000}}`, "amount.amount"},
		{"bare string", `{"amount":"5000"}`, "amount"},
		{"other currency than the price", `{"amount":{"amount":"5","currency":"RUB"}}`, "currency"},
		{"unknown currency", `{"amount":{"amount":"5","currency":"BTC"}}`, "currency"},
		{"unknown field", `{"amount":{"amount":"5"},"user_id":222}`, "user_id"},
		{"unknown nested field", `{"amount":{"amount":"5","rate":1}}`, "rate"},
		{"long note", `{"amount":{"amount":"5"},"note":"` + strings.Repeat("я", domain.MaxSavingNoteLen+1) + `"}`, "note"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectError(t, h.call(http.MethodPost, path, alice, c.body), http.StatusBadRequest, "validation", c.field)
		})
	}
	if len(h.db.savings) != 0 {
		t.Fatalf("invalid input created savings: %+v", h.db.savings)
	}
}

func TestPriceCurrencyIsLockedBySavings(t *testing.T) {
	h := newHarness(t)
	id := createWish(t, h, alice, map[string]any{"title": "Диван"})
	expectStatus(t, h.call(http.MethodPost, "/api/wishes/"+id+"/savings", alice, `{"amount":{"amount":"100","currency":"RUB"}}`), http.StatusCreated)
	expectError(t, h.call(http.MethodPatch, "/api/wishes/"+id, alice, `{"price":{"amount":"45000","currency":"EUR"}}`), 400, "validation", "price")
	rec := h.call(http.MethodPatch, "/api/wishes/"+id, alice, `{"price":{"amount":"45000","currency":"RUB"}}`)
	expectStatus(t, rec, http.StatusOK)
	if w := decode[wishJSON](t, rec); w.Saved == nil || w.Saved.Percent == nil || *w.Saved.Percent != 0 || w.Saved.Count == nil || *w.Saved.Count != 1 {
		t.Fatalf("saved after setting a price: %+v", w.Saved)
	}
}

// failingSavings breaks only ListSavings, as if the count query failed.
type failingSavings struct{ fakeWishes }

func (failingSavings) ListSavings(context.Context, domain.WishID) ([]domain.Saving, error) {
	return nil, errDiskOnFire
}

// A wish response must not fail because the contribution count could not be
// read: PATCH has already been committed when its response is written.
func TestSavedCountIsBestEffort(t *testing.T) {
	h := newHarness(t)
	id := createWish(t, h, alice, map[string]any{"title": "Диван", "price": map[string]string{"amount": "100", "currency": "EUR"}})
	expectStatus(t, h.call(http.MethodPost, "/api/wishes/"+id+"/savings", alice, `{"amount":{"amount":"10"}}`), http.StatusCreated)

	broken := newHarness(t, func(o *Options, _ *tuning) {
		o.Services.Wishes = failingSavings{fakeWishes{h.db}}
	})
	rec := broken.call(http.MethodPatch, "/api/wishes/"+id, alice, `{"hot":true}`)
	expectStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"percent":10,"count":null`) {
		t.Fatalf("count must fall back to null: %s", rec.Body.String())
	}
	if !h.db.wishes[domain.WishID(1)].Hot {
		t.Fatal("the patch must be applied")
	}
	if !strings.Contains(broken.logs.String(), "count savings") || !strings.Contains(broken.logs.String(), "disk I/O error") {
		t.Fatalf("the failure must be logged: %s", broken.logs.String())
	}
	expectError(t, broken.call(http.MethodGet, "/api/wishes/"+id+"/savings", alice, nil), http.StatusInternalServerError, "internal", "")
}
