package recipeimport

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// qty is a quantity found in caption text, before it becomes a
// domain.Quantity.
type qty struct {
	h         int64       // hundredths; 0 = no amount
	number    bool        // an amount was written (a digit, a fraction or a number word)
	unit      domain.Unit // a unit from the fixed list, or ""
	oov       string      // a measure word outside the list («горсть», «кочан»), lower-cased
	toTaste   bool        // «по вкусу»
	optional  bool        // «по желанию», «для подачи»: explicitly no amount
	shared    bool        // «по 1/2 ч.л.»: the amount applies to every food of the line
	approx    bool        // «~», «примерно»
	rangeText string      // «1–2», «1/1,2»: the amount is the first value of these
	end       int         // byte offset just after the quantity in the scanned text
}

// hasQuantity reports whether q says something about the amount.
func (q qty) hasQuantity() bool { return q.number || q.unit != "" || q.toTaste }

// quantity converts q to the domain type: a bare count becomes pieces, an
// out-of-vocabulary measure keeps only its number, and «по желанию» has
// none.
func (q qty) quantity() *domain.Quantity {
	switch {
	case q.toTaste:
		return &domain.Quantity{Unit: domain.UnitToTaste}
	case q.unit != "" && q.h > 0:
		return &domain.Quantity{Hundredths: q.h, Unit: q.unit}
	case q.oov != "" && q.h > 0:
		return &domain.Quantity{Hundredths: q.h}
	case q.oov == "" && q.h > 0:
		return &domain.Quantity{Hundredths: q.h, Unit: domain.UnitPiece}
	}
	return nil
}

const (
	// numPat reads «1 и 1/2» (one and a half), «1 1/2», «1/2», «1/1,2»,
	// «0,5», «1½» and «½».
	numPat = `(?:\d{1,6}\s+и\s+\d{1,3}/\d{1,3}|\d{1,6}\s+\d{1,3}/\d{1,3}|\d{1,6}/\d{1,3}(?:[.,]\d{1,2})?|\d{1,6}(?:[.,]\d{1,3})?\s?[½⅓⅔¼¾⅕⅛]?|[½⅓⅔¼¾⅕⅛])`
	// wordPat lists the number words a caption may use instead of digits.
	wordPat = `(?:одна|один|одно|одну|две|два|три|четыре|пять|шесть|семь|восемь|девять|десять|полтора|полторы|половина|половинка|пол)`
)

var (
	amountRe = regexp.MustCompile(`^(?:(~|≈|примерно|около|приблизительно|прибл\.)\s*)?(?:(по)\s+)?(` + numPat + `)(?:\s*[-–—]\s*(` + numPat + `))?`)
	wordRe   = regexp.MustCompile(`^(?:(~|≈|примерно|около|приблизительно)\s*)?(?:(по)\s+)?(` + wordPat + `)`)
	sizeRe   = regexp.MustCompile(`^\s*(?:крупн|средн|небольш|больш|маленьк|мелк)[а-я]*`)
	toTaste  = regexp.MustCompile(`^по[\s-]+вкусу`)
	optional = regexp.MustCompile(`^(?:по\s+желанию|для\s+подачи|для\s+украшения|для\s+декора|для\s+посыпки|опционально|на\s+выбор)`)
)

var numberWords = map[string]int64{
	"одна": 100, "один": 100, "одно": 100, "одну": 100, "две": 200, "два": 200, "три": 300,
	"четыре": 400, "пять": 500, "шесть": 600, "семь": 700, "восемь": 800, "девять": 900,
	"десять": 1000, "полтора": 150, "полторы": 150, "половина": 50, "половинка": 50, "пол": 50,
}

type unitPattern struct {
	re *regexp.Regexp
	u  domain.Unit
	// word is true for spelled-out units that make sense without a number
	// («щепотка соли» is one pinch); abbreviations need one.
	word bool
}

func unitPat(pattern string, u domain.Unit, word bool) unitPattern {
	return unitPattern{re: regexp.MustCompile(`^(?:` + pattern + `)`), u: u, word: word}
}

