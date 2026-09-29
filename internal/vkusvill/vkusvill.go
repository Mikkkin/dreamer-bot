// Package vkusvill is a client for ВкусВилл's official, experimental MCP
// server, announced on the company's Habr blog on 2025-12-30. It finds
// products for shopping-list items and turns chosen products into a shared
// basket on vkusvill.ru.
//
// The server speaks stateless streamable-HTTP JSON-RPC and needs no key, so
// every tool call is one POST: no MCP session and no SDK. Only the fixed
// Endpoint is ever contacted, and the only data sent are product names and
// product IDs with quantities.
package vkusvill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/time/rate"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Endpoint is the only URL the client talks to. It is never configurable.
const Endpoint = "https://mcp.vkusvill.ru/mcp"

const (
	// MaxCartLines is the tool's limit (maxItems in its input schema). Its
	// description says 1..20, but a 30-line basket was created in one call
	// on 2026-09-30, so one call always suffices and lines are never split
	// into several baskets.
	MaxCartLines = 30
	// maxCandidates is how many products a search offers per item.
	maxCandidates = 3
	// maxQuantityHundredths is the tool's upper bound for q (40).
	maxQuantityHundredths = 40 * 100
	// MaxXMLID is the largest product ID the tool accepts.
	MaxXMLID = 999_999_999
	// maxQueryRunes is the tool's maxLength for the search query.
	maxQueryRunes = 255
)

var (
	// ErrUnavailable means ВкусВилл did not give a usable answer: a network
	// error, a timeout, a non-200 status, a JSON-RPC or tool error, a
	// malformed or oversized response, or a basket link that failed the
	// checks. Callers fall back to plain search links.
	ErrUnavailable = errors.New("vkusvill: service unavailable")
	// ErrInvalidCart means the cart lines break the tool's limits.
	ErrInvalidCart = errors.New("vkusvill: invalid cart")
)

// Product is a ВкусВилл product offered for a shopping-list item.
type Product struct {
	XMLID int
	Name  string
	// Price is per Unit, in RUB; nil when unknown.
	Price *domain.Money
	// Unit is how the product is sold, e.g. "шт" or "кг"; "" when unknown.
	Unit string
	// Weight is the net weight of one piece, e.g. "900 г"; "" when unknown
	// (loose goods sold by the kilogram have none).
	Weight string
}

// Line is one product of a basket.
type Line struct {
	XMLID int
	// Quantity is a decimal from 0.01 to 40, see ParseQuantity.
	Quantity string
}

// ParseQuantity reads a basket quantity: a decimal from 0.01 to 40 with at
// most two decimals ("2", "0.5", "1,5"). It returns hundredths.
func ParseQuantity(raw string) (int64, error) {
	q, err := domain.ParseQuantity(raw, "")
	if err != nil || q == nil || q.Hundredths > maxQuantityHundredths {
		return 0, fmt.Errorf("%w: quantity must be a decimal from 0.01 to 40", ErrInvalidCart)
	}
	return q.Hundredths, nil
}

// settings holds knobs that production never changes but tests do.
type settings struct {
	endpoint   string
	timeout    time.Duration
	rate       rate.Limit
	burst      int
	cacheTTL   time.Duration
	cacheLimit int
	now        func() time.Time
}

func defaultSettings() settings {
	return settings{
		endpoint: Endpoint,
		// The documented contract: slower than 8 s counts as unavailable.
		timeout: 8 * time.Second,
		// The server publishes no limits and is labelled experimental; two
		// calls a second across all users is gentle and still lets a
		// 30-item list be matched well within the request deadline.
		rate:  2,
		burst: 4,
		// Search results barely change within hours, and the couple matches
		// the same staples again and again.
		cacheTTL:   6 * time.Hour,
		cacheLimit: 500,
		now:        time.Now,
	}
}

// Client calls the ВкусВилл MCP server. It is safe for concurrent use.
type Client struct {
	endpoint string
	http     *http.Client
	limiter  *rate.Limiter
	cache    *cache
	log      *slog.Logger
	lastID   atomic.Int64
}

// New returns a client for Endpoint. log may be nil.
func New(log *slog.Logger) *Client { return newClient(defaultSettings(), log) }

