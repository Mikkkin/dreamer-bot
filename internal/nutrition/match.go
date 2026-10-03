package nutrition

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Matching an ingredient name to a food
//
// A name and every alias of the table go through the same pipeline: lower
// case, ё→е, invisible characters dropped, percentages pulled out («молоко
// 3,2%»), parentheticals removed, then words split, amounts and measure
// words dropped («2 ст. л.», «граммов») and each remaining word reduced to a
// stem by a light suffix stripper («сахарного песка» → «сахарн песк»).
//
// Match first looks for an alias whose stem sequence equals the whole name.
// Otherwise it splits the name into alternatives («X или Y», «X/Y»; only the
// part before the first comma counts) and, within one alternative, looks for
// the aliases whose stems are all present in the name, keeping the longest
// ones. Words after a preposition («для подачи», «из курицы», «с сахаром»)
// may complete an alias but never match on their own. Matching is whole-stem
// only: «соль» and «солод» never meet. A tie between different foods goes
// to the aliases that cover more of the words before the preposition
// («масло оливковое для жарки» is olive oil); an even tie is no match at
// all. The «X% fat» variants of one product (молоко 1,5/2,5/3,2%)
// form a family: a percentage in the name picks the closest variant, and a
// bare name picks the variant that owns the bare alias (молоко → 2,5%).
//
// A few guards keep a match from naming the wrong product: «сок апельсина»
// or «масло авокадо» (a product word in front of the food), «веганский
// пармезан» or «рисовое молоко» (a substitute), «вяленые томаты» (a dried
// fresh food) and «масло виноградное» (a product made from another food) are
// left unmatched.

// query is an analysed ingredient name.
type query struct {
	key     string   // all stems and prepositions in order, for the exact lookup
	alts    [][]part // alternatives, each a conjunction of parts
	percent float64
	hasPct  bool
}

// part is one food mention: the stems before the first preposition
// (primary) and the stems after it (secondary).
type part struct {
	primary   []string
	secondary []string
}

func (p part) all() []string { return append(slices.Clip(p.primary), p.secondary...) }

// token kinds produced by tokenize.
const (
	tokWord = iota
	tokComma
	tokAlt
	tokConj
)

type token struct {
	kind int
	text string
}

// cleanRunes lower-cases s, folds ё to е, mends «и» + U+200C (and the
// combining breve) into «й» and turns non-breaking, braille-blank and other
// spaces into plain spaces. Zero-width characters disappear.
func cleanRunes(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		r = unicode.ToLower(r)
		switch {
		case r == 'ё':
			r = 'е'
		case r == '\u200c' || r == '\u0306':
			if n := len(out); n > 0 && out[n-1] == 'и' {
				out[n-1] = 'й'
			}
			continue
		case r == '\u200b' || r == '\u200d' || r == '\u2060' || r == '\ufeff' || unicode.Is(unicode.Mn, r):
			continue
		case r == '\u00a0' || r == '\u2800' || unicode.IsSpace(r):
			r = ' '
		}
		out = append(out, r)
	}
	return out
}

// extractPercent removes every «3,2%» / «82.5 %» from rs and returns the
// first value found.
func extractPercent(rs []rune) ([]rune, float64, bool) {
	var (
		out   = make([]rune, 0, len(rs))
		value float64
		found bool
	)
	for i := 0; i < len(rs); i++ {
		if !isDigit(rs[i]) || (i > 0 && (isDigit(rs[i-1]) || rs[i-1] == '.' || rs[i-1] == ',')) {
			out = append(out, rs[i])
			continue
		}
		j := i
		for j < len(rs) && isDigit(rs[j]) {
			j++
		}
		if j+1 < len(rs) && (rs[j] == ',' || rs[j] == '.') && isDigit(rs[j+1]) {
			j++
			for j < len(rs) && isDigit(rs[j]) {
				j++
			}
		}
		k := j
		for k < len(rs) && rs[k] == ' ' {
			k++
		}
		if k < len(rs) && rs[k] == '%' {
			if v, err := strconv.ParseFloat(strings.ReplaceAll(string(rs[i:j]), ",", "."), 64); err == nil && !found {
				value, found = v, true
			}
			out = append(out, ' ')
			i = k
			continue
		}
		out = append(out, rs[i:j]...)
		i = j - 1
	}
	return out, value, found
}