// unitPatterns match unit spellings in lower-cased text with ё folded.
// The longest match that ends at a word boundary wins.
var unitPatterns = []unitPattern{
	unitPat(`столов(?:ая|ой|ую|ые|ых)\s+ложк(?:ами|ой|ек|а|и|у)`, domain.UnitTablespoon, true),
	unitPat(`чайн(?:ая|ой|ую|ые|ых)\s+ложк(?:ами|ой|ек|а|и|у)`, domain.UnitTeaspoon, true),
	unitPat(`ст\s*[./]?\s*ложк(?:ек|а|и|у)`, domain.UnitTablespoon, false),
	unitPat(`ч\s*[./]?\s*ложк(?:ек|а|и|у)`, domain.UnitTeaspoon, false),
	unitPat(`ст\s*[./]?\s*л\.?`, domain.UnitTablespoon, false),
	unitPat(`ч\s*[./]?\s*л\.?`, domain.UnitTeaspoon, false),
	unitPat(`стакан(?:ами|ах|ов|а|у|ы)?`, domain.UnitCup, true),
	unitPat(`стак\.?`, domain.UnitCup, false),
	unitPat(`щепот(?:ками|ки|ку|ка|ок|ь)`, domain.UnitPinch, true),
	unitPat(`зубч(?:иками|иков|ика|ики|ик)`, domain.UnitClove, true),
	unitPat(`зуб\.?`, domain.UnitClove, false),
	unitPat(`пуч(?:ками|ков|ка|ки|ок)`, domain.UnitBunch, true),
	unitPat(`упаков(?:ками|ки|ку|ка|ок)`, domain.UnitPack, true),
	unitPat(`пач(?:ками|ки|ку|ка|ек)`, domain.UnitPack, true),
	unitPat(`пакет(?:ик)?(?:ов|а|и|ы)?`, domain.UnitPack, true),
	unitPat(`уп\.?`, domain.UnitPack, false),
	unitPat(`килограмм(?:ов|а|ы)?`, domain.UnitKilogram, false),
	unitPat(`кг\.?`, domain.UnitKilogram, false),
	unitPat(`грамм(?:ов|а|ы)?`, domain.UnitGram, false),
	unitPat(`гр\.?`, domain.UnitGram, false),
	unitPat(`г\.?`, domain.UnitGram, false),
	unitPat(`миллилитр(?:ов|а|ы)?`, domain.UnitMilliliter, false),
	unitPat(`мл\.?`, domain.UnitMilliliter, false),
	unitPat(`литр(?:ов|а|ы)?`, domain.UnitLiter, false),
	unitPat(`л\.?`, domain.UnitLiter, false),
	unitPat(`штук(?:и|а|у)?`, domain.UnitPiece, false),
	unitPat(`шт\.?`, domain.UnitPiece, false),
}

// oovRe matches measure words outside the unit list. Their number is kept,
// the word itself is not (a head of garlic is not a piece of garlic).
var oovRe = regexp.MustCompile(`^(?:кочанчик(?:ов|а|и)?|кочан(?:ами|ов|а|у|ы)?|голов(?:ками|ки|ку|ка|ок)|луковиц(?:ы|а|у)?|горст(?:ями|ей|ь|и)|горсточк[аиу]|веточ(?:ки|ку|ка|ек)|вет(?:ки|ку|ка|ок)|стебл(?:ей|я|и)|стебель|листик(?:ов|а|и)?|листа|листов|листья|листьев|ломтик(?:ов|а|и)?|ломт(?:ей|я|ь|и)|дол(?:ьки|ьку|ька|ек)|банк(?:и|у|а)|банок|бутыл(?:ки|ку|ка|ок)|кусоч(?:ков|ка|ек|ки)|кус(?:ков|ка|ок|ки)|кап(?:ли|лю|ля|ель)|стручк(?:ов|а|и)|стручок|кружоч(?:ков|ка|ек|ки))`)

// bareStRe matches «ст.» or «ст» left over when no unit matched: an
// abbreviation shared by «стакан» and «столовая ложка».
var bareStRe = regexp.MustCompile(`^ст\.?`)

