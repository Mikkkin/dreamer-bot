package vkusvill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// The testdata files are real answers of https://mcp.vkusvill.ru/mcp
// recorded on 2026-09-30, trimmed to a few products. The server answered
// every probe with application/json, even when asked for
// text/event-stream only; search_milk.sse frames the recorded search the way
// the MCP streamable-HTTP transport allows.

// ---- fake server

type call struct {
	id     int64
	tool   string
	args   string
	header http.Header
}

// fakeMCP records tool calls and answers them with respond.
type fakeMCP struct {
	t       *testing.T
	server  *httptest.Server
	respond func(w http.ResponseWriter, c call)

	mu    sync.Mutex
	calls []call
}

func newFakeMCP(t *testing.T, respond func(w http.ResponseWriter, c call)) *fakeMCP {
	t.Helper()
	f := &fakeMCP{t: t, respond: respond}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int64  `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&req) != nil ||
			req.JSONRPC != "2.0" || req.Method != "tools/call" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		c := call{id: req.ID, tool: req.Params.Name, args: string(req.Params.Arguments), header: r.Header.Clone()}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		f.mu.Unlock()
		f.respond(w, c)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeMCP) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeMCP) last() call {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		f.t.Fatal("no calls")
	}
	return f.calls[len(f.calls)-1]
}

var firstID = regexp.MustCompile(`"id":\d+`)

// fixture answers with a recorded response, its JSON-RPC id set to the
// request's.
func fixture(t *testing.T, name, contentType string) func(http.ResponseWriter, call) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return func(w http.ResponseWriter, c call) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(firstID.ReplaceAll(data, fmt.Appendf(nil, `"id":%d`, c.id)))
	}
}

// toolText answers with a tool result whose text is the given JSON.
func toolText(text string) func(http.ResponseWriter, call) {
	return func(w http.ResponseWriter, c call) {
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": c.id,
			"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": false},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func testSettings(f *fakeMCP, clk *clock) settings {
	s := defaultSettings()
	s.endpoint = f.server.URL
	s.timeout = 2 * time.Second
	s.rate = rate.Inf
	s.now = clk.now
	return s
}

func newTestClient(t *testing.T, respond func(http.ResponseWriter, call), tune ...func(*settings)) (*Client, *fakeMCP, *clock) {
	t.Helper()
	f := newFakeMCP(t, respond)
	clk := &clock{t: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	s := testSettings(f, clk)
	for _, fn := range tune {
		fn(&s)
	}
	return newClient(s, nil), f, clk
}

func rub(minor int64) *domain.Money { return &domain.Money{Minor: minor, Currency: "RUB"} }

func sameProducts(t *testing.T, got, want []Product) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d products %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.XMLID != w.XMLID || g.Name != w.Name || g.Unit != w.Unit || g.Weight != w.Weight ||
			(g.Price == nil) != (w.Price == nil) || (g.Price != nil && *g.Price != *w.Price) {
			t.Errorf("product %d = %+v (price %v), want %+v (price %v)", i, g, g.Price, w, w.Price)
		}
	}
}

// ---- search

var milk = []Product{
	{XMLID: 36296, Name: "Молоко 2,5% в бутылке, 900 мл", Price: rub(10000), Unit: "шт", Weight: "900 г"},
	{XMLID: 173, Name: "Молоко 3,2%, 1 л", Price: rub(9300), Unit: "шт", Weight: "1 кг"},
	{XMLID: 17736, Name: "Молоко цельное в бутылке, 900 мл", Price: rub(10700), Unit: "шт", Weight: "924 г"},
}

func TestSearchParsesRecordedResponse(t *testing.T) {
	c, f, _ := newTestClient(t, fixture(t, "search_milk.json", "application/json"))
	got, err := c.Search(context.Background(), "  Молоко \t ")
	if err != nil {
		t.Fatal(err)
	}
	sameProducts(t, got, milk)

	sent := f.last()
	if sent.tool != "vkusvill_products_search" {
		t.Errorf("tool = %q", sent.tool)
	}
	if want := `{"q":"Молоко","vvonly":0,"mode":"custom","fields":["id","xml_id","name","price","unit","weight"]}`; sent.args != want {
		t.Errorf("arguments = %s, want %s", sent.args, want)
	}
	if sent.header.Get("Content-Type") != "application/json" || sent.header.Get("Accept") != "application/json, text/event-stream" {
		t.Errorf("headers = %v", sent.header)
	}
}

func TestSearchLooseGoodsHaveNoWeight(t *testing.T) {
	c, _, _ := newTestClient(t, fixture(t, "search_bananas.json", "application/json"))
	got, err := c.Search(context.Background(), "бананы")
	if err != nil {
		t.Fatal(err)
	}
	sameProducts(t, got, []Product{
		{XMLID: 731, Name: "Бананы", Price: rub(16800), Unit: "кг"},
		{XMLID: 56310, Name: "Банан, шт", Price: rub(4500), Unit: "шт", Weight: "150 г"},
	})
}

func TestSearchReadsEventStream(t *testing.T) {
	c, _, _ := newTestClient(t, fixture(t, "search_milk.sse", "text/event-stream"))
	got, err := c.Search(context.Background(), "молоко")
	if err != nil {
		t.Fatal(err)
	}
	sameProducts(t, got, milk)
}

func TestSearchWithoutResultsIsCached(t *testing.T) {
	c, f, _ := newTestClient(t, fixture(t, "search_empty.json", "application/json"))
	for range 2 {
		got, err := c.Search(context.Background(), "zzqxjwvkq")
		if err != nil || len(got) != 0 {
			t.Fatalf("Search = %+v, %v", got, err)
		}
	}
	if f.count() != 1 {
		t.Errorf("server calls = %d, want 1", f.count())
	}
	if got, err := c.Search(context.Background(), " \n "); err != nil || got != nil || f.count() != 1 {
		t.Errorf("blank query = %+v, %v after %d calls", got, err, f.count())
	}
}

func TestSearchSkipsUnusableProducts(t *testing.T) {
	c, _, _ := newTestClient(t, toolText(`{"ok":true,"data":{"items":[
		{"xml_id":"x","name":"Строка вместо числа"},
		{"xml_id":0,"name":"Без номера"},
		{"xml_id":5,"name":"  "},
		{"xml_id":6,"name":"Сыр\u0000 &quot;Российский&quot;","price":{"current":null,"currency":"RUB"},"unit":null},
		{"xml_id":7,"name":"Кофе","price":{"current":5,"currency":"USD"},"weight":{"value":"0.25","unit":"кг"}},
		{"xml_id":8,"name":"Чай","price":{"current":104.90000000000001,"currency":"RUB"},"unit":"шт"},
		{"xml_id":9,"name":"Лишний"}
	]}}`))
	got, err := c.Search(context.Background(), "что угодно")
	if err != nil {
		t.Fatal(err)
	}
	sameProducts(t, got, []Product{
		{XMLID: 6, Name: `Сыр "Российский"`},
		{XMLID: 7, Name: "Кофе", Weight: "250 г"},
		{XMLID: 8, Name: "Чай", Price: rub(10490), Unit: "шт"},
	})
}

// ---- cache

func TestSearchCache(t *testing.T) {
	c, f, clk := newTestClient(t, fixture(t, "search_milk.json", "application/json"), func(s *settings) { s.cacheLimit = 2 })
	ctx := context.Background()
	search := func(q string) []Product {
		t.Helper()
		got, err := c.Search(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	first := search("молоко")
	first[0].Price.Minor = 1 // callers get copies
	first[0].Name = "changed"
	sameProducts(t, search("  МОЛОКО  "), milk)
	if f.count() != 1 {
		t.Fatalf("calls = %d, want 1 (normalized queries share an entry)", f.count())
	}

	clk.add(6*time.Hour - time.Second)
	search("молоко")
	if f.count() != 1 {
		t.Fatalf("calls = %d, want a hit before the TTL", f.count())
	}
	clk.add(time.Second)
	search("молоко")
	if f.count() != 2 {
		t.Fatalf("calls = %d, want a miss at the TTL", f.count())
	}

	// With room for two entries, a third query evicts the oldest one.
	clk.add(time.Minute)
	search("кефир")
	clk.add(time.Minute)
	search("сметана")
	calls := f.count()
	search("кефир")
	if f.count() != calls {
		t.Error("the newer entry must survive eviction")
	}
	search("молоко")
	if f.count() != calls+1 {
		t.Error("the oldest entry must be evicted")
	}
}

func TestFailuresAreNotCached(t *testing.T) {
	fail := true
	var mu sync.Mutex
	ok := fixture(t, "search_milk.json", "application/json")
	c, f, _ := newTestClient(t, func(w http.ResponseWriter, cl call) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			fail = false
			http.Error(w, "busy", http.StatusBadGateway)
			return
		}
		ok(w, cl)
	})
	if _, err := c.Search(context.Background(), "молоко"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	got, err := c.Search(context.Background(), "молоко")
	if err != nil || len(got) != 3 || f.count() != 2 {
		t.Fatalf("retry = %d products, %v, %d calls", len(got), err, f.count())
	}
}

func TestEstimate(t *testing.T) {
	c, _, clk := newTestClient(t, fixture(t, "search_milk.json", "application/json"))
	if _, err := c.Search(context.Background(), "молоко"); err != nil {
		t.Fatal(err)
	}
	total, ok := c.Estimate([]Line{{XMLID: 173, Quantity: "2"}, {XMLID: 36296, Quantity: "1.5"}, {XMLID: 17736, Quantity: "0,33"}})
	// 93×2 + 100×1.5 + 107×0.33 = 186 + 150 + 35.31
	if !ok || total != (domain.Money{Minor: 37131, Currency: "RUB"}) {
		t.Errorf("Estimate = %+v, %v", total, ok)
	}
	if _, ok := c.Estimate([]Line{{XMLID: 173, Quantity: "1"}, {XMLID: 999, Quantity: "1"}}); ok {
		t.Error("an unknown price must give no estimate")
	}
	clk.add(7 * time.Hour)
	if _, ok := c.Estimate([]Line{{XMLID: 173, Quantity: "1"}}); ok {
		t.Error("an expired price must give no estimate")
	}
}

// ---- cart

func TestCreateCart(t *testing.T) {
	c, f, _ := newTestClient(t, fixture(t, "cart.json", "application/json"))
	link, err := c.CreateCart(context.Background(), []Line{
		{XMLID: 173, Quantity: "1"}, {XMLID: 731, Quantity: "0,5"}, {XMLID: 173, Quantity: "1.25"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if link != "https://vkusvill.ru/?share_basket=1239890060" {
		t.Errorf("link = %q", link)
	}
	sent := f.last()
	if want := `{"products":[{"xml_id":173,"q":2.25},{"xml_id":731,"q":0.5}]}`; sent.tool != "vkusvill_cart_link_create" || sent.args != want {
		t.Errorf("sent %s %s, want %s", sent.tool, sent.args, want)
	}
}

func TestCreateCartSendsThirtyLinesInOneCall(t *testing.T) {
	c, f, _ := newTestClient(t, fixture(t, "cart.json", "application/json"))
	lines := make([]Line, MaxCartLines)
	for i := range lines {
		lines[i] = Line{XMLID: i + 1, Quantity: "40"}
	}
	if _, err := c.CreateCart(context.Background(), lines); err != nil {
		t.Fatal(err)
	}
	if f.count() != 1 || strings.Count(f.last().args, "xml_id") != MaxCartLines {
		t.Errorf("calls = %d, args = %s", f.count(), f.last().args)
	}
}

func TestCreateCartValidatesLines(t *testing.T) {
	c, f, _ := newTestClient(t, fixture(t, "cart.json", "application/json"))
	many := make([]Line, MaxCartLines+1)
	for i := range many {
		many[i] = Line{XMLID: i + 1, Quantity: "1"}
	}
	cases := map[string][]Line{
		"no lines":           nil,
		"too many lines":     many,
		"zero id":            {{XMLID: 0, Quantity: "1"}},
		"negative id":        {{XMLID: -5, Quantity: "1"}},
		"huge id":            {{XMLID: 1_000_000_000, Quantity: "1"}},
		"zero quantity":      {{XMLID: 1, Quantity: "0"}},
		"empty quantity":     {{XMLID: 1, Quantity: ""}},
		"above 40":           {{XMLID: 1, Quantity: "40.01"}},
		"three decimals":     {{XMLID: 1, Quantity: "1.234"}},
		"not a number":       {{XMLID: 1, Quantity: "два"}},
		"negative quantity":  {{XMLID: 1, Quantity: "-1"}},
		"merged above 40":    {{XMLID: 1, Quantity: "30"}, {XMLID: 1, Quantity: "11"}},
		"exponent":           {{XMLID: 1, Quantity: "1e1"}},
		"a unit in quantity": {{XMLID: 1, Quantity: "1 кг"}},
	}
	for name, lines := range cases {
		if _, err := c.CreateCart(context.Background(), lines); !errors.Is(err, ErrInvalidCart) {
			t.Errorf("%s: err = %v, want ErrInvalidCart", name, err)
		}
	}
	if f.count() != 0 {
		t.Errorf("invalid carts reached the server %d times", f.count())
	}
}

func TestBasketURL(t *testing.T) {
	ok := map[string]string{
		"https://vkusvill.ru/?share_basket=2063312749":     "https://vkusvill.ru/?share_basket=2063312749",
		"https://www.vkusvill.ru/?share_basket=1":          "https://www.vkusvill.ru/?share_basket=1",
		"https://vkusvill.ru?share_basket=5":               "https://vkusvill.ru/?share_basket=5",
		"HTTPS://vkusvill.ru/?share_basket=0012345678":     "https://vkusvill.ru/?share_basket=0012345678",
		"https://vkusvill.ru/?share_basket=12345678901234": "https://vkusvill.ru/?share_basket=12345678901234",
	}
	for in, want := range ok {
		if got, err := basketURL(in); err != nil || got != want {
			t.Errorf("basketURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"",
		"http://vkusvill.ru/?share_basket=1",
		"javascript:alert(1)//vkusvill.ru/?share_basket=1",
		"//vkusvill.ru/?share_basket=1",
		"vkusvill.ru/?share_basket=1",
		"https://evil.example/?share_basket=1",
		"https://vkusvill.ru.evil.example/?share_basket=1",
		"https://evilvkusvill.ru/?share_basket=1",
		"https://m.vkusvill.ru/?share_basket=1",
		"https://VKUSVILL.RU/?share_basket=1",
		"https://user@vkusvill.ru/?share_basket=1",
		"https://vkusvill.ru:pass@evil.example/?share_basket=1",
		"https://vkusvill.ru:8443/?share_basket=1",
		"https://vkusvill.ru/cart/?share_basket=1",
		"https://vkusvill.ru/?share_basket=1#frag",
		"https://vkusvill.ru/?share_basket=1&next=https://evil.example",
		"https://vkusvill.ru/?share_basket=1&share_basket=2",
		"https://vkusvill.ru/?share_basket=",
		"https://vkusvill.ru/?share_basket=12a",
		"https://vkusvill.ru/?share_basket=-1",
		"https://vkusvill.ru/?share_basket=123456789012345678901",
		"https://vkusvill.ru/?share_basket=1%0Aevil",
		"https://vkusvill.ru/?basket=1",
		"https://vkusvill.ru\\@evil.example/?share_basket=1",
		"data:text/html,<script>alert(1)</script>",
	}
	for _, in := range bad {
		if got, err := basketURL(in); err == nil {
			t.Errorf("basketURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestCreateCartRejectsForeignLink(t *testing.T) {
	c, _, _ := newTestClient(t, toolText(`{"ok":true,"data":{"link":"https://evil.example/?share_basket=1"}}`))
	link, err := c.CreateCart(context.Background(), []Line{{XMLID: 173, Quantity: "1"}})
	if !errors.Is(err, ErrUnavailable) || link != "" {
		t.Fatalf("CreateCart = %q, %v; want ErrUnavailable", link, err)
	}
}

// ---- transport failures

func TestCallFailuresAreUnavailable(t *testing.T) {
	const marker = "BODY-MARKER-7f3a"
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the client followed a redirect")
	}))
	defer other.Close()

	jsonReply := func(body string) func(http.ResponseWriter, call) {
		return func(w http.ResponseWriter, c call) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, body, c.id)
		}
	}
	cases := map[string]func(http.ResponseWriter, call){
		"server error": func(w http.ResponseWriter, _ call) {
			http.Error(w, marker, http.StatusInternalServerError)
		},
		"redirect": func(w http.ResponseWriter, _ call) {
			w.Header().Set("Location", other.URL)
			w.WriteHeader(http.StatusTemporaryRedirect)
		},
		"html page": func(w http.ResponseWriter, _ call) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>" + marker + "</html>"))
		},
		"oversized body": func(w http.ResponseWriter, c call) {
			w.Header().Set("Content-Type", "application/json")
			text := `{"ok":true,"data":{"items":[],"pad":"` + strings.Repeat("x", maxResponseBytes) + `"}}`
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.id, "result": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": text}}}})
			_, _ = w.Write(body)
		},
		"JSON-RPC error":   fixture(t, "cart_too_many.json", "application/json"),
		"tool error":       fixture(t, "cart_empty_input.json", "application/json"),
		"isError":          jsonReply(`{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"` + marker + `"}],"isError":true}}`),
		"no result":        jsonReply(`{"jsonrpc":"2.0","id":%d}`),
		"no content":       jsonReply(`{"jsonrpc":"2.0","id":%d,"result":{"content":[]}}`),
		"text is not JSON": jsonReply(`{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"` + marker + `"}]}}`),
		"data is not JSON": jsonReply(`{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"{\"ok\":true,\"data\":{\"items\":\"` + marker + `\"}}"}]}}`),
		"wrong id": func(w http.ResponseWriter, c call) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"{\"ok\":true,\"data\":{\"items\":[]}}"}]}}`, c.id+1)
		},
		"empty event stream": func(w http.ResponseWriter, _ call) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(": ping\n\n"))
		},
		"timeout": func(w http.ResponseWriter, _ call) {
			time.Sleep(300 * time.Millisecond)
		},
	}
	for name, respond := range cases {
		t.Run(name, func(t *testing.T) {
			c, _, _ := newTestClient(t, respond, func(s *settings) { s.timeout = 150 * time.Millisecond })
			_, err := c.Search(context.Background(), "молоко")
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
			if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "xxxx") {
				t.Errorf("error leaks the response body: %v", err)
			}
		})
	}
}

