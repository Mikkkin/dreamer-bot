package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/vkusvill"
)

// fakeVkusvill answers searches from a table keyed by the lower-cased query.
type fakeVkusvill struct {
	products map[string][]vkusvill.Product
	failing  map[string]bool // lower-cased queries whose search fails
	link     string
	cartErr  error
	estimate *domain.Money
	delay    time.Duration

	mu        sync.Mutex
	searches  []string
	carts     [][]vkusvill.Line
	active    int
	maxActive int
}

func (f *fakeVkusvill) Search(ctx context.Context, query string) ([]vkusvill.Product, error) {
	f.mu.Lock()
	f.searches = append(f.searches, query)
	f.active++
	f.maxActive = max(f.maxActive, f.active)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	time.Sleep(f.delay)
	if f.failing[strings.ToLower(query)] {
		return nil, fmt.Errorf("%w: HTTP 502", vkusvill.ErrUnavailable)
	}
	return f.products[strings.ToLower(query)], nil
}

func (f *fakeVkusvill) CreateCart(_ context.Context, lines []vkusvill.Line) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.carts = append(f.carts, slices.Clone(lines))
	if f.cartErr != nil {
		return "", f.cartErr
	}
	return f.link, nil
}

func (f *fakeVkusvill) Estimate([]vkusvill.Line) (domain.Money, bool) {
	if f.estimate == nil {
		return domain.Money{}, false
	}
	return *f.estimate, true
}

func (f *fakeVkusvill) searched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.searches)
}

func withVkusvill(f *fakeVkusvill) option {
	return func(o *Options, _ *tuning) { o.Vkusvill = f }
}

func rubles(minor int64) *domain.Money { return &domain.Money{Minor: minor, Currency: "RUB"} }

// addItems puts unchecked items on the shared list and returns their IDs.
func addItems(t *testing.T, h *harness, items ...map[string]any) []int64 {
	t.Helper()
	rec := h.call(http.MethodPost, "/api/shopping", alice, map[string]any{"items": items})
	expectStatus(t, rec, http.StatusCreated)
	var ids []int64
	for _, it := range decode[shoppingItems](t, rec).Items {
		ids = append(ids, it.ID)
	}
	return ids
}

func item(name string) map[string]any { return map[string]any{"name": name} }

type matchesJSON struct {
	Matches []vkusvillMatchJSON `json:"matches"`
}

const (
	matchPath = "/api/shopping/vkusvill/match"
	cartPath  = "/api/shopping/vkusvill/cart"
)

func TestVkusvillDisabled(t *testing.T) {
	h := newHarness(t)
	addItems(t, h, item("Молоко"))
	for _, c := range []struct{ path, body string }{
		{matchPath, `{}`},
		{cartPath, `{"lines":[{"xml_id":173,"quantity":"2"}]}`},
	} {
		e := expectError(t, h.call(http.MethodPost, c.path, alice, c.body), http.StatusServiceUnavailable, "unavailable", "")
		if !strings.Contains(e.Error.Message, "ВкусВилл") {
			t.Errorf("%s: message %q", c.path, e.Error.Message)
		}
	}
	if logs := h.logs.String(); strings.Contains(logs, `"level":"ERROR"`) || !strings.Contains(logs, "external service unavailable") {
		t.Errorf("a switched-off feature is a warning, not an error: %s", logs)
	}
}

func TestVkusvillMatch(t *testing.T) {
	vv := &fakeVkusvill{
		products: map[string][]vkusvill.Product{
			"молоко": {
				{XMLID: 173, Name: "Молоко 3,2%, 1 л", Price: rubles(9300), Unit: "шт", Weight: "1 кг"},
				{XMLID: 731, Name: "Молоко развесное"},
			},
		},
		failing: map[string]bool{"хлеб": true},
	}
	h := newHarness(t, withVkusvill(vv))
	ids := addItems(t, h,
		map[string]any{"name": "Молоко", "amount": "1", "unit": "л"},
		item("Хлеб"),
		map[string]any{"name": "молоко", "amount": "2", "unit": "шт"},
		item("Соль"),
	)
	expectStatus(t, h.call(http.MethodPatch, "/api/shopping/"+strconv.FormatInt(ids[3], 10), alice, `{"checked":true}`), http.StatusOK)

	rec := h.call(http.MethodPost, matchPath, alice, `{}`)
	expectStatus(t, rec, http.StatusOK)
	got := decode[matchesJSON](t, rec).Matches
	if len(got) != 3 || got[0].ItemID != ids[0] || got[1].ItemID != ids[1] || got[2].ItemID != ids[2] {
		t.Fatalf("matches = %+v", got)
	}
	if got[0].Query != "Молоко" || got[2].Query != "молоко" || len(got[0].Candidates) != 2 || len(got[2].Candidates) != 2 {
		t.Errorf("milk matches = %+v", got)
	}
	// Only names are searched, each distinct name once; a failed search
	// leaves its item without candidates.
	if s := vv.searched(); len(s) != 2 || !slices.Contains(s, "Молоко") || !slices.Contains(s, "Хлеб") {
		t.Errorf("searches = %q", s)
	}
	raw := decode[struct {
		Matches []map[string]json.RawMessage `json:"matches"`
	}](t, rec).Matches
	if string(raw[1]["candidates"]) != "[]" {
		t.Errorf("failed search candidates = %s, want []", raw[1]["candidates"])
	}
	wantCandidates := `[{"xml_id":173,"name":"Молоко 3,2%, 1 л","price":{"amount":"93","currency":"RUB","formatted":"93` + " " + `₽"},"unit":"шт","weight":"1` + " " + `кг"},` +
		`{"xml_id":731,"name":"Молоко развесное","price":null,"unit":null,"weight":null}]`
	if string(raw[0]["candidates"]) != wantCandidates {
		t.Errorf("candidates = %s\nwant %s", raw[0]["candidates"], wantCandidates)
	}
}