func newClient(s settings, log *slog.Logger) *Client {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Client{
		endpoint: s.endpoint,
		http: &http.Client{
			Timeout: s.timeout,
			// A redirect could only lead somewhere other than the fixed
			// endpoint; it is treated as a failed call.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		limiter: rate.NewLimiter(s.rate, s.burst),
		cache:   newCache(s.cacheTTL, s.cacheLimit, s.now),
		log:     log,
	}
}

// Search returns up to three products for a shopping-list item name, best
// first. Results are cached per query (case-insensitive); failures are not.
// An empty query finds nothing without calling the server.
func (c *Client) Search(ctx context.Context, query string) ([]Product, error) {
	q := normalizeQuery(query)
	if q == "" {
		return nil, nil
	}
	key := strings.ToLower(q)
	if products, ok := c.cache.get(key); ok {
		return products, nil
	}
	var data searchData
	if err := c.callTool(ctx, "vkusvill_products_search", searchArgs{
		Q:      q,
		VVOnly: 0, // the default 1 would offer ВкусВилл's own brand only
		Mode:   "custom",
		Fields: []string{"id", "xml_id", "name", "price", "unit", "weight"},
	}, &data); err != nil {
		return nil, err
	}
	products := data.products(maxCandidates)
	c.cache.put(key, products)
	return products, nil
}

// CreateCart creates a shared basket with the given lines and returns its
// link, https://vkusvill.ru/?share_basket=<digits>. Lines naming the same
// product are merged. The link is checked strictly and rebuilt from its
// parts, so nothing else ever reaches the caller.
func (c *Client) CreateCart(ctx context.Context, lines []Line) (string, error) {
	products, err := cartProducts(lines)
	if err != nil {
		return "", err
	}
	var data struct {
		Link string `json:"link"`
	}
	if err := c.callTool(ctx, "vkusvill_cart_link_create", cartArgs{Products: products}, &data); err != nil {
		return "", err
	}
	link, err := basketURL(data.Link)
	if err != nil {
		return "", fmt.Errorf("%w: vkusvill_cart_link_create: %w", ErrUnavailable, err)
	}
	return link, nil
}

// Estimate adds price × quantity over the lines, using the prices of
// recent searches. It reports false when a line's price is unknown, so an
// estimate is never silently partial. Prices change, so it is approximate.
func (c *Client) Estimate(lines []Line) (domain.Money, bool) {
	var (
		sum      int64 // kopecks × hundredths
		currency domain.Currency
	)
	for _, l := range lines {
		h, err := ParseQuantity(l.Quantity)
		if err != nil {
			return domain.Money{}, false
		}
		price, ok := c.cache.price(l.XMLID)
		if !ok || (currency != "" && price.Currency != currency) {
			return domain.Money{}, false
		}
		currency = price.Currency
		sum += price.Minor * h
	}
	total, err := domain.NewMoney((sum+50)/100, currency)
	if err != nil {
		return domain.Money{}, false
	}
	return total, true
}

type searchArgs struct {
	Q      string   `json:"q"`
	VVOnly int      `json:"vvonly"`
	Mode   string   `json:"mode"`
	Fields []string `json:"fields"`
}

type cartArgs struct {
	Products []cartProduct `json:"products"`
}

type cartProduct struct {
	XMLID int `json:"xml_id"`
	// Q is sent as a JSON number with the exact decimal, e.g. 1.5.
	Q json.Number `json:"q"`
}

// cartProducts validates the lines and merges repeated products.
func cartProducts(lines []Line) ([]cartProduct, error) {
	if len(lines) == 0 || len(lines) > MaxCartLines {
		return nil, fmt.Errorf("%w: %d lines, want 1 to %d", ErrInvalidCart, len(lines), MaxCartLines)
	}
	var (
		order []int
		total = make(map[int]int64, len(lines))
	)
	for i, l := range lines {
		if l.XMLID <= 0 || l.XMLID > MaxXMLID {
			return nil, fmt.Errorf("%w: line %d: bad product id", ErrInvalidCart, i)
		}
		h, err := ParseQuantity(l.Quantity)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i, err)
		}
		if _, seen := total[l.XMLID]; !seen {
			order = append(order, l.XMLID)
		}
		total[l.XMLID] += h
		if total[l.XMLID] > maxQuantityHundredths {
			return nil, fmt.Errorf("%w: product %d: more than 40 in total", ErrInvalidCart, l.XMLID)
		}
	}
	out := make([]cartProduct, len(order))
	for i, id := range order {
		out[i] = cartProduct{XMLID: id, Q: json.Number(domain.Quantity{Hundredths: total[id]}.Amount())}
	}
	return out, nil
}

// basketURL accepts only https://vkusvill.ru/?share_basket=<digits> (or
// www.vkusvill.ru) and returns it rebuilt from the checked parts.
func basketURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("basket link is not a URL")
	}
	if u.Scheme != "https" || (u.Host != "vkusvill.ru" && u.Host != "www.vkusvill.ru") ||
		u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.Fragment != "" {
		return "", errors.New("basket link does not point to vkusvill.ru")
	}
	query, err := url.ParseQuery(u.RawQuery)
	basket := query["share_basket"]
	if err != nil || len(query) != 1 || len(basket) != 1 || !isBasketID(basket[0]) {
		return "", errors.New("basket link has an unexpected query")
	}
	return "https://" + u.Host + "/?share_basket=" + basket[0], nil
}

func isBasketID(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// normalizeQuery trims the query, turns control characters into spaces,
// collapses whitespace and cuts it to the tool's length limit.
func normalizeQuery(raw string) string {
	s := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, raw)), " ")
	if utf8.RuneCountInString(s) > maxQueryRunes {
		s = strings.TrimSpace(string([]rune(s)[:maxQueryRunes]))
	}
	return s
}
