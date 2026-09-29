package vkusvill

import (
	"encoding/json"
	"html"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const (
	maxNameRunes = 200
	maxUnitRunes = 16
	// nbsp joins a number and its unit, as in domain.Money.Format.
	nbsp = " "
)

// searchData is the data of vkusvill_products_search. Items are decoded one
// by one, so a single malformed product is skipped instead of failing the
// whole search.
type searchData struct {
	Items []json.RawMessage `json:"items"`
}

type searchItem struct {
	XMLID int    `json:"xml_id"`
	Name  string `json:"name"`
	Price *struct {
		Current  json.Number `json:"current"`
		Currency string      `json:"currency"`
	} `json:"price"`
	Unit   string `json:"unit"`
	Weight *struct {
		Value json.Number `json:"value"`
		Unit  string      `json:"unit"`
	} `json:"weight"`
}

// products returns the first limit usable products in the server's order.
func (d searchData) products(limit int) []Product {
	out := make([]Product, 0, limit)
	for _, raw := range d.Items {
		if len(out) == limit {
			break
		}
		var it searchItem
		if err := json.Unmarshal(raw, &it); err != nil {
			continue
		}
		name := cleanText(it.Name, maxNameRunes)
		if it.XMLID <= 0 || it.XMLID > MaxXMLID || name == "" {
			continue
		}
		p := Product{XMLID: it.XMLID, Name: name, Unit: cleanText(it.Unit, maxUnitRunes)}
		if it.Price != nil {
			p.Price = rubles(it.Price.Current, it.Price.Currency)
		}
		if it.Weight != nil {
			p.Weight = weight(it.Weight.Value, it.Weight.Unit)
		}
		out = append(out, p)
	}
	return out
}

// cleanText decodes HTML entities (names come as "1&nbsp;л"), drops
// control characters, collapses whitespace and caps the length.
func cleanText(raw string, maxRunes int) string {
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, html.UnescapeString(raw))
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxRunes {
		s = strings.TrimSpace(string([]rune(s)[:maxRunes]))
	}
	return s
}

// rubles converts a price such as 93 or 104.9 into kopecks. The server
// sends prices as JSON numbers, i.e. floats on the wire, so the value is
// rounded to whole kopecks once, right here; money is integer from then on.
func rubles(current json.Number, currency string) *domain.Money {
	if currency != "" && currency != "RUB" {
		return nil
	}
	v, err := strconv.ParseFloat(current.String(), 64)
	if err != nil || math.IsNaN(v) || v <= 0 || v > float64(domain.MaxAmountMinor)/100 {
		return nil
	}
	m, err := domain.NewMoney(int64(math.Round(v*100)), "RUB")
	if err != nil {
		return nil
	}
	return &m
}

// weight renders a net weight for people: 0.9 кг → "900 г", 1.4 кг →
// "1,4 кг". Anything unusable gives "".
func weight(value json.Number, rawUnit string) string {
	v, err := strconv.ParseFloat(value.String(), 64)
	unit := cleanText(rawUnit, maxUnitRunes)
	if err != nil || math.IsNaN(v) || v <= 0 || v >= 1e6 || unit == "" {
		return ""
	}
	sub := ""
	switch unit {
	case "кг":
		sub = "г"
	case "л":
		sub = "мл"
	}
	if sub != "" && v < 1 {
		return strconv.FormatFloat(math.Round(v*1000), 'f', -1, 64) + nbsp + sub
	}
	num := strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
	return strings.Replace(num, ".", ",", 1) + nbsp + unit
}

// cloneProducts copies products so that callers cannot change cached ones.
func cloneProducts(in []Product) []Product {
	out := make([]Product, len(in))
	for i, p := range in {
		if p.Price != nil {
			price := *p.Price
			p.Price = &price
		}
		out[i] = p
	}
	return out
}
