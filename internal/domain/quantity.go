package domain

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Unit is a kitchen unit from a fixed list, so shopping-list items with the
// same name and unit can be merged and a future nutrition calculator can
// convert amounts to grams.
type Unit string

// Units in display order. UnitToTaste never has a numeric amount.
const (
	UnitGram       Unit = "г"
	UnitKilogram   Unit = "кг"
	UnitMilliliter Unit = "мл"
	UnitLiter      Unit = "л"
	UnitPiece      Unit = "шт"
	UnitTablespoon Unit = "ст. л."
	UnitTeaspoon   Unit = "ч. л."
	UnitCup        Unit = "стакан"
	UnitPinch      Unit = "щепотка"
	UnitClove      Unit = "зубчик"
	UnitBunch      Unit = "пучок"
	UnitPack       Unit = "упаковка"
	UnitToTaste    Unit = "по вкусу"
)

// Units lists every supported unit in display order.
var Units = [...]Unit{
	UnitGram, UnitKilogram, UnitMilliliter, UnitLiter, UnitPiece, UnitTablespoon,
	UnitTeaspoon, UnitCup, UnitPinch, UnitClove, UnitBunch, UnitPack, UnitToTaste,
}

// Related is the other unit of the same metric pair (г↔кг, мл↔л), or u
// itself. Amounts in related units can be added up.
func (u Unit) Related() Unit {
	switch u {
	case UnitGram:
		return UnitKilogram
	case UnitKilogram:
		return UnitGram
	case UnitMilliliter:
		return UnitLiter
	case UnitLiter:
		return UnitMilliliter
	}
	return u
}

// addQuantities sums two amounts in the same or related units. A mixed sum
// (200 мл + 0,5 л) is expressed in the larger unit when that is exact and at
// least 1 (1,5 кг), otherwise in the smaller one (700 мл, 1333 г). ok is false
// for unrelated units or when the sum exceeds MaxQuantityHundredths.
func addQuantities(a, b Quantity) (sum Quantity, ok bool) {
	switch {
	case a.Unit == b.Unit:
		sum = Quantity{Hundredths: a.Hundredths + b.Hundredths, Unit: a.Unit}
	case a.Unit.Related() == b.Unit:
		small, large := a.Unit, b.Unit
		if small == UnitKilogram || small == UnitLiter {
			small, large = large, small
		}
		inSmall := func(q Quantity) int64 {
			if q.Unit == large {
				return q.Hundredths * 1000
			}
			return q.Hundredths
		}
		total := inSmall(a) + inSmall(b)
		sum = Quantity{Hundredths: total, Unit: small}
		if total%1000 == 0 && total >= 1000*hundredthsPerUnit {
			sum = Quantity{Hundredths: total / 1000, Unit: large}
		}
	default:
		return Quantity{}, false
	}
	return sum, sum.Hundredths <= MaxQuantityHundredths
}

// ParseUnit validates a unit; the empty string means "no unit".
func ParseUnit(raw string) (Unit, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	for _, u := range Units {
		if string(u) == s {
			return u, nil
		}
	}
	return "", invalid("unit", "неизвестная единица измерения")
}

const (
	hundredthsPerUnit = 100
	// MaxQuantityHundredths caps an amount at 100 000 (e.g. 100 000 г).
	MaxQuantityHundredths int64 = 100_000 * hundredthsPerUnit
	// MaxItemNameLen caps ingredient and shopping-item names.
	MaxItemNameLen = 80
)

// Quantity is an optional amount (in hundredths, 0 = none) with an optional
// unit, e.g. 1.5 кг, 3 шт, "по вкусу" or a bare number.
type Quantity struct {
	Hundredths int64
	Unit       Unit
}

// ParseQuantity builds a quantity from a decimal amount such as "1.5" or
// "1,5" and a unit. It returns nil when both are empty.
func ParseQuantity(amountRaw, unitRaw string) (*Quantity, error) {
	unit, err := ParseUnit(unitRaw)
	if err != nil {
		return nil, err
	}
	amountRaw = strings.TrimSpace(amountRaw)
	if amountRaw == "" && unit == "" {
		return nil, nil
	}
	if unit == UnitToTaste {
		if amountRaw != "" {
			return nil, invalid("amount", "для «по вкусу» количество не указывается")
		}
		return &Quantity{Unit: unit}, nil
	}
	if amountRaw == "" {
		return nil, invalid("amount", "укажите количество")
	}
	h, err := parseHundredths(amountRaw)
	if err != nil {
		return nil, err
	}
	return &Quantity{Hundredths: h, Unit: unit}, nil
}

func parseHundredths(raw string) (int64, error) {
	s := strings.ReplaceAll(strings.ReplaceAll(raw, " ", ""), ",", ".")
	intPart, frac, _ := strings.Cut(s, ".")
	if intPart == "" || !isDigits(intPart) || !isDigits(frac) || len(frac) > 2 || len(intPart) > 6 {
		return 0, invalid("amount", "количество — число, например 1,5")
	}
	units, _ := strconv.ParseInt(intPart, 10, 64)
	for len(frac) < 2 {
		frac += "0"
	}
	f, _ := strconv.ParseInt(frac, 10, 64)
	h := units*hundredthsPerUnit + f
	switch {
	case h <= 0:
		return 0, invalid("amount", "количество должно быть больше нуля")
	case h > MaxQuantityHundredths:
		return 0, invalid("amount", "слишком большое количество")
	}
	return h, nil
}

// Amount renders the numeric part as a canonical decimal ("1.5", "200"),
// or "" when there is none.
func (q Quantity) Amount() string {
	if q.Hundredths == 0 {
		return ""
	}
	units, frac := q.Hundredths/hundredthsPerUnit, q.Hundredths%hundredthsPerUnit
	if frac == 0 {
		return strconv.FormatInt(units, 10)
	}
	s := strconv.FormatInt(units, 10) + "." + twoDigits(frac)
	return strings.TrimRight(s, "0")
}

// Format renders the quantity for people: "1,5 кг", "3 шт", "по вкусу".
func (q Quantity) Format() string {
	amount := strings.ReplaceAll(q.Amount(), ".", ",")
	switch {
	case amount == "":
		return string(q.Unit)
	case q.Unit == "":
		return amount
	default:
		return amount + " " + string(q.Unit)
	}
}

// NormalizeItemName validates an ingredient or shopping-item name.
func NormalizeItemName(raw string) (string, error) {
	s := cleanText(raw, false)
	switch n := utf8.RuneCountInString(s); {
	case n == 0:
		return "", invalid("name", "укажите название")
	case n > MaxItemNameLen:
		return "", invalid("name", "название слишком длинное (максимум 80 символов)")
	}
	return s, nil
}
