package domain

import (
	"strings"
	"unicode"
)

// UnitForms are the words of a unit for the amounts it is shown with:
// One «1 чайная ложка», Few «2 чайные ложки», Many «5 чайных ложек» and
// Fraction «½ чайной ложки» (any amount that is not whole takes the
// genitive singular). Abbreviated units (г, кг, мл, л, шт) and «по вкусу»
// never change, so all four forms repeat the code.
type UnitForms struct {
	One      string
	Few      string
	Many     string
	Fraction string
}

// declined holds the units whose words change with the amount; every other
// unit is invariant.
var declined = map[Unit]UnitForms{
	UnitTablespoon: {"столовая ложка", "столовые ложки", "столовых ложек", "столовой ложки"},
	UnitTeaspoon:   {"чайная ложка", "чайные ложки", "чайных ложек", "чайной ложки"},
	UnitCup:        {"стакан", "стакана", "стаканов", "стакана"},
	UnitPinch:      {"щепотка", "щепотки", "щепоток", "щепотки"},
	UnitClove:      {"зубчик", "зубчика", "зубчиков", "зубчика"},
	UnitBunch:      {"пучок", "пучка", "пучков", "пучка"},
	UnitPack:       {"упаковка", "упаковки", "упаковок", "упаковки"},
}

// Forms returns the words of u. An invariant unit repeats its code; the
// empty unit has empty forms.
func (u Unit) Forms() UnitForms {
	if f, ok := declined[u]; ok {
		return f
	}
	s := string(u)
	return UnitForms{One: s, Few: s, Many: s, Fraction: s}
}

// For picks the form for an amount in hundredths: «1 стакан», «3 стакана»,
// «11 стаканов», «1½ стакана».
func (f UnitForms) For(hundredths int64) string {
	switch PluralFor(hundredths) {
	case PluralOne:
		return f.One
	case PluralFew:
		return f.Few
	case PluralFraction:
		return f.Fraction
	default:
		return f.Many
	}
}

// Plural is the grammatical number a Russian noun takes after an amount.
type Plural int

// The four forms of UnitForms, in the same order.
const (
	PluralOne Plural = iota
	PluralFew
	PluralMany
	PluralFraction
)

// PluralFor classifies an amount in hundredths. A whole n takes One when
// it ends in 1 but not in 11 (1, 21, 101), Few when it ends in 2–4 but not
// in 12–14 (2, 23, 104), and Many otherwise (0, 5, 11–14, 20). Any amount
// that is not whole takes Fraction.
func PluralFor(hundredths int64) Plural {
	if hundredths < 0 {
		hundredths = -hundredths
	}
	if hundredths%hundredthsPerUnit != 0 {
		return PluralFraction
	}
	n := hundredths / hundredthsPerUnit
	switch last, lastTwo := n%10, n%100; {
	case last == 1 && lastTwo != 11:
		return PluralOne
	case last >= 2 && last <= 4 && (lastTwo < 12 || lastTwo > 14):
		return PluralFew
	default:
		return PluralMany
	}
}

// FractionFriendly reports whether amounts in u are shown with vulgar
// fractions («½ кг», «1¼ стакана»). Grams and millilitres are precise
// measures and always use a decimal comma («1,5 г»); a bare number is
// fraction friendly.
func (u Unit) FractionFriendly() bool { return u != UnitGram && u != UnitMilliliter }

// unitSpellings are the common ways to write a unit besides its code and
// its forms, as people type them and as recipes from the internet spell
// them. Keys are compared after unitKey.
var unitSpellings = map[Unit][]string{
	UnitGram:       {"гр", "грамм", "грамма", "граммов", "граммы"},
	UnitKilogram:   {"килограмм", "килограмма", "килограммов", "килограммы", "кило"},
	UnitMilliliter: {"миллилитр", "миллилитра", "миллилитров", "миллилитры"},
	UnitLiter:      {"литр", "литра", "литров", "литры"},
	UnitPiece:      {"штук", "штука", "штуки", "штуку"},
	UnitTablespoon: {"стл", "ст ложка", "ст ложки", "ст ложек", "ст ложку", "столовую ложку"},
	UnitTeaspoon:   {"чл", "ч ложка", "ч ложки", "ч ложек", "ч ложку", "чайную ложку"},
	UnitCup:        {"стаканы"},
	UnitPinch:      {"щепоть", "щепотку"},
	UnitClove:      {"зуб", "зубчики"},
	UnitBunch:      {"пучки"},
	UnitPack:       {"уп", "упак", "упаковку", "пачка", "пачки", "пачек", "пачку"},
}

// unitAliases maps every accepted spelling, normalised by unitKey, to its
// unit code.
var unitAliases = buildUnitAliases()

func buildUnitAliases() map[string]Unit {
	out := make(map[string]Unit)
	add := func(spelling string, u Unit) {
		key := unitKey(spelling)
		if prev, ok := out[key]; ok && prev != u {
			panic("domain: unit spelling " + spelling + " is ambiguous")
		}
		out[key] = u
	}
	for _, u := range Units {
		add(string(u), u)
		f := u.Forms()
		for _, s := range [...]string{f.One, f.Few, f.Many, f.Fraction} {
			add(s, u)
		}
		for _, s := range unitSpellings[u] {
			add(s, u)
		}
	}
	return out
}

// unitKey normalises a unit as typed: lower case, ё as е, dots, hyphens and
// any run of spaces as one space, so «Ч.Л.», «ч. л.» and «ч л» meet.
func unitKey(raw string) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r == '.' || r == '-' || unicode.IsSpace(r):
			return ' '
		case r == 'ё' || r == 'Ё':
			return 'е'
		default:
			return unicode.ToLower(r)
		}
	}, raw)
	return strings.Join(strings.Fields(s), " ")
}

// ParseUnit reads a unit: its code («ч. л.»), any of its forms («чайных
// ложек») or a common spelling («ч.л.», «чл», «гр», «шт.», «уп»), in any
// letter case. The result is always the code; the empty string means "no
// unit".
func ParseUnit(raw string) (Unit, error) {
	key := unitKey(raw)
	if key == "" {
		return "", nil
	}
	if u, ok := unitAliases[key]; ok {
		return u, nil
	}
	return "", invalid("unit", "неизвестная единица измерения")
}