func TestVkusvillMatchSelection(t *testing.T) {
	vv := &fakeVkusvill{}
	h := newHarness(t, withVkusvill(vv))
	var many []map[string]any
	for i := range 35 {
		many = append(many, item(fmt.Sprintf("Товар %02d", i)))
	}
	ids := addItems(t, h, many...)

	got := decode[matchesJSON](t, h.call(http.MethodPost, matchPath, alice, `{"item_ids":null}`)).Matches
	if len(got) != 30 || got[0].ItemID != ids[0] || got[29].ItemID != ids[29] {
		t.Fatalf("got %d matches, want the first 30", len(got))
	}
	for _, m := range got {
		if m.Candidates == nil {
			t.Fatal("candidates must be an array")
		}
	}

	expectStatus(t, h.call(http.MethodPatch, "/api/shopping/"+strconv.FormatInt(ids[1], 10), alice, `{"checked":true}`), http.StatusOK)
	body := fmt.Sprintf(`{"item_ids":[%d,%d,%d,999999]}`, ids[1], ids[34], ids[2])
	got = decode[matchesJSON](t, h.call(http.MethodPost, matchPath, alice, body)).Matches
	if len(got) != 2 || got[0].ItemID != ids[2] || got[1].ItemID != ids[34] {
		t.Errorf("filtered matches = %+v; checked and unknown items are skipped, list order kept", got)
	}
}

func TestVkusvillMatchValidation(t *testing.T) {
	vv := &fakeVkusvill{}
	h := newHarness(t, withVkusvill(vv))
	addItems(t, h, item("Молоко"))
	cases := []struct{ body, field string }{
		{`{"item_ids":[]}`, "item_ids"},
		{`{"item_ids":[0]}`, "item_ids.0"},
		{`{"item_ids":[5,-1]}`, "item_ids.1"},
		{`{"item_ids":"5"}`, "item_ids"},
		{`{"items":[1]}`, "items"},
		{`[]`, "-"},
	}
	for _, c := range cases {
		expectError(t, h.call(http.MethodPost, matchPath, alice, c.body), http.StatusBadRequest, "validation", c.field)
	}
	if len(vv.searched()) != 0 {
		t.Error("invalid requests must not search")
	}
}

func TestVkusvillMatchUnavailable(t *testing.T) {
	vv := &fakeVkusvill{failing: map[string]bool{"молоко": true, "хлеб": true}}
	h := newHarness(t, withVkusvill(vv))
	// Nothing to match needs no ВкусВилл at all.
	rec := h.call(http.MethodPost, matchPath, alice, `{}`)
	expectStatus(t, rec, http.StatusOK)
	if strings.TrimSpace(rec.Body.String()) != `{"matches":[]}` {
		t.Errorf("empty list = %s", rec.Body.String())
	}

	addItems(t, h, item("Молоко"), item("Хлеб"))
	expectError(t, h.call(http.MethodPost, matchPath, alice, `{}`), http.StatusServiceUnavailable, "unavailable", "")
	if strings.Contains(h.logs.String(), `"level":"ERROR"`) {
		t.Errorf("an outage is logged as an error: %s", h.logs.String())
	}
}

func TestVkusvillMatchBoundsConcurrency(t *testing.T) {
	vv := &fakeVkusvill{delay: 20 * time.Millisecond}
	h := newHarness(t, withVkusvill(vv))
	var items []map[string]any
	for i := range 12 {
		items = append(items, item(fmt.Sprintf("Товар %d", i)))
	}
	addItems(t, h, items...)
	expectStatus(t, h.call(http.MethodPost, matchPath, alice, `{}`), http.StatusOK)
	if len(vv.searched()) != 12 || vv.maxActive > matchWorkers || vv.maxActive < 2 {
		t.Errorf("searches = %d, at most %d at once (want 2..%d)", len(vv.searched()), vv.maxActive, matchWorkers)
	}
}

