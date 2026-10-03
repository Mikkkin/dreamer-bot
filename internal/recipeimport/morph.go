package recipeimport

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file holds the little Russian morphology the parser needs: turning
// the genitive of «200 г белокочанной капусты» back into the nominative
// «белокочанная капуста», and telling instructions («нарежьте»,
// «обжариваем», «выпекать») from food names. It is deliberately small:
// suffix rules plus short lists of the food words that break them.

type gender int

const (
	masculine gender = iota
	feminine
	neuter
)

// nounLexicon maps genitive (and count) forms that the suffix rules get
// wrong to the nominative used as the ingredient name.
var nounLexicon = map[string]string{
	"картофелин": "картофель", "картофелины": "картофель", "картофелина": "картофель",
	"картошки": "картофель", "картошек": "картофель", "картошка": "картофель", "картофеля": "картофель",
	"луковиц": "лук", "луковицы": "лук", "луковица": "лук",
	"морковки": "морковь", "морковок": "морковь", "морковка": "морковь", "моркови": "морковь",
	"яиц": "яйца", "яйца": "яйца", "яичек": "яйца",
	"соли": "соль", "зелени": "зелень", "фасоли": "фасоль", "ванили": "ваниль",
	"вермишели": "вермишель", "форели": "форель", "печени": "печень", "сельди": "сельдь",
	"мякоти": "мякоть", "кукурузы": "кукуруза",
	"вишни": "вишня", "черешни": "черешня", "дыни": "дыня",
	"масла": "масло", "молока": "молоко", "мяса": "мясо", "теста": "тесто", "пива": "пиво",
	"вина": "вино", "сала": "сало", "толокна": "толокно", "зерна": "зерно", "яблока": "яблоко",
	"варенья": "варенье", "печенья": "печенье", "мороженого": "мороженое",
	"макарон": "макароны", "ягод": "ягоды", "трав": "травы", "слив": "сливы", "груш": "груши",
	"оливок": "оливки", "маслин": "маслины", "креветок": "креветки", "сосисок": "сосиски",
	"сарделек": "сардельки", "котлет": "котлеты", "вафель": "вафли", "специй": "специи",
	"семечек": "семечки", "яблок": "яблоки", "листьев": "листья", "сливок": "сливки",
	"конфет": "конфеты", "галет": "галеты", "фисташек": "фисташки", "клецек": "клецки",
	"плов": "плов", "сливки": "сливки", "специи": "специи",
}

// indeclinable nouns never change.
var indeclinable = map[string]bool{
	"какао": true, "кофе": true, "пюре": true, "филе": true, "эспрессо": true, "чили": true,
	"черри": true, "киви": true, "кешью": true, "тофу": true, "авокадо": true, "манго": true,
	"сулугуни": true, "нори": true, "васаби": true, "мисо": true, "капучино": true,
	"латте": true, "спагетти": true, "равиоли": true, "фузилли": true, "пенне": true,
	"брокколи": true, "маскарпоне": true, "бри": true, "табаско": true, "песто": true,
	"кимчи": true, "саке": true, "суши": true, "гхи": true,
}

// masculineSoft are nominatives in -ь that are masculine.
var masculineSoft = map[string]bool{
	"имбирь": true, "разрыхлитель": true, "миндаль": true, "картофель": true, "щавель": true,
	"фенхель": true, "кисель": true, "пельмень": true, "корень": true, "ячмень": true,
	"загуститель": true, "подсластитель": true, "ревень": true, "лосось": true, "женьшень": true,
}

