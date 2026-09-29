// Package stores lists the grocery stores the Mini App can search in. Each
// store is a deep-link search URL template; the client substitutes the
// query and opens the link in the browser. The server never fetches these
// sites.
package stores

import "strings"

// Placeholder is replaced by the URL-encoded search query. It always sits in
// the query string of a template, so a query can never change the host or
// the path of the link.
const Placeholder = "{q}"

// Store is one grocery store with a search deep link.
type Store struct {
	ID             string
	Name           string
	Emoji          string
	SearchTemplate string
	// OpensApp: the store's apple-app-site-association covers the search
	// path, so on iOS the link opens the store app instead of the browser.
	OpensApp bool
	// Cart: a whole shopping list can become a real cart in this store (only
	// ВкусВилл, through its official MCP server; see internal/vkusvill).
	Cart bool
}

// SearchURL fills the template with an already URL-encoded query.
func (s Store) SearchURL(encodedQuery string) string {
	return strings.Replace(s.SearchTemplate, Placeholder, encodedQuery, 1)
}

// catalog is the single table of supported stores, in display order.
//
// Templates were checked on 2026-09-29: ВкусВилл, Магнит, Лавка and Metro by
// fetching search results; the others against the stores' own search URLs
// (their pages sit behind anti-bot challenges, so a browser opens them fine
// but a server cannot). OpensApp comes from each store's
// apple-app-site-association file. The Mini App opens only these hosts
// (STORE_HOSTS in web/src/lib/stores.ts; a web test keeps the two in sync).
var catalog = [...]Store{
	{ID: "vkusvill", Name: "ВкусВилл", Emoji: "🥬", SearchTemplate: "https://vkusvill.ru/search/?q={q}", Cart: true},
	{ID: "perekrestok", Name: "Перекрёсток", Emoji: "🛒", SearchTemplate: "https://www.perekrestok.ru/cat/search?search={q}"},
	{ID: "magnit", Name: "Магнит", Emoji: "🧲", SearchTemplate: "https://magnit.ru/search?term={q}"},
	{ID: "5ka", Name: "Пятёрочка", Emoji: "5️⃣", SearchTemplate: "https://5ka.ru/search/?text={q}"},
	{ID: "lavka", Name: "Яндекс Лавка", Emoji: "🟡", SearchTemplate: "https://lavka.yandex.ru/search?text={q}"},
	{ID: "samokat", Name: "Самокат", Emoji: "🛴", SearchTemplate: "https://samokat.ru/search?value={q}"},
	{ID: "kuper", Name: "Купер", Emoji: "🧺", SearchTemplate: "https://kuper.ru/multisearch?q={q}", OpensApp: true},
	{ID: "lenta", Name: "Лента", Emoji: "🟦", SearchTemplate: "https://lenta.com/search/?searchText={q}"},
	{ID: "auchan", Name: "Ашан", Emoji: "🐦", SearchTemplate: "https://www.auchan.ru/search/?query={q}", OpensApp: true},
	{ID: "metro", Name: "METRO", Emoji: "🏬", SearchTemplate: "https://online.metro-cc.ru/search?q={q}", OpensApp: true},
	{ID: "av", Name: "Азбука вкуса", Emoji: "🍷", SearchTemplate: "https://av.ru/search?freeText={q}"},
}

// All returns every store in display order. The slice is a fresh copy, so
// callers may keep or modify it.
func All() []Store { return append([]Store(nil), catalog[:]...) }