// dropParentheticals removes «(…)», «[…]» and «{…}», nested or unclosed.
func dropParentheticals(rs []rune) []rune {
	out := make([]rune, 0, len(rs))
	depth := 0
	for _, r := range rs {
		switch r {
		case '(', '[', '{':
			depth++
			continue
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
			out = append(out, ' ')
			continue
		}
		if depth == 0 {
			out = append(out, r)
		}
	}
	return out
}

func isDigit(r rune) bool  { return r >= '0' && r <= '9' }
func isLetter(r rune) bool { return unicode.IsLetter(r) }

// letterRun counts the letters of rs from i in direction step.
func letterRun(rs []rune, i, step int) int {
	n := 0
	for ; i >= 0 && i < len(rs) && isLetter(rs[i]); i += step {
		n++
	}
	return n
}

// tokenize splits cleaned text into words and separators. Commas and
// semicolons end the useful part of a name; «/» and «:» separate
// alternatives unless they join short abbreviations («в/с», «ст/л»), which
// become one word; «+» and «&» join foods. A digit followed by a letter
// starts a new word («200г»).
func tokenize(rs []rune) []token {
	var (
		toks []token
		word []rune
	)
	flush := func() {
		if len(word) > 0 {
			toks = append(toks, token{kind: tokWord, text: string(word)})
			word = word[:0]
		}
	}
	for i, r := range rs {
		switch {
		case isLetter(r) || isDigit(r):
			if len(word) > 0 && isLetter(r) && isDigit(word[len(word)-1]) {
				flush()
			}
			word = append(word, r)
		case r == '/' && letterRun(rs, i-1, -1) >= 1 && letterRun(rs, i-1, -1) <= 2 &&
			letterRun(rs, i+1, 1) >= 1 && letterRun(rs, i+1, 1) <= 2:
			// «в/с» stays one word: keep collecting.
		case r == ',' || r == ';':
			flush()
			toks = append(toks, token{kind: tokComma})
		case r == '/' || r == ':' || r == '|':
			flush()
			toks = append(toks, token{kind: tokAlt})
		case r == '+' || r == '&':
			flush()
			toks = append(toks, token{kind: tokConj})
		default:
			flush()
		}
	}
	flush()
	for i, t := range toks {
		if t.kind != tokWord {
			continue
		}
		switch t.text {
		case "или", "либо":
			toks[i] = token{kind: tokAlt}
		case "и":
			toks[i] = token{kind: tokConj}
		}
	}
	return toks
}

// prepositions end the primary part of a food mention.
var prepositions = map[string]bool{
	"для": true, "из": true, "от": true, "с": true, "со": true, "в": true, "во": true,
	"на": true, "под": true, "без": true, "по": true, "после": true, "при": true,
	"к": true, "ко": true, "вместо": true,
}

// measureStems are the stems of units, measures and counted containers;
// they never name a food.
var measureStems = setOf(
	"г", "гр", "грам", "грамм", "кг", "кил", "килограмм", "мг", "мл", "миллилитр",
	"л", "литр", "шт", "штук", "штучк", "ст", "стл", "ч", "чл", "ложк", "ложечк",
	"стакан", "стаканчик", "щепотк", "щепот", "зубчик", "зуб", "зубч", "пучк",
	"упаковк", "уп", "упак", "пачк", "банк", "баночк", "пакетик", "горст", "кочан",
	"головк", "куск", "кусочк", "ломтик", "капл", "капел", "веточк", "см", "мм",
	"кубик", "дольк", "долк", "порц",
)

// spoonAdjectives are dropped when a spoon word follows («столовая ложка»),
// so that «уксус столовый» keeps its adjective.
var spoonAdjectives = setOf("столов", "чайн", "десертн")

