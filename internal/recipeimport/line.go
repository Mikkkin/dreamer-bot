package recipeimport

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// item is one ingredient read from a caption line.
type item struct {
	name string
	q    qty
	// nameFirst is true when the quantity followed the name («Соль — 1 ч.
	// л.»); only such a quantity is shared backwards along a list.
	nameFirst bool
	// dashNoQty marks «Фрикадельки - готовые…»: a spaced dash whose
	// quantity slot holds no readable amount.
	dashNoQty bool
}

// maxNameWords bounds an ingredient name; longer text is prose.
const maxNameWords = 6

var (
	sentenceEnd = regexp.MustCompile(`[.!?…]\s+[А-ЯЁA-Z]`)
	commaSpaces = regexp.MustCompile(`\s*,\s*(\pL)`)
	// separators between a name and its quantity, tried left to right.
	separators = []rune{'—', '–', '-', ':', '='}
)

// tailStarts are words that open a preparation note or an aside after a
// comma («…, нарезанный на кусочки», «…, либо фарш»), never a food.
var tailStarts = map[string]bool{
	"для": true, "без": true, "из": true, "на": true, "в": true, "во": true, "до": true,
	"с": true, "со": true, "от": true, "либо": true, "или": true, "можно": true, "если": true,
	"чтобы": true, "но": true, "а": true, "лучше": true, "желательно": true, "слегка": true,
	"мелко": true, "крупно": true, "тонко": true, "предварительно": true, "заранее": true,
	"хорошо": true, "очень": true, "у": true, "т": true, "как": true, "так": true, "то": true,
}

// dropPrefixes start size words and qualifiers that are dropped from names
// (convention: «средняя морковь» is «Морковь», «любой фарш» is «Фарш»).
var dropPrefixes = []string{"крупн", "средн", "небольш", "больш", "маленьк", "любой", "любая", "любое", "любые", "любых", "любого", "любую"}

// dropPhrases are qualifiers that say nothing about the food.
var dropPhrases = []string{
	"для подачи", "для украшения", "для декора", "для посыпки", "по желанию", "по вкусу",
	"по-вкусу", "опционально", "из холодильника", "комнатной температуры", "с горкой",
	"без горки", "с верхом", "без верха", "на выбор",
}

// countNouns are the count forms that name a food in the nominative
// («одна луковица» is one onion).
var countNouns = map[string]string{
	"луковица": "лук", "луковицы": "лук", "картофелина": "картофель", "картофелины": "картофель",
	"картошка": "картофель", "картошки": "картофель", "морковка": "морковь", "морковки": "морковь",
}

// parseLine reads every ingredient of one caption line (bullets, emoji and
// numbering already removed). Items may have no quantity. Lists are split
// on «, » and «;»; «X и Y» splits when both parts carry an amount or share
// «по вкусу»; «соль/перец» splits when there is no number to share. A
// quantity at the end of a list applies to the foods before it that have
// none («Помидор, огурец - по 15 г»).
func parseLine(text string) []item {
	t, parens := blankParens(text)
	if loc := sentenceEnd.FindStringIndex(t); loc != nil {
		t = t[:loc[0]+1]
	}
	t = strings.TrimRight(t, " ,;.")
	parts := splitList(t)
	var items []item
	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx > 0 && isAlternative(part) {
			continue
		}
		if both := splitAnd(part); both != nil {
			items = append(items, both...)
			continue
		}
		it, ok := parsePart(part)
		if !ok {
			continue
		}
		if !it.q.hasQuantity() && !it.q.optional && idx > 0 && isTail(part) {
			continue
		}
		if it.dashNoQty && len(parts) <= 2 {
			if q, ok := parenQuantity(parens); ok {
				it.q = q
			}
		}
		items = append(items, it)
	}
	// A quantity after the last name of a list is shared backwards.
	for i := len(items) - 1; i > 0; i-- {
		if !items[i].q.hasQuantity() || !items[i].nameFirst {
			continue
		}
		for j := i - 1; j >= 0 && !items[j].q.hasQuantity() && !items[j].q.optional; j-- {
			items[j].q = items[i].q
		}
	}
	var out []item
	for _, it := range items {
		out = append(out, splitShared(it)...)
	}
	return out
}