func endsWithAny(s string, suffixes ...string) bool {
	for _, x := range suffixes {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}

// lastRune returns the last letter of s.
func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

func isVowel(r rune) bool { return strings.ContainsRune("аеёиоуыэюя", r) }

// hardHushing are the consonants after which Russian writes и, not ы.
func hardHushing(r rune) bool { return strings.ContainsRune("кгхжшчщ", r) }

// nounNominative converts a genitive noun (lower case) to the nominative
// and tells its number and gender.
func nounNominative(w string) (string, bool, gender) {
	if v, ok := nounLexicon[w]; ok {
		plural := strings.HasSuffix(v, "ы") || strings.HasSuffix(v, "и") || v == "яйца" || v == "листья"
		return v, plural, genderOf(v)
	}
	if indeclinable[w] || utf8.RuneCountInString(w) < 3 || !hasCyrillic(w) {
		return w, false, genderOf(w)
	}
	switch {
	case endsWithAny(w, "ов", "ев") && utf8.RuneCountInString(w) >= 5:
		stem := w[:len(w)-len("ов")]
		if hardHushing(lastRune(stem)) {
			return stem + "и", true, masculine
		}
		return stem + "ы", true, masculine
	case strings.HasSuffix(w, "ей") && utf8.RuneCountInString(w) >= 5:
		return strings.TrimSuffix(w, "ей") + "и", true, masculine
	case strings.HasSuffix(w, "ы"):
		return strings.TrimSuffix(w, "ы") + "а", false, feminine
	case strings.HasSuffix(w, "ии"):
		return strings.TrimSuffix(w, "ии") + "ия", false, feminine
	case strings.HasSuffix(w, "и"):
		stem := strings.TrimSuffix(w, "и")
		if hardHushing(lastRune(stem)) {
			return stem + "а", false, feminine
		}
		return stem + "ь", false, feminine
	case strings.HasSuffix(w, "я"):
		stem := strings.TrimSuffix(w, "я")
		if isVowel(lastRune(stem)) {
			return stem + "й", false, masculine
		}
		return stem + "ь", false, masculine
	case strings.HasSuffix(w, "а"):
		stem := strings.TrimSuffix(w, "а")
		return restoreFleeting(stem), false, masculine
	}
	return w, false, genderOf(w)
}

// restoreFleeting puts back the vowel that disappears in the genitive:
// порошка → порошок, песка → песок, перца → перец, кусочка → кусочек.
func restoreFleeting(stem string) string {
	runes := []rune(stem)
	n := len(runes)
	if n < 3 {
		return stem
	}
	last, prev := runes[n-1], runes[n-2]
	switch {
	case last == 'к' && !isVowel(prev) && prev != 'й' && prev != 'ь':
		if prev == 'ч' && n >= 3 && runes[n-3] == 'о' {
			return string(runes[:n-1]) + "ек"
		}
		return string(runes[:n-1]) + "ок"
	case last == 'ц' && !isVowel(prev):
		return string(runes[:n-1]) + "ец"
	}
	return stem
}

func genderOf(nominative string) gender {
	switch {
	case endsWithAny(nominative, "а", "я"):
		return feminine
	case endsWithAny(nominative, "о", "е", "ё"):
		return neuter
	case strings.HasSuffix(nominative, "ь"):
		if masculineSoft[nominative] {
			return masculine
		}
		return feminine
	}
	return masculine
}

// adjectiveNominative agrees a genitive adjective (lower case) with its
// noun's nominative number and gender.
func adjectiveNominative(w string, plural bool, g gender) string {
	cut := func(n int) string { return string([]rune(w)[:utf8.RuneCountInString(w)-n]) }
	soft := func(base string) bool { return strings.HasSuffix(base, "н") }
	switch {
	case endsWithAny(w, "ого", "его"):
		base := cut(3)
		softAdj := strings.HasSuffix(w, "его")
		hushing := hardHushing(lastRune(base))
		switch {
		case plural && (softAdj || hushing):
			return base + "ие"
		case plural:
			return base + "ые"
		case g == neuter && (softAdj || strings.ContainsRune("жшчщ", lastRune(base))):
			return base + "ее"
		case g == neuter:
			return base + "ое"
		case g == feminine && softAdj:
			return base + "яя"
		case g == feminine:
			return base + "ая"
		case softAdj || hushing:
			return base + "ий"
		default:
			return base + "ый"
		}
	case strings.HasSuffix(w, "ой"):
		base := cut(2)
		switch {
		case plural && hardHushing(lastRune(base)):
			return base + "ие"
		case plural:
			return base + "ые"
		case g == feminine:
			return base + "ая"
		case g == neuter:
			return base + "ое"
		}
		return w // a masculine nominative in -ой («молодой»)
	case strings.HasSuffix(w, "ей"):
		base := cut(2)
		switch {
		case plural:
			return base + "ие"
		case g == masculine:
			return base + "ий"
		case g == neuter:
			return base + "ее"
		case soft(base):
			return base + "яя"
		default:
			return base + "ая"
		}
	case endsWithAny(w, "ых", "их"):
		base := cut(2)
		hushing := hardHushing(lastRune(base))
		softAdj := strings.HasSuffix(w, "их") && !hushing
		switch {
		case plural && (hushing || softAdj):
			return base + "ие"
		case plural:
			return base + "ые"
		case g == feminine && softAdj:
			return base + "яя"
		case g == feminine:
			return base + "ая"
		case g == neuter && softAdj:
			return base + "ее"
		case g == neuter:
			return base + "ое"
		case hushing || softAdj:
			return base + "ий"
		default:
			return base + "ый"
		}
	}
	return w
}

// isGenitiveAdjective reports whether a lower-case word looks like a
// genitive adjective that precedes its noun.
func isGenitiveAdjective(w string) bool {
	return utf8.RuneCountInString(w) >= 4 && endsWithAny(w, "ого", "его", "ой", "ей", "ых", "их")
}

// genitiveToNominative rewrites a food name written in the genitive after
// an amount («белокочанной капусты», «любых ягод или фруктов») in the
// nominative. Alternatives joined by «или», «либо» or «/» are converted one
// by one; words after the noun («сок лимона», «уксуса 80%») stay as they
// are.
func genitiveToNominative(name string) string {
	for _, sep := range []string{" или ", " либо ", " и ", "/"} {
		if strings.Contains(name, sep) {
			parts := strings.Split(name, sep)
			for i, p := range parts {
				parts[i] = genitiveToNominative(strings.TrimSpace(p))
			}
			return strings.Join(parts, sep)
		}
	}
	tokens := strings.Fields(name)
	head := -1
	for i, t := range tokens {
		w := lowerKeepYo(t)
		if !isGenitiveAdjective(w) || i == len(tokens)-1 {
			head = i
			break
		}
	}
	if head < 0 {
		return name
	}
	nounParts := strings.Split(tokens[head], "-")
	plural, g := false, masculine
	for i, part := range nounParts {
		if !hasCyrillic(part) {
			continue
		}
		nom, pl, gg := nounNominative(lowerKeepYo(part))
		if i == 0 {
			plural, g = pl, gg
		}
		nounParts[i] = matchCase(part, nom)
	}
	tokens[head] = strings.Join(nounParts, "-")
	for i := 0; i < head; i++ {
		tokens[i] = matchCase(tokens[i], adjectiveNominative(lowerKeepYo(tokens[i]), plural, g))
	}
	return strings.Join(tokens, " ")
}

// lowerKeepYo lower-cases s but keeps ё, so that a converted word keeps the
// author's spelling.
func lowerKeepYo(s string) string { return strings.ToLower(s) }

// matchCase gives word the case of its first letter in like.
func matchCase(like, word string) string {
	r, _ := utf8.DecodeRuneInString(like)
	if unicode.IsUpper(r) {
		return capitalize(word)
	}
	return word
}

// --------------------------------------------------------------- verbs --

// nounsInT are nouns that end like an infinitive.
var nounsInT = map[string]bool{
	"мякоть": true, "треть": true, "четверть": true, "сеть": true, "медь": true, "плоть": true,
	"нить": true, "суть": true, "путь": true, "мать": true, "ртуть": true, "печать": true,
	"тетрадь": true, "благодать": true, "рать": true, "сайте": true,
	"режим": true, "свежим": true, "приятным": true,
}

// imperatives2sg are the singular imperatives recipes use most; the form
// is too close to nouns to be recognised by its ending.
var imperatives2sg = map[string]bool{
	"добавь": true, "смешай": true, "перемешай": true, "нарежь": true, "посоли": true,
	"поперчи": true, "налей": true, "положи": true, "поставь": true, "выложи": true,
	"залей": true, "обжарь": true, "отвари": true, "свари": true, "взбей": true, "запеки": true,
	"остуди": true, "подавай": true, "вылей": true, "всыпь": true, "влей": true, "натри": true,
	"раскатай": true, "сформируй": true, "накрой": true, "убери": true, "оставь": true,
	"разогрей": true, "растопи": true, "промой": true, "почисти": true, "очисти": true,
	"измельчи": true, "перелей": true, "жарь": true, "вари": true, "пеки": true, "туши": true,
	"режь": true, "смажь": true, "посыпь": true, "укрась": true, "полей": true, "подай": true,
	"возьми": true, "замеси": true, "доведи": true, "перемешивай": true, "отложи": true,
}

// readerVerbs address the reader, not the food: «смотрите по форме»,
// «пишите в комментариях».
var readerVerbs = map[string]bool{
	"смотрите": true, "смотри": true, "посмотрите": true, "пишите": true, "напишите": true,
	"читайте": true, "подписывайтесь": true, "сохраняйте": true, "ставьте": true,
	"переходите": true, "заходите": true, "делитесь": true, "пробуйте": true,
}

// isVerb reports whether a lower-case word looks like an instruction verb:
// an infinitive («выпекать», «довести», «печь»), a plural or polite
// imperative («нарежьте», «влейте»), a first person plural («обжариваем»,
// «режем», «ставим», «подаём») or a common singular imperative.
func isVerb(w string) bool {
	n := utf8.RuneCountInString(w)
	if n < 3 || nounsInT[w] || readerVerbs[w] {
		return false
	}
	if imperatives2sg[w] {
		return true
	}
	switch {
	case strings.HasSuffix(w, "ть") && n >= 4:
		return !strings.HasSuffix(w, "сть") || w == "класть"
	case endsWithAny(w, "ести", "зти", "йти") && n >= 5:
		return true
	case strings.HasSuffix(w, "чь") && n >= 4:
		return true
	case endsWithAny(w, "йте", "ите", "ьте") && n >= 5:
		return true
	case endsWithAny(w, "аем", "яем", "уем", "еем", "ием", "жем", "ём") && n >= 4:
		return true
	case strings.HasSuffix(w, "им") && n >= 5:
		return !endsWithAny(w, "ким", "гим", "хим", "чим", "ным", "щим")
	}
	return false
}

// isSecondPersonSingular reports a future or present «ты» form
// («попробуешь», «будешь»): chat with the reader, not an instruction.
func isSecondPersonSingular(w string) bool {
	return utf8.RuneCountInString(w) >= 5 && endsWithAny(w, "ешь", "ишь", "ёшь")
}

// isParticiple reports a participle that starts a preparation note
// («нарезанный на кусочки», «натёртый»).
func isParticiple(w string) bool {
	return utf8.RuneCountInString(w) >= 6 && endsWithAny(w,
		"анный", "янный", "енный", "ённый", "анная", "енная", "ённая", "анные", "енные", "ённые",
		"анное", "енное", "тый", "тая", "тое", "тые", "нутый")
}