// numberStems are amounts written as words.
var numberStems = setOf(
	"один", "одн", "два", "две", "двух", "три", "трех", "четыр", "пят", "шест",
	"десят", "пол", "половин", "половинк", "полтор", "нескольк", "немн", "чут", "пар",
)

// stemOverrides fix word forms whose stems would collide with another
// food's: «зелень» (greens) vs «зелёный», «сырой» (raw) vs «сыр», «печенье»
// vs «печень», «варенье» vs «варёный», «мороженое» vs «мороженый».
var stemOverrides = map[string]string{
	"зелень": "зелень", "зелени": "зелень", "зеленью": "зелень",
	"сырой": "сырой", "сырая": "сырой", "сырое": "сырой", "сырые": "сырой",
	"сырого": "сырой", "сырых": "сырой", "сырую": "сырой", "сырыми": "сырой",
	"печенье": "печенье", "печенья": "печенье", "печеньем": "печенье", "печеньки": "печенье",
	"варенье": "варенье", "варенья": "варенье", "вареньем": "варенье",
	"мороженое": "мороженое", "мороженого": "мороженое", "мороженым": "мороженое",
	"яиц": "яйц",
	"лед": "лед", "льда": "лед", "льдом": "лед", "льду": "лед",
	// Indeclinable or adverbial forms that look like a declined word:
	// «салями» is not «сало», «перец горошком» is not «горошек».
	"салями": "салями", "горошком": "горошком",
}

// stemSynonyms merge stems that mean the same thing for matching.
var stemSynonyms = map[string]string{
	"сух":     "сушен", // сухой базилик = сушёный базилик
	"вялен":   "сушен",
	"помидор": "томат",
	"огурчик": "огурц",
	"лучк":    "лук",
}

var suffixes = []string{
	"иями", "ями", "ами", "иях", "иям", "ием", "ией",
	"ого", "его", "ому", "ему", "ыми", "ими",
	"ях", "ах", "ов", "ев", "ей", "ой", "ий", "ый", "ая", "яя", "ое", "ее", "ые", "ие",
	"ую", "юю", "ых", "их", "ым", "им", "ом", "ем", "ам", "ям", "ою", "ею",
	"ью", "ья", "ье", "ьи", "ия", "ию", "ии",
	"а", "я", "о", "е", "ы", "и", "у", "ю", "ь", "й",
}

func isVowel(r rune) bool { return strings.ContainsRune("аеиоуыэюя", r) }

// latinLookalikes map Latin letters that look Cyrillic, for words that mix
// both scripts («cметана» typed with a Latin «c»).
var latinLookalikes = map[rune]rune{
	'a': 'а', 'e': 'е', 'o': 'о', 'p': 'р', 'c': 'с', 'x': 'х', 'y': 'у', 'k': 'к', 'm': 'м', 't': 'т', 'h': 'н', 'b': 'в',
}

func fixScripts(w string) string {
	var cyr, lat bool
	for _, r := range w {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyr = true
		case r < unicode.MaxASCII && isLetter(r):
			lat = true
		}
	}
	if !cyr || !lat {
		return w
	}
	return strings.Map(func(r rune) rune {
		if c, ok := latinLookalikes[r]; ok {
			return c
		}
		return r
	}, w)
}

// stem reduces a lower-case word to a matching stem: the longest common
// noun or adjective ending inside the region after the first vowel is
// stripped, the participle spelling «-енн» is folded to «-ен» (плавленный =
// плавленый) and a fleeting vowel in «-ок/-ек/-ец» is dropped, so that
// «песок»/«песка», «огурец»/«огурцы» and «чеснок»/«чеснока» agree.
func stem(w string) string {
	if s, ok := stemOverrides[w]; ok {
		return s
	}
	rs := []rune(w)
	rv := len(rs)
	for i, r := range rs {
		if isVowel(r) {
			rv = i + 1
			break
		}
	}
	for _, suf := range suffixes {
		sr := []rune(suf)
		if len(rs)-len(sr) >= rv && strings.HasSuffix(w, suf) {
			rs = rs[:len(rs)-len(sr)]
			break
		}
	}
	if n := len(rs); n > rv && rs[n-1] == 'ь' {
		rs = rs[:n-1] // «хлопья» and «хлопьях», «листья» and «листьев»
	}
	if n := len(rs); n >= 4 && string(rs[n-3:]) == "енн" {
		rs = rs[:n-1]
	}
	if n := len(rs); n >= 4 {
		tail := string(rs[n-2:])
		if (tail == "ок" || tail == "ек" || tail == "ец") && slices.ContainsFunc(rs[:n-2], isVowel) {
			rs = append(rs[:n-2:n-2], rs[n-1])
		}
	}
	s := string(rs)
	if syn, ok := stemSynonyms[s]; ok {
		return syn
	}
	return s
}