func TestFromEventStream(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`
	ok := map[string]string{
		"single event":         "event: message\ndata: " + response + "\n\n",
		"after a notification": "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n: comment\ndata: " + response + "\n\n",
		"after another id":     "data: {\"jsonrpc\":\"2.0\",\"id\":6,\"result\":{}}\n\ndata: " + response + "\n\n",
		"CRLF line endings":    "event: message\r\ndata: " + response + "\r\n\r\n",
		"data split in lines":  "data: {\"jsonrpc\":\"2.0\",\ndata: \"id\":7,\"result\":{\"content\":[]}}\n\n",
		"no final blank line":  "data:" + response,
	}
	for name, stream := range ok {
		if msg, err := fromEventStream([]byte(stream), 7); err != nil || msg.Result == nil {
			t.Errorf("%s: %+v, %v", name, msg, err)
		}
	}
	bad := map[string]string{
		"empty":        "",
		"only comment": ": ping\n\n",
		"other id":     "data: {\"jsonrpc\":\"2.0\",\"id\":8,\"result\":{}}\n\n",
		"not JSON":     "data: hello\n\n",
		"wrong field":  "info: " + response + "\n\n",
	}
	for name, stream := range bad {
		if _, err := fromEventStream([]byte(stream), 7); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestRateLimiterHonoursContext(t *testing.T) {
	c, f, _ := newTestClient(t, fixture(t, "search_empty.json", "application/json"), func(s *settings) {
		s.rate = rate.Every(time.Hour)
		s.burst = 1
	})
	if _, err := c.Search(context.Background(), "первый"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := c.Search(ctx, "второй")
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want ErrUnavailable and context.Canceled", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("waited %v after cancellation", waited)
	}
	deadline, cancelDeadline := context.WithTimeout(context.Background(), time.Minute)
	defer cancelDeadline()
	if _, err := c.Search(deadline, "третий"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a wait past the deadline must fail at once: %v", err)
	}
	if f.count() != 1 {
		t.Errorf("server calls = %d, want 1", f.count())
	}
}

// ---- formatting helpers

func TestRubles(t *testing.T) {
	cases := []struct {
		current, currency string
		want              int64 // 0 = nil
	}{
		{"93", "RUB", 9300},
		{"104.9", "RUB", 10490},
		{"104.90000000000001", "RUB", 10490},
		{"0.5", "", 50},
		{"1e3", "RUB", 100000},
		{"0", "RUB", 0},
		{"-5", "RUB", 0},
		{"", "RUB", 0},
		{"abc", "RUB", 0},
		{"5", "USD", 0},
		{"1e300", "RUB", 0},
	}
	for _, c := range cases {
		got := rubles(json.Number(c.current), c.currency)
		switch {
		case c.want == 0 && got != nil:
			t.Errorf("rubles(%q, %q) = %+v, want nil", c.current, c.currency, *got)
		case c.want != 0 && (got == nil || *got != domain.Money{Minor: c.want, Currency: "RUB"}):
			t.Errorf("rubles(%q, %q) = %v, want %d kopecks", c.current, c.currency, got, c.want)
		}
	}
}

func TestWeight(t *testing.T) {
	cases := []struct{ value, unit, want string }{
		{"0.9", "кг", "900 г"},
		{"0.924", "кг", "924 г"},
		{"1", "кг", "1 кг"},
		{"1.3999999999999999", "кг", "1,4 кг"},
		{"0.45", "л", "450 мл"},
		{"2", "шт", "2 шт"},
		{"0", "кг", ""},
		{"-1", "кг", ""},
		{"1", "", ""},
		{"x", "кг", ""},
	}
	for _, c := range cases {
		if got := weight(json.Number(c.value), c.unit); got != c.want {
			t.Errorf("weight(%s, %q) = %q, want %q", c.value, c.unit, got, c.want)
		}
	}
}

func TestDefaults(t *testing.T) {
	s := defaultSettings()
	if s.endpoint != Endpoint || s.timeout != 8*time.Second || s.cacheTTL != 6*time.Hour || s.cacheLimit != 500 {
		t.Errorf("defaults = %+v", s)
	}
	c := New(nil)
	if c.endpoint != "https://mcp.vkusvill.ru/mcp" || c.http.Timeout != 8*time.Second || c.http.CheckRedirect == nil {
		t.Errorf("client = %+v", c.http)
	}
	if got := normalizeQuery(strings.Repeat("я", 300)); len([]rune(got)) != maxQueryRunes {
		t.Errorf("query not capped: %d runes", len([]rune(got)))
	}
}