func TestVkusvillCart(t *testing.T) {
	vv := &fakeVkusvill{link: "https://vkusvill.ru/?share_basket=2063312749", estimate: rubles(27000)}
	h := newHarness(t, withVkusvill(vv))
	rec := h.call(http.MethodPost, cartPath, alice, `{"lines":[{"xml_id":173,"quantity":"2"},{"xml_id":731,"quantity":"0,5"}]}`)
	expectStatus(t, rec, http.StatusOK)
	want := `{"url":"https://vkusvill.ru/?share_basket=2063312749","estimated_total":{"amount":"270","currency":"RUB","formatted":"270` + " " + `₽"}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s\nwant %s", got, want)
	}
	if len(vv.carts) != 1 || !slices.Equal(vv.carts[0], []vkusvill.Line{{XMLID: 173, Quantity: "2"}, {XMLID: 731, Quantity: "0,5"}}) {
		t.Errorf("carts = %+v", vv.carts)
	}

	vv.estimate = nil
	rec = h.call(http.MethodPost, cartPath, alice, `{"lines":[{"xml_id":173,"quantity":"1"}]}`)
	expectStatus(t, rec, http.StatusOK)
	if !strings.HasSuffix(strings.TrimSpace(rec.Body.String()), `"estimated_total":null}`) {
		t.Errorf("unknown prices must give a null estimate: %s", rec.Body.String())
	}
}

func TestVkusvillCartValidation(t *testing.T) {
	vv := &fakeVkusvill{link: "https://vkusvill.ru/?share_basket=1"}
	h := newHarness(t, withVkusvill(vv))
	lines := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"xml_id":%d,"quantity":"1"}`, i+1)
		}
		return `{"lines":[` + strings.Join(parts, ",") + `]}`
	}
	cases := []struct{ name, body, field string }{
		{"no lines", `{}`, "lines"},
		{"empty lines", `{"lines":[]}`, "lines"},
		{"null lines", `{"lines":null}`, "lines"},
		{"too many", lines(31), "lines"},
		{"zero id", `{"lines":[{"xml_id":0,"quantity":"1"}]}`, "lines.0.xml_id"},
		{"huge id", `{"lines":[{"xml_id":1,"quantity":"1"},{"xml_id":1000000000,"quantity":"1"}]}`, "lines.1.xml_id"},
		{"zero quantity", `{"lines":[{"xml_id":1,"quantity":"0"}]}`, "lines.0.quantity"},
		{"missing quantity", `{"lines":[{"xml_id":1}]}`, "lines.0.quantity"},
		{"above 40", `{"lines":[{"xml_id":1,"quantity":"40.5"}]}`, "lines.0.quantity"},
		{"three decimals", `{"lines":[{"xml_id":1,"quantity":"0.125"}]}`, "lines.0.quantity"},
		{"number quantity", `{"lines":[{"xml_id":1,"quantity":2}]}`, "-"},
		{"unknown line field", `{"lines":[{"xml_id":1,"quantity":"1","price":"93"}]}`, "price"},
		{"unknown field", `{"lines":[{"xml_id":1,"quantity":"1"}],"total":1}`, "total"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectError(t, h.call(http.MethodPost, cartPath, alice, c.body), http.StatusBadRequest, "validation", c.field)
		})
	}
	if len(vv.carts) != 0 {
		t.Errorf("invalid carts reached ВкусВилл: %+v", vv.carts)
	}
	expectStatus(t, h.call(http.MethodPost, cartPath, alice, lines(30)), http.StatusOK)
}

func TestVkusvillCartErrors(t *testing.T) {
	vv := &fakeVkusvill{cartErr: fmt.Errorf("line 1: %w", vkusvill.ErrInvalidCart)}
	h := newHarness(t, withVkusvill(vv))
	body := `{"lines":[{"xml_id":1,"quantity":"30"},{"xml_id":1,"quantity":"11"}]}`
	expectError(t, h.call(http.MethodPost, cartPath, alice, body), http.StatusBadRequest, "validation", "lines")

	vv.cartErr = fmt.Errorf("%w: vkusvill_cart_link_create: basket link does not point to vkusvill.ru", vkusvill.ErrUnavailable)
	expectError(t, h.call(http.MethodPost, cartPath, alice, `{"lines":[{"xml_id":1,"quantity":"1"}]}`), http.StatusServiceUnavailable, "unavailable", "")
}

func TestVkusvillRateLimitIsStricter(t *testing.T) {
	h := newHarness(t, withVkusvill(&fakeVkusvill{}), func(_ *Options, tu *tuning) {
		tu.externalRate = rateLimit{every: 0, burst: 1}
	})
	expectStatus(t, h.call(http.MethodPost, matchPath, alice, `{}`), http.StatusOK)
	expectError(t, h.call(http.MethodPost, cartPath, alice, `{"lines":[{"xml_id":1,"quantity":"1"}]}`), http.StatusTooManyRequests, "rate_limited", "")
	// Other endpoints and the partner are unaffected.
	expectStatus(t, h.call(http.MethodGet, "/api/shopping", alice, nil), http.StatusOK)
	expectStatus(t, h.call(http.MethodPost, matchPath, bob, `{}`), http.StatusOK)
}