// analyze runs the matching pipeline over a name or an alias.
func analyze(name string) query {
	rs := cleanRunes(name)
	rs, pct, hasPct := extractPercent(rs)
	rs = dropParentheticals(rs)
	toks := tokenize(rs)

	var q query
	q.percent, q.hasPct = pct, hasPct

	// Classify words: stems, prepositions, or nothing (amounts, measures).
	type word struct {
		kind int // tokWord with stem, or a separator kind
		stem string
		prep bool
	}
	words := make([]word, 0, len(toks))
	for i, t := range toks {
		if t.kind != tokWord {
			words = append(words, word{kind: t.kind})
			continue
		}
		w := fixScripts(t.text)
		if prepositions[w] {
			words = append(words, word{kind: tokWord, prep: true, stem: w})
			continue
		}
		if strings.IndexFunc(w, isLetter) < 0 {
			continue // an amount
		}
		if len([]rune(w)) == 1 {
			continue // stray letters: «т. е.», «ч», «л»
		}
		s := stem(w)
		if measureStems[s] || numberStems[s] {
			continue
		}
		if spoonAdjectives[s] && i+1 < len(toks) && toks[i+1].kind == tokWord && stem(toks[i+1].text) == "ложк" {
			continue
		}
		if s == "обезжирен" && !q.hasPct {
			q.percent, q.hasPct = 0, true
		}
		words = append(words, word{kind: tokWord, stem: s})
	}

	keyParts := make([]string, 0, len(words))
	for _, w := range words {
		if w.kind == tokWord {
			keyParts = append(keyParts, w.stem)
		} else if w.kind == tokConj {
			keyParts = append(keyParts, "и")
		}
	}
	q.key = strings.Join(keyParts, " ")

	// Only the text before the first comma names the food; the rest is
	// commentary («лук, нарезанный кольцами»).
	var (
		alt     []part
		cur     part
		inSec   bool
		started bool
	)
	endPart := func() {
		if len(cur.primary) == 0 && len(cur.secondary) > 0 {
			// «для соуса сметана»: nothing before the preposition.
			cur.primary, cur.secondary = cur.secondary, nil
		}
		if len(cur.primary) > 0 {
			alt = append(alt, cur)
		}
		cur, inSec = part{}, false
	}
	endAlt := func() {
		endPart()
		if len(alt) > 0 {
			q.alts = append(q.alts, alt)
		}
		alt = nil
	}
loop:
	for _, w := range words {
		switch {
		case w.kind == tokComma:
			if started {
				break loop
			}
		case w.kind == tokAlt:
			endAlt()
		case w.kind == tokConj:
			endPart()
		case w.prep:
			inSec = true
		case inSec:
			cur.secondary = append(cur.secondary, w.stem)
			started = true
		default:
			cur.primary = append(cur.primary, w.stem)
			started = true
		}
	}
	endAlt()
	return q
}

