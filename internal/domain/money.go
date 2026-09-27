package domain

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Currency is an ISO-4217 code from the supported set.
type Currency string

// Supported currencies in display order. All of them have two minor digits.
var Currencies = [...]Currency{"EUR", "USD", "RUB", "GBP"}

var currencySymbols = map[Currency]string{
	"EUR": "€",
	"USD": "$",
	"RUB": "₽",
	"GBP": "£",
}

// ParseCurrency validates a currency code (case-insensitive).
func ParseCurrency(raw string) (Currency, error) {
	c := Currency(strings.ToUpper(strings.TrimSpace(raw)))
	if _, ok := currencySymbols[c]; !ok {
		return "", invalid("currency", "неподдерживаемая валюта")
	}
	return c, nil
}

// Symbol returns the currency sign, e.g. "€".
func (c Currency) Symbol() string { return currencySymbols[c] }

const (
	minorPerUnit = 100
	// MaxAmountMinor caps a single price at one billion units.
	MaxAmountMinor int64 = 1_000_000_000 * minorPerUnit
)

// Money is an amount in minor units (cents, kopecks) of a single currency.
// Floats are never used for money.
type Money struct {
	Minor    int64
	Currency Currency
}

// NewMoney validates the amount and the currency.
func NewMoney(minor int64, c Currency) (Money, error) {
	if _, err := ParseCurrency(string(c)); err != nil {
		return Money{}, err
	}
	if minor <= 0 {
		return Money{}, invalid("price", "сумма должна быть больше нуля")
	}
	if minor > MaxAmountMinor {
		return Money{}, invalid("price", "слишком большая сумма")
	}
	return Money{Minor: minor, Currency: c}, nil
}

// ParseAmount parses a human-typed amount such as "1200", "1 200,50",
// "1,200.50" or "1.200,5". The last separator followed by one or two digits
// is treated as the decimal separator; any other '.' or ',' is a thousands
// separator. Whitespace, apostrophes and underscores are ignored.
func ParseAmount(raw string, c Currency) (Money, error) {
	s := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '\'' || r == '_' {
			return -1
		}
		return r
	}, raw)
	if s == "" {
		return Money{}, invalid("price", "укажите сумму")
	}

	intPart, fracPart := splitDecimal(s)
	intPart = strings.NewReplacer(".", "", ",", "").Replace(intPart)
	if intPart == "" || !isDigits(intPart) || !isDigits(fracPart) || len(fracPart) > 2 {
		return Money{}, invalid("price", "не получилось распознать сумму")
	}
	if len(intPart) > 12 {
		return Money{}, invalid("price", "слишком большая сумма")
	}

	units, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return Money{}, invalid("price", "не получилось распознать сумму")
	}
	var frac int64
	if fracPart != "" {
		for len(fracPart) < 2 {
			fracPart += "0"
		}
		frac, _ = strconv.ParseInt(fracPart, 10, 64)
	}
	return NewMoney(units*minorPerUnit+frac, c)
}

// splitDecimal returns the integer and fractional parts of s according to
// the separator heuristic documented on ParseAmount.
func splitDecimal(s string) (intPart, fracPart string) {
	idx := strings.LastIndexAny(s, ".,")
	if idx < 0 {
		return s, ""
	}
	sep := s[idx]
	other := byte(',')
	if sep == ',' {
		other = '.'
	}
	digitsAfter := len(s) - idx - 1
	mixed := strings.IndexByte(s, other) >= 0
	single := strings.Count(s, string(sep)) == 1
	if mixed || (single && digitsAfter <= 2) {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Decimal renders the amount as a canonical machine string, e.g. "1200.50"
// or "1200" for whole amounts. It round-trips through ParseAmount.
func (m Money) Decimal() string {
	units, frac := m.Minor/minorPerUnit, m.Minor%minorPerUnit
	if frac == 0 {
		return strconv.FormatInt(units, 10)
	}
	return strconv.FormatInt(units, 10) + "." + twoDigits(frac)
}

// Format renders the amount for people in Russian style: "1 200,50 €".
func (m Money) Format() string {
	units, frac := m.Minor/minorPerUnit, m.Minor%minorPerUnit
	out := groupThousands(strconv.FormatInt(units, 10))
	if frac != 0 {
		out += "," + twoDigits(frac)
	}
	return out + " " + m.Currency.Symbol()
}

func twoDigits(v int64) string {
	if v < 10 {
		return "0" + strconv.FormatInt(v, 10)
	}
	return strconv.FormatInt(v, 10)
}

// groupThousands inserts narrow no-break spaces between groups of three digits.
func groupThousands(digits string) string {
	if len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	head := len(digits) % 3
	if head > 0 {
		b.WriteString(digits[:head])
	}
	for i := head; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

// SumByCurrency adds amounts per currency and returns them sorted by the
// order of Currencies. Different currencies are never mixed or converted.
func SumByCurrency(amounts []Money) []Money {
	totals := make(map[Currency]int64, len(Currencies))
	for _, m := range amounts {
		totals[m.Currency] += m.Minor
	}
	out := make([]Money, 0, len(totals))
	for c, minor := range totals {
		out = append(out, Money{Minor: minor, Currency: c})
	}
	sort.Slice(out, func(i, j int) bool { return currencyRank(out[i].Currency) < currencyRank(out[j].Currency) })
	return out
}

func currencyRank(c Currency) int {
	for i, known := range Currencies {
		if known == c {
			return i
		}
	}
	return len(Currencies)
}
