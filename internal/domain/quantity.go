package domain

import (
	"strconv"
	"strings"
	"unicode"
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

const (
	hundredthsPerUnit = 100
	// MaxQuantityHundredths caps an amount at 100 000 (e.g. 100 000 г).
	MaxQuantityHundredths int64 = 100_000 * hundredthsPerUnit
	// MaxItemNameLen caps ingredient and shopping-item names.
	MaxItemNameLen = 80
)

// nbsp joins an amount and its unit, so that a line never breaks between
// them.
const nbsp = "\u00a0"

// Quantity is an optional amount (in hundredths, 0 = none) with an optional
// unit, e.g. 1.5 кг, 3 шт, "по вкусу" or a bare number.
type Quantity struct {
	Hundredths int64
	Unit       Unit
}

// ParseQuantity builds a quantity from an amount as ParseQuantityAmount
// reads it («1,5», «1/2», «1½») and a unit as ParseUnit reads it («ч. л.»,
// «чайные ложки»). It returns nil when both are empty.
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
	h, err := ParseQuantityAmount(amountRaw)
	if err != nil {
		return nil, err
	}
	return &Quantity{Hundredths: h, Unit: unit}, nil
}

// vulgarFractions are the fraction characters accepted in amounts, as
// numerator and denominator.
var vulgarFractions = map[rune][2]int64{
	'½': {1, 2}, '⅓': {1, 3}, '⅔': {2, 3}, '¼': {1, 4}, '¾': {3, 4},
	'⅕': {1, 5}, '⅖': {2, 5}, '⅗': {3, 5}, '⅘': {4, 5}, '⅒': {1, 10},
}

// vulgarHundredths are the fractional parts that Format shows as a vulgar
// fraction for fraction-friendly units; thirds are stored rounded.
var vulgarHundredths = map[int64]string{25: "¼", 33: "⅓", 50: "½", 67: "⅔", 75: "¾"}

func badAmount() error {
	return invalid("amount", "укажите дробь вида 1/2, 1/3, 1/4 или десятичную, например 0,5")
}

// ParseQuantityAmount reads an amount into hundredths: a decimal with at
// most two decimals («0,5», «1.5», «250»), a fraction («1/2», «3/4»), a
// mixed number («1 1/2») or a vulgar fraction, alone or after a whole part
// («½», «1½», «1 ½»). Fractions use the denominators 2, 3, 4, 5 and 10;
// thirds round to hundredths (⅓ = 0.33, ⅔ = 0.67). The amount must be
// above zero and at most MaxQuantityHundredths.
func ParseQuantityAmount(raw string) (int64, error) {
	var b strings.Builder
	for _, r := range raw {
		switch f, vulgar := vulgarFractions[r]; {
		case vulgar:
			// «1½» reads as «1 1/2».
			b.WriteString(" " + strconv.FormatInt(f[0], 10) + "/" + strconv.FormatInt(f[1], 10) + " ")
		case r == '⁄' || r == '∕': // fraction slash, division slash
			b.WriteByte('/')
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	var (
		h  int64
		ok bool
	)
	switch fields := strings.Fields(b.String()); len(fields) {
	case 1:
		if strings.Contains(fields[0], "/") {
			h, ok = parseFraction(fields[0], false)
		} else {
			h, ok = parseDecimal(fields[0])
		}
	case 2:
		var whole, frac int64
		whole, ok = parseWhole(fields[0])
		if ok {
			frac, ok = parseFraction(fields[1], true)
		}
		h = whole*hundredthsPerUnit + frac
	}
	switch {
	case !ok:
		return 0, badAmount()
	case h <= 0:
		return 0, invalid("amount", "количество должно быть больше нуля")
	case h > MaxQuantityHundredths:
		return 0, invalid("amount", "слишком большое количество")
	}
	return h, nil
}

// maxAmountDigits bounds every number in an amount, which keeps the
// arithmetic far from overflowing.
const maxAmountDigits = 6

func parseWhole(s string) (int64, bool) {
	if s == "" || len(s) > maxAmountDigits || !isDigits(s) {
		return 0, false
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n, true
}

// parseDecimal reads «1.5» or «1,5» with at most two decimals.
func parseDecimal(s string) (int64, bool) {
	intPart, frac, _ := strings.Cut(strings.ReplaceAll(s, ",", "."), ".")
	units, ok := parseWhole(intPart)
	if !ok || !isDigits(frac) || len(frac) > 2 {
		return 0, false
	}
	for len(frac) < 2 {
		frac += "0"
	}
	f, _ := strconv.ParseInt(frac, 10, 64)
	return units*hundredthsPerUnit + f, true
}

// parseFraction reads «n/d» into hundredths, rounding half up. proper
// requires n < d, as the fraction follows a whole part.
func parseFraction(s string, proper bool) (int64, bool) {
	numRaw, denRaw, found := strings.Cut(s, "/")
	num, okNum := parseWhole(numRaw)
	den, okDen := parseWhole(denRaw)
	if !found || !okNum || !okDen || (proper && num >= den) {
		return 0, false
	}
	switch den {
	case 2, 3, 4, 5, 10:
	default:
		return 0, false
	}
	return (num*2*hundredthsPerUnit + den) / (2 * den), true
}

// Amount renders the numeric part as a canonical decimal ("1.5", "200",
// "0.33"), or "" when there is none. It is the machine form of the API.
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

// FormatAmount renders the numeric part for people, or "" when there is
// none. For a fraction-friendly unit (and a bare number) ½ ¼ ¾ ⅓ ⅔ are
// glued to the whole part («½», «1½», «2⅔»); every other amount uses a
// decimal comma («0,3», «1,5», «250»).
func (q Quantity) FormatAmount() string {
	if q.Hundredths == 0 {
		return ""
	}
	whole, frac := q.Hundredths/hundredthsPerUnit, q.Hundredths%hundredthsPerUnit
	if v, ok := vulgarHundredths[frac]; ok && q.Unit.FractionFriendly() {
		if whole == 0 {
			return v
		}
		return strconv.FormatInt(whole, 10) + v
	}
	return strings.ReplaceAll(q.Amount(), ".", ",")
}

// Label is the unit's word declined for the amount: «чайные ложки» for
// 2 ч. л., «стакана» for 1½ стакана, «г» for any grams, «по вкусу». Without
// an amount it is the dictionary form («чайная ложка»); without a unit it
// is "".
func (q Quantity) Label() string {
	forms := q.Unit.Forms()
	if q.Hundredths == 0 {
		return forms.One
	}
	return forms.For(q.Hundredths)
}

// Format renders the quantity for people, with a no-break space between
// the amount and the declined unit: «2 чайные ложки», «½ чайной ложки»,
// «1¼ кг», «0,3 л», «1,5 г», «по вкусу», or a bare «2».
func (q Quantity) Format() string {
	amount, label := q.FormatAmount(), q.Label()
	switch {
	case amount == "":
		return label
	case label == "":
		return amount
	default:
		return amount + nbsp + label
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