// aliasStems returns the distinct content stems of an analysed alias,
// prepositions excluded.
func aliasStems(q query) []string {
	var out []string
	for _, alt := range q.alts {
		for _, p := range alt {
			for _, s := range p.all() {
				if !slices.Contains(out, s) {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// Guards (see the top of the file).
var (
	// productWords turn a food into another product when they come before
	// it: «сок апельсина», «масло авокадо», «ботва редиски».
	productWords = setOf(
		"масл", "сок", "соус", "паст", "порошк", "мук", "крахмал", "ботв", "сироп",
		"уксус", "пюр", "хлоп", "экстракт", "джем", "варенье", "бульон", "отвар",
		"тест", "приправ", "чипс", "цедр", "кислот", "эссенц", "ликер", "настойк",
		"сухар", "стружк", "крошк", "концентрат", "смес", "кожур",
	)
	// otherSource marks a stand-in made from something else: plant-based
	// milks and creams, vegan cheese, flours and oils of other plants,
	// «морская капуста».
	otherSource = setOf(
		"веганск", "растительн", "соев", "кокосов", "миндальн", "овсян", "рисов",
		"гречнев", "кукурузн", "безглютенов", "льнян", "кунжутн", "тыквен", "нутов",
		"арахисов", "конопл", "постн", "горчичн", "рапсов", "рыжиков", "пальмов",
		"трюфельн", "морск",
	)
	// productHeads are processed products; an adjective made from another
	// food («масло виноградное», «хлеб кукурузный») makes them a different
	// product.
	productHeads = setOf(
		"масл", "мук", "молк", "сливк", "паст", "сок", "соус", "сироп", "уксус",
		"крахмал", "хлеб", "хлоп", "сахар", "пюр",
	)
	// freshCategories are foods whose dried form is a different food.
	freshCategories = setOf(
		"vegetables", "fruits", "berries", "mushrooms", "greens", "meat", "poultry",
		"offal", "fish", "seafood", "dairy", "eggs",
	)
	adjectiveSuffixes = []string{"ов", "ев", "ьн", "ян", "н"}
)

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

// Match finds the food an ingredient name refers to. ok is false when the
// name is unknown or ambiguous («перец»).
func (t *Table) Match(name string) (Food, bool) {
	if i, ok := t.match(name); ok {
		return t.foods[i], true
	}
	return Food{}, false
}

func (t *Table) match(name string) (int, bool) {
	q := analyze(name)
	if q.key == "" {
		return 0, false
	}
	if ids := t.exact[q.key]; len(ids) > 0 {
		if f, ok := t.resolve(ids, q); ok {
			return f, true
		}
	}
	for _, alt := range q.alts {
		if f, ok := t.matchAlt(alt, q); ok {
			return f, true
		}
	}
	return 0, false
}

// matchAlt matches one alternative: its parts must agree on one food
// («зелень лука и укропа» may name only greens; «соль и перец» with two
// different foods is ambiguous).
func (t *Table) matchAlt(alt []part, q query) (int, bool) {
	found, any := 0, false
	for _, p := range alt {
		f, ok := t.matchPart(p, q)
		if !ok {
			continue
		}
		if any && f != found {
			return 0, false
		}
		found, any = f, true
	}
	return found, any
}

func (t *Table) matchPart(p part, q query) (int, bool) {
	all := p.all()
	have := make(map[string]bool, len(all))
	for _, s := range all {
		have[s] = true
	}
	primary := make(map[string]bool, len(p.primary))
	for _, s := range p.primary {
		primary[s] = true
	}

	var best []int
	bestLen := 0
	seen := make(map[int]bool)
	for _, s := range p.primary {
		for _, ai := range t.byStem[s] {
			if seen[ai] {
				continue
			}
			seen[ai] = true
			a := t.aliases[ai]
			if !allIn(a.stems, have) {
				continue
			}
			switch n := len(a.stems); {
			case n > bestLen:
				best, bestLen = []int{ai}, n
			case n == bestLen:
				best = append(best, ai)
			}
		}
	}
	if len(best) == 0 {
		return 0, false
	}
	f, ok := t.resolve(best, q)
	if !ok {
		// A tie between foods: the aliases that cover more of the main
		// words win («масло оливковое для жарки» is olive oil, not the
		// «масло для жарки» of sunflower oil, whose «жарки» is only a
		// secondary word here).
		if best = t.mostPrimary(best, primary); len(best) > 0 {
			f, ok = t.resolve(best, q)
		}
		if !ok {
			return 0, false
		}
	}
	covered := make(map[string]bool)
	for _, ai := range best {
		for _, s := range t.aliases[ai].stems {
			covered[s] = true
		}
	}
	if t.rejected(f, p.primary, covered) {
		return 0, false
	}
	return f, true
}

// mostPrimary keeps the aliases that cover the most main (pre-preposition)
// words of the name; nil when that does not narrow the tie.
func (t *Table) mostPrimary(aliasIDs []int, primary map[string]bool) []int {
	var (
		kept []int
		most = -1
	)
	for _, ai := range aliasIDs {
		n := 0
		for _, s := range t.aliases[ai].stems {
			if primary[s] {
				n++
			}
		}
		switch {
		case n > most:
			kept, most = []int{ai}, n
		case n == most:
			kept = append(kept, ai)
		}
	}
	if len(kept) == len(aliasIDs) {
		return nil
	}
	return kept
}

func allIn(stems []string, have map[string]bool) bool {
	for _, s := range stems {
		if !have[s] {
			return false
		}
	}
	return true
}

// rejected applies the guards to a candidate food f whose aliases covered
// the stems in covered.
func (t *Table) rejected(f int, primary []string, covered map[string]bool) bool {
	firstCovered := slices.IndexFunc(primary, func(s string) bool { return covered[s] })
	head := false
	for s := range covered {
		if productHeads[s] {
			head = true
			break
		}
	}
	for i, s := range primary {
		if covered[s] {
			continue
		}
		switch {
		case productWords[s] && i < firstCovered:
			return true
		case otherSource[s]:
			return true
		case s == "сушен" && freshCategories[t.foods[f].Category]:
			return true
		case head && t.madeFromOther(s, f):
			return true
		}
	}
	return false
}

// madeFromOther reports whether s is an adjective made from the one-word
// name of a food other than f («виноградн» from «виноград»).
func (t *Table) madeFromOther(s string, f int) bool {
	for _, suf := range adjectiveSuffixes {
		base, ok := strings.CutSuffix(s, suf)
		if !ok || len([]rune(base)) < 3 {
			continue
		}
		for _, g := range t.single[base] {
			if g != f {
				return true
			}
		}
	}
	return false
}

// resolve turns matching aliases into one food. Aliases of different
// foods are ambiguous unless the foods are fat variants of one product;
// then a percentage picks the closest variant and a bare name picks the
// variant that owns a bare alias. A single food with a percentage in the
// name moves to the closest variant of its family.
func (t *Table) resolve(aliasIDs []int, q query) (int, bool) {
	var foods []int
	for _, ai := range aliasIDs {
		if f := t.aliases[ai].food; !slices.Contains(foods, f) {
			foods = append(foods, f)
		}
	}
	f := foods[0]
	if len(foods) > 1 {
		fam := t.foods[f].family
		for _, g := range foods[1:] {
			if t.foods[g].family != fam || fam < 0 {
				return 0, false
			}
		}
		if !q.hasPct {
			var bare []int
			for _, ai := range aliasIDs {
				a := t.aliases[ai]
				if a.bare && !slices.Contains(bare, a.food) {
					bare = append(bare, a.food)
				}
			}
			if len(bare) != 1 {
				return 0, false
			}
			return bare[0], true
		}
	}
	if q.hasPct {
		if fam := t.foods[f].family; fam >= 0 {
			return t.closest(t.families[fam], q.percent), true
		}
	}
	return f, true
}

// closest picks the family member whose fat percentage is nearest to p,
// the lower one on a tie.
func (t *Table) closest(members []int, p float64) int {
	best, bestD, bestP := members[0], math.Inf(1), math.Inf(1)
	for _, m := range members {
		for _, v := range t.foods[m].percents {
			d := math.Abs(v - p)
			if d < bestD-1e-9 || (math.Abs(d-bestD) <= 1e-9 && v < bestP) {
				best, bestD, bestP = m, d, v
			}
		}
	}
	return best
}