// atBoundary reports whether s[i:] starts a new word (no letter or digit
// right at i).
func atBoundary(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// matchUnit finds the longest unit spelling at the start of s. Without a
// number only spelled-out units count.
func matchUnit(s string, needWord bool) (domain.Unit, int) {
	var (
		best domain.Unit
		end  int
	)
	for _, p := range unitPatterns {
		if needWord && !p.word {
			continue
		}
		m := p.re.FindStringIndex(s)
		if m == nil || m[1] <= end {
			continue
		}
		// «г.» may end a sentence; the boundary is checked after the word.
		e := m[1]
		word := strings.TrimRight(s[:e], ".")
		if !atBoundary(s, len(word)) {
			continue
		}
		best, end = p.u, e
	}
	return best, end
}

// matchOOV finds an out-of-vocabulary measure word at the start of s.
func matchOOV(s string) (string, int) {
	m := oovRe.FindStringIndex(s)
	if m == nil || !atBoundary(s, m[1]) {
		return "", 0
	}
	return s[:m[1]], m[1]
}

// scanQty reads a quantity at the very start of s, which must be lower
// case with ё folded (see lowerKeep): an amount («200», «1/2», «1½»,
// «0,5», «1 1/2», «две», «полстакана», «~800», «по 15-20», a range
// «3-4» or alternatives «1/1,2») with an optional unit or measure word, a
// unit word alone («щепотка», «стакан»), «по вкусу», or a marker of an
// optional ingredient («по желанию», «для подачи»).
func scanQty(s string) (qty, bool) {
	if m := toTaste.FindStringIndex(s); m != nil && atBoundary(s, m[1]) {
		end := m[1]
		if end < len(s) && s[end] == '.' {
			end++
		}
		return qty{toTaste: true, end: end}, true
	}
	if m := optional.FindStringIndex(s); m != nil && atBoundary(s, m[1]) {
		return qty{optional: true, end: m[1]}, true
	}
	var q qty
	pos := 0
	if m := amountRe.FindStringSubmatchIndex(s); m != nil {
		first := s[m[6]:m[7]]
		h, alt, ok := parseNumber(first)
		if !ok {
			return qty{}, false
		}
		q.h, q.number = h, true
		q.approx = m[2] >= 0
		q.shared = m[4] >= 0
		if m[8] >= 0 {
			q.rangeText = strings.TrimSpace(s[m[6]:m[9]])
		} else if alt {
			q.rangeText = first
		}
		pos = m[1]
		// «10%» is part of a name («сливки 10%»), not an amount.
		if rest := strings.TrimLeft(s[pos:], " "); strings.HasPrefix(rest, "%") {
			return qty{}, false
		}
		if pos < len(s) && unicode.IsDigit(rune(s[pos])) {
			return qty{}, false
		}
	} else if m := wordRe.FindStringSubmatchIndex(s); m != nil {
		word := s[m[6]:m[7]]
		end := m[1]
		if word == "пол" {
			// «пол», «пол-лимона», «полстакана»: half of what follows.
			rest := strings.TrimLeft(s[end:], " -")
			if u, n := matchUnit(rest, true); u != "" {
				q = qty{h: 50, number: true, unit: u, end: len(s) - len(rest) + n}
				q.shared = m[4] >= 0
				return q, true
			}
			if end < len(s) && !atBoundary(s, end) && s[end] != '-' {
				return qty{}, false
			}
		} else if !atBoundary(s, end) {
			return qty{}, false
		}
		q.h, q.number = numberWords[word], true
		q.approx = m[2] >= 0
		q.shared = m[4] >= 0
		pos = end
	}
	// The unit, possibly after a size word: «3 крупные головки».
	rest := s[pos:]
	afterSpace := strings.TrimLeft(rest, " ")
	skipped := len(rest) - len(afterSpace)
	if q.number {
		if m := sizeRe.FindStringIndex(rest); m != nil {
			tail := strings.TrimLeft(rest[m[1]:], " ")
			if u, _ := matchUnit(tail, false); u != "" {
				afterSpace, skipped = tail, len(rest)-len(tail)
			} else if w, _ := matchOOV(tail); w != "" {
				afterSpace, skipped = tail, len(rest)-len(tail)
			}
		}
	}
	if u, n := matchUnit(afterSpace, !q.number); u != "" {
		q.unit = u
		q.end = pos + skipped + n
		if !q.number {
			q.h = 100 // «щепотка соли» is one pinch
		}
		return q, true
	}
	if w, n := matchOOV(afterSpace); w != "" {
		q.oov = w
		q.end = pos + skipped + n
		return q, true
	}
	// «2 ст. молока»: «ст.» without «л» is a cup or a tablespoon; the
	// number is kept, the unit is not guessed.
	if m := bareStRe.FindStringIndex(afterSpace); q.number && m != nil && atBoundary(afterSpace, len("ст")) {
		q.oov = "ст"
		q.end = pos + skipped + m[1]
		return q, true
	}
	if !q.number {
		return qty{}, false
	}
	q.end = pos
	return q, true
}

// parseNumber reads one amount into hundredths. alt is true for «1/1,2»,
// two alternative amounts of which the first is taken.
func parseNumber(s string) (h int64, alt, ok bool) {
	s = strings.TrimSpace(s)
	// «1 и 1/2» is the mixed number «1 1/2».
	if f := strings.Fields(s); len(f) == 3 && f[1] == "и" {
		s = f[0] + " " + f[2]
	}
	var frac int64
	// A trailing vulgar fraction: «1½», «1 ½», «½».
	if r, size := utf8.DecodeLastRuneInString(s); size > 0 {
		if f, ok := vulgar[r]; ok {
			frac = f
			s = strings.TrimSpace(s[:len(s)-size])
			if s == "" {
				return frac, false, true
			}
		}
	}
	if whole, fraction, mixed := strings.Cut(s, " "); mixed && strings.Contains(fraction, "/") {
		w, err := strconv.ParseInt(whole, 10, 64)
		f, ok := fractionHundredths(fraction)
		if err != nil || !ok || f >= 100 {
			return 0, false, false
		}
		return w*100 + f, false, true
	}
	if num, den, isFrac := strings.Cut(s, "/"); isFrac {
		if strings.ContainsAny(den, ".,") {
			// «1/1,2 кг» are two alternatives, not a fraction.
			v, ok := decimalHundredths(num)
			return v, true, ok
		}
		f, ok := fractionHundredths(s)
		if !ok {
			return 0, false, false
		}
		if n, _ := strconv.ParseInt(num, 10, 64); n > 0 {
			if d, _ := strconv.ParseInt(den, 10, 64); n >= d {
				// «3/2» or «2/1» rarely means a fraction in a caption:
				// treat it as alternatives as well.
				v, ok := decimalHundredths(num)
				return v, true, ok
			}
		}
		return f, false, true
	}
	v, ok := decimalHundredths(s)
	return v + frac, false, ok
}

var vulgar = map[rune]int64{'½': 50, '⅓': 33, '⅔': 67, '¼': 25, '¾': 75, '⅕': 20, '⅛': 13}

// fractionHundredths reads «n/d» into hundredths, rounding half up.
func fractionHundredths(s string) (int64, bool) {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		return 0, false
	}
	n, err1 := strconv.ParseInt(num, 10, 64)
	d, err2 := strconv.ParseInt(den, 10, 64)
	if err1 != nil || err2 != nil || d <= 0 || n <= 0 {
		return 0, false
	}
	return (n*200 + d) / (2 * d), true
}

// decimalHundredths reads «250», «1,5» or «0.25», rounding to hundredths.
func decimalHundredths(s string) (int64, bool) {
	whole, frac, _ := strings.Cut(strings.ReplaceAll(s, ",", "."), ".")
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, false
	}
	h := w * 100
	if frac != "" {
		for len(frac) < 3 {
			frac += "0"
		}
		f, err := strconv.ParseInt(frac[:3], 10, 64)
		if err != nil {
			return 0, false
		}
		h += (f + 5) / 10
	}
	return h, true
}