// splitList splits at top-level «, » and «;». A comma without a space
// («содовая,тоник») joins alternatives and is kept.
func splitList(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == ';':
			parts = append(parts, s[start:i])
			start = i + 1
		case s[i] == ',' && i+1 < len(s) && s[i+1] == ' ':
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// splitAnd splits «20-30 гр крахмала и 3-4 ст л воды» into two
// ingredients when both halves carry their own amount.
func splitAnd(part string) []item {
	left, right, ok := strings.Cut(part, " и ")
	if !ok {
		return nil
	}
	a, okA := parsePart(strings.TrimSpace(left))
	b, okB := parsePart(strings.TrimSpace(right))
	if !okA || !okB || !a.q.number || !b.q.number {
		return nil
	}
	return []item{a, b}
}

// splitShared splits one item whose quantity is shared by several foods:
// «Соль и перец по вкусу», «Щепотка соли/перца», «Соль/перец/зелень - по
// вкусу», «Сахар и соль - по 1 ч.л.».
func splitShared(it item) []item {
	q := it.q
	shareable := q.toTaste || q.shared || (q.unit != "" && !q.number) || !q.hasQuantity()
	if !shareable {
		return []item{it}
	}
	var names []string
	switch {
	case strings.Contains(it.name, "/") && !digitSlash(it.name):
		names = strings.Split(it.name, "/")
	case strings.Contains(it.name, " и ") && q.hasQuantity():
		names = strings.Split(it.name, " и ")
	default:
		return []item{it}
	}
	for _, n := range names {
		if len(strings.Fields(n)) > 3 || strings.TrimSpace(n) == "" {
			return []item{it}
		}
	}
	out := make([]item, 0, len(names))
	for _, n := range names {
		c := it
		c.name = capitalize(strings.TrimSpace(n))
		out = append(out, c)
	}
	return out
}

// digitSlash reports a «/» between two digits («1/2»): a fraction, never a
// split between two foods.
func digitSlash(s string) bool {
	for i := 1; i+1 < len(s); i++ {
		if s[i] == '/' && s[i-1] >= '0' && s[i-1] <= '9' && s[i+1] >= '0' && s[i+1] <= '9' {
			return true
		}
	}
	return false
}

// isAlternative reports a list part that offers a replacement for the
// previous food («…, либо фарш с солью»), not a food of its own.
func isAlternative(part string) bool {
	words := letterWords(part)
	return len(words) > 0 && (words[0] == "либо" || words[0] == "или")
}

// isTail reports a list part that is a note about the previous food.
func isTail(part string) bool {
	words := letterWords(part)
	if len(words) == 0 {
		return true
	}
	first := words[0]
	return tailStarts[first] || isVerb(first) || isParticiple(first)
}

// parenQuantity finds an amount with a unit inside parentheses: «(у меня
// 800 гр.)».
func parenQuantity(parens []string) (qty, bool) {
	for _, p := range parens {
		low := lowerKeep(p)
		for i := 0; i < len(low); i++ {
			if i > 0 && low[i-1] != ' ' {
				continue
			}
			if q, ok := scanQty(low[i:]); ok && q.number && q.unit != "" {
				return q, true
			}
		}
	}
	return qty{}, false
}

// parsePart reads one ingredient: amount first («200 г муки», «Щепотка
// соли», «2 яйца»), name first with a separator («Соль — 1 ч. л.»,
// «Белки-6 шт», «яйцо :4 шт») or without one («Морковь 1 кг», «Соль по
// вкусу»), or a bare name.
func parsePart(part string) (item, bool) {
	low := lowerKeep(part)
	if q, ok := scanQty(low); ok && !q.optional && (q.number || q.unit != "" || q.oov != "" || q.toTaste) {
		// «Стебель сельдерея — 100 г»: a measure word that starts a name.
		if !q.number {
			if it, ok := nameFirstWithSeparator(part, low); ok && it.q.number {
				return it, true
			}
		}
		rest := trimPunct(part[q.end:])
		if rest == "" {
			if strings.HasPrefix(q.oov, "луковиц") {
				q.oov, q.unit = "", ""
				return item{name: "Лук", q: q}, true
			}
			return item{}, false
		}
		genitive := q.unit != "" || q.oov != "" || (q.number && q.h != 100)
		if q.toTaste {
			genitive = false
		}
		name := cleanName(rest, genitive)
		q = fixMeasure(q, name)
		if strings.HasPrefix(q.oov, "луковиц") && name == "" {
			name = "Лук"
		}
		return item{name: name, q: q}, name != ""
	}
	if it, ok := nameFirstWithSeparator(part, low); ok {
		return it, true
	}
	if it, ok := nameFirstNoSeparator(part, low); ok {
		return it, true
	}
	name := cleanName(part, false)
	return item{name: name}, name != ""
}

// fixMeasure turns a head of onion into pieces: «3 крупные головки» of
// «Лук репчатый» are three onions, while a head of garlic stays a measure
// outside the unit list.
func fixMeasure(q qty, name string) qty {
	if q.oov == "" || !(strings.HasPrefix(q.oov, "голов") || strings.HasPrefix(q.oov, "луковиц")) {
		return q
	}
	low := lowerKeep(name)
	if (strings.HasPrefix(low, "лук") || strings.Contains(low, " лук")) && !strings.Contains(low, "чеснок") {
		q.oov, q.unit = "", ""
		if q.h == 0 {
			q.h = 100
		}
		q.number = true
	}
	return q
}

// bareEnd reports whether a bare number (no unit) ends the useful text: a
// count like «Яйца — 3» or «Яйцо 2 (крупных)», not «180 градусов».
func bareEnd(rest string) bool {
	rest = strings.TrimLeft(rest, " ")
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return strings.ContainsRune(",.;:)(/", r)
}

// quantityAfter reports whether s (lower case) starts with a quantity that
// may follow a name.
func quantityAfter(s string) (qty, bool) {
	q, ok := scanQty(s)
	if !ok {
		return qty{}, false
	}
	if q.number && q.unit == "" && q.oov == "" && !bareEnd(s[q.end:]) {
		return qty{}, false
	}
	return q, true
}

func nameFirstWithSeparator(part, low string) (item, bool) {
	for i, r := range low {
		if !containsRune(separators, r) {
			continue
		}
		before := strings.TrimRight(low[:i], " ")
		if before == "" {
			continue
		}
		// «1–2», «300-320»: a range, not a separator.
		if last, _ := utf8.DecodeLastRuneInString(before); unicode.IsDigit(last) && r != ':' {
			continue
		}
		j := i + utf8.RuneLen(r)
		after := strings.TrimLeft(low[j:], " ")
		spaced := j < len(low) && low[j] == ' ' && i > 0 && low[i-1] == ' '
		q, ok := quantityAfter(after)
		if !ok {
			if q, ok := scanQty(after); ok && q.number {
				if cq, ok := countWithMeasure(after); ok && spaced && r != ':' && r != '=' {
					if name := cleanName(part[:i], false); name != "" {
						return item{name: name, q: fixMeasure(cq, name), nameFirst: true}, true
					}
				}
				continue // «Время — 30 минут»: a number that is not an amount
			}
			if spaced && r != ':' && r != '=' && len(strings.Fields(before)) <= maxNameWords {
				return item{name: cleanName(part[:i], false), dashNoQty: true}, true
			}
			continue
		}
		name := cleanName(part[:i], false)
		if name == "" {
			continue
		}
		q = fixMeasure(q, name)
		return item{name: name, q: q, nameFirst: true}, true
	}
	return item{}, false
}

func nameFirstNoSeparator(part, low string) (item, bool) {
	for i := 1; i < len(low); i++ {
		if low[i-1] != ' ' {
			continue
		}
		q, ok := quantityAfter(low[i:])
		if !ok {
			continue
		}
		raw := part[:i]
		// «ботва от 4 пучков редиса»: the food is «ботва редиса».
		if words := strings.Fields(lowerKeep(raw)); len(words) > 1 {
			if last := words[len(words)-1]; last == "от" || last == "из" {
				tail := strings.Fields(trimPunct(part[i+q.end:]))
				if len(tail) > 0 && len(tail) <= 2 {
					raw = strings.Join(strings.Fields(raw)[:len(words)-1], " ") + " " + strings.Join(tail, " ")
				}
			}
		}
		name := cleanName(raw, false)
		if name == "" {
			return item{}, false
		}
		q = fixMeasure(q, name)
		return item{name: name, q: q, nameFirst: true}, true
	}
	return item{}, false
}

func containsRune(rs []rune, r rune) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

// cleanName turns the name part of a line into an ingredient name: no
// quotes, emoji or edge punctuation, no size words or serving notes, a
// genitive converted to the nominative, ALL CAPS lowered, the first letter
// capitalised.
func cleanName(raw string, genitive bool) string {
	s := strings.NewReplacer("«", " ", "»", " ", "\"", " ", "“", " ", "”", " ", "*", " ").Replace(stripEmoji(raw))
	s = collapseSpaces(s)
	low := lowerKeep(s)
	for _, phrase := range dropPhrases {
		for {
			i := strings.Index(low, phrase)
			if i < 0 {
				break
			}
			s, low = s[:i]+s[i+len(phrase):], low[:i]+low[i+len(phrase):]
		}
	}
	s = trimPunct(collapseSpaces(s))
	tokens := strings.Fields(s)
	kept := tokens[:0]
	for i, t := range tokens {
		lt := lowerKeep(t)
		if hasAnyPrefix(lt, dropPrefixes) {
			continue
		}
		if lt == "можно" {
			continue
		}
		// a temperature after the noun: «масла холодного»
		if i > 0 && hasAnyPrefix(lt, []string{"холодн", "тепл", "тёпл"}) {
			continue
		}
		kept = append(kept, t)
	}
	for len(kept) > 0 && tailStarts[lowerKeep(trimPunct(kept[len(kept)-1]))] {
		kept = kept[:len(kept)-1]
	}
	s = trimPunct(strings.Join(kept, " "))
	if s == "" {
		return ""
	}
	if isAllCaps(s, 2) {
		s = strings.ToLower(s)
	}
	if genitive {
		s = genitiveToNominative(s)
	}
	if v, ok := countNouns[strings.ToLower(s)]; ok {
		s = v
	}
	s = commaSpaces.ReplaceAllString(s, ", $1")
	return truncateRunes(capitalize(s), maxNameRunes)
}

// maxNameRunes is domain.MaxItemNameLen.
const maxNameRunes = 80

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// validName reports whether a parsed name looks like a food, not prose:
// a few words, no instruction verb, no stray number.
func validName(name string) bool {
	fields := strings.Fields(name)
	if len(fields) == 0 || len(fields) > maxNameWords {
		return false
	}
	for _, f := range fields {
		if strings.Trim(f, "0123456789.,-–") == "" {
			return false
		}
	}
	for _, w := range letterWords(name) {
		if isVerb(w) || strings.HasPrefix(w, "рецепт") {
			return false
		}
	}
	return true
}

// notMeasures are words after a number that make it a time, a temperature
// or a size, never an amount of food.
var notMeasures = []string{"мин", "час", "сек", "градус", "ккал", "кал", "см", "мм", "лет", "раз", "день", "дня", "дней", "порци", "персон", "человек"}

// countWithMeasure reads «2 стейка» or «1 крупный» after a dash: a count
// with a measure word outside the unit list (the number is kept, the word
// is not) or with a size word (pieces).
func countWithMeasure(after string) (qty, bool) {
	q, ok := scanQty(after)
	if !ok || !q.number || q.unit != "" || q.oov != "" {
		return qty{}, false
	}
	rest := strings.Fields(after[q.end:])
	if len(rest) == 0 || len(rest) > 2 {
		return qty{}, false
	}
	w := strings.Trim(rest[0], ".,;:")
	if w == "" || !hasCyrillic(w) || hasAnyPrefix(w, notMeasures) || strings.ContainsAny(rest[0], "%°") {
		return qty{}, false
	}
	if !hasAnyPrefix(w, dropPrefixes) {
		q.oov = w // «2 стейка»: two of something the unit list has no word for
	}
	return q, true
}
