package recipeimport

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Parsed is a recipe read from a caption. Title is "" when the caption
// never names the dish; Servings is 0 when unknown. Confidence is 0..1:
// about 0.9 for a caption with a quantified ingredient list and steps,
// lower for a list without steps or prose without a list, and below 0.2
// when the text is not a recipe. Warnings are short Russian notes for the
// user («Для диапазонов взято меньшее значение: …»), at most maxWarnings
// of them.
type Parsed struct {
	Title       string
	Servings    int
	Ingredients []domain.Ingredient
	Steps       []string
	Confidence  float64
	Warnings    []string
}

// notARecipe reports a parse that holds nothing to import.
func (p Parsed) notARecipe() bool {
	return (len(p.Ingredients) == 0 && len(p.Steps) == 0) || p.Confidence < minConfidence
}

// minConfidence is the score below which a caption is not a recipe.
const minConfidence = 0.2

// maxSteps bounds the steps kept from one caption.
const maxSteps = 50

// maxRangeNotes bounds the foods named in the range warning.
const maxRangeNotes = 5

// maxWarnings bounds the warnings of one parse; the last one then counts
// the rest («…и ещё 7»).
const maxWarnings = 10

// capWarnings keeps at most maxWarnings warnings.
func capWarnings(ws []string) []string {
	if len(ws) <= maxWarnings {
		return ws
	}
	rest := len(ws) - (maxWarnings - 1)
	return append(ws[:maxWarnings-1:maxWarnings-1], fmt.Sprintf("…и ещё %d", rest))
}

// Parse reads a recipe from an Instagram caption (or any pasted recipe
// text) with deterministic rules. It never fails: an empty or unrelated
// text gives an empty result with a confidence near zero.
func Parse(caption string) Parsed {
	p := &parser{cyrillic: hasCyrillic(caption)}
	for _, raw := range strings.Split(normalizeInvisible(caption), "\n") {
		p.lines = append(p.lines, prepareLine(raw))
	}
	p.run()
	return p.result()
}

// ------------------------------------------------------------- lines --

// line is one caption line with its decoration taken apart.
type line struct {
	text       string   // no hashtags, bullets, numbering or emoji; spaces collapsed
	low        string   // lowerKeep(text)
	segments   []string // the line split at emoji, hashtags removed (title candidates)
	bullet     string   // the bullet that was removed, "" for none
	numbered   bool     // «1.», «1)», «1️⃣», «Шаг 1»
	tip        bool     // starts with 💡 ❗ ‼ ⚠: a tip, not a step
	pointsDown bool     // ends with 👇 or ⬇: introduces what follows
	hashtags   bool     // the line was only hashtags
	blank      bool
	dot        bool // a lone «.» (some captions end a section with it)
}

var (
	hashtagRe   = regexp.MustCompile(`#+[^\s#]*`)
	keycapRe    = regexp.MustCompile(`^[0-9]\x{FE0F}?\x{20E3}`)
	numberingRe = regexp.MustCompile(`^(?:шаг\s*)?\d{1,2}\s*[.)]\s*`)
	stepWordRe  = regexp.MustCompile(`^шаг\s*\d{1,2}\s*[:.)\-–—]?\s*`)
	bulletRunes = "•·◦●○■□-–—*+>→"
)

func prepareLine(raw string) line {
	s := strings.TrimSpace(raw)
	var ln line
	if s == "" {
		ln.blank = true
		return ln
	}
	if tags := hashtagRe.FindAllStringIndex(s, -1); len(tags) > 0 {
		prefix := strings.TrimSpace(s[:tags[0][0]])
		if len(tags) >= 2 {
			if len(strings.Fields(prefix)) <= 1 {
				ln.hashtags = true
				return ln
			}
			s = prefix
		} else {
			s = strings.TrimSpace(s[:tags[0][0]] + " " + s[tags[0][1]:])
		}
	}
	first, _ := utf8.DecodeRuneInString(s)
	ln.tip = first == '💡' || first == '❗' || first == '‼' || first == '⚠'
	ln.pointsDown = strings.ContainsRune(lastEmojiRun(s), '👇') || strings.ContainsRune(lastEmojiRun(s), '⬇')
	ln.segments = splitAtEmoji(s)

	rest := s
	for {
		rest = strings.TrimLeft(rest, " ")
		if m := keycapRe.FindStringIndex(rest); m != nil {
			ln.numbered = true
			rest = rest[m[1]:]
			continue
		}
		r, size := utf8.DecodeRuneInString(rest)
		switch {
		case r == '🔟':
			ln.numbered = true
		case isEmoji(r):
			if ln.bullet == "" {
				ln.bullet = string(r)
			}
		case strings.ContainsRune(bulletRunes, r) && size > 0:
			// «- Соль», «-засыпаем», «• рикотта», «· 1 яйцо»
			if ln.bullet == "" {
				ln.bullet = string(r)
			}
		default:
			size = 0
		}
		if size == 0 {
			break
		}
		rest = rest[size:]
	}
	if !ln.numbered {
		low := lowerKeep(rest)
		if m := numberingRe.FindStringIndex(low); m != nil && !startsDecimal(rest) {
			ln.numbered = true
			rest = rest[m[1]:]
		} else if m := stepWordRe.FindStringIndex(low); m != nil {
			ln.numbered = true
			rest = rest[m[1]:]
		}
	}
	ln.text = collapseSpaces(stripEmoji(rest))
	ln.low = lowerKeep(ln.text)
	switch strings.Trim(ln.text, " ") {
	case "":
		ln.blank = true
	case ".", "..", "…":
		ln.dot = true
	}
	return ln
}

// startsDecimal reports text that begins with a decimal number («1.5 л»),
// which is not step numbering.
func startsDecimal(s string) bool {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(s) && (s[i] == '.' || s[i] == ',') && s[i+1] >= '0' && s[i+1] <= '9'
}

// lastEmojiRun returns the emoji at the end of s.
func lastEmojiRun(s string) string {
	end := len(s)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:end])
		if !isEmoji(r) && r != ' ' {
			break
		}
		end -= size
	}
	return s[end:]
}

// splitAtEmoji splits s at every run of emoji and returns the trimmed,
// non-empty pieces.
func splitAtEmoji(s string) []string {
	var out []string
	for _, piece := range strings.FieldsFunc(s, isEmoji) {
		if p := strings.TrimSpace(piece); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// stepText is the text of a step line: no numbering, bullets, emoji or a
// trailing «Рецепт в видео».
func (ln line) stepText() string {
	t := ln.text
	if loc := videoTail.FindStringIndex(ln.low); loc != nil {
		t = t[:loc[0]]
	}
	return trimStep(t)
}

func trimStep(s string) string {
	return strings.TrimRight(strings.TrimLeft(collapseSpaces(s), " -–—•·:"), " ")
}

// words returns the line's lower-case letter words.
func (ln line) words() []string { return letterWords(ln.text) }

// ------------------------------------------------- line classification --

var (
	ingredientsHeaderRe = regexp.MustCompile(`^(?:основные\s+|все\s+)?(?:ингредиенты|ингридиенты|ингредиент|ингридиент|нам\s+понадобится|вам\s+понадобится|понадобится|что\s+нужно|что\s+понадобится|продукты|состав|список\s+продуктов)\s*(.*)$`)
	headerServingsRe    = regexp.MustCompile(`^на\s+\S+(?:\s*[-–]\s*\d+)?\s+(?:порци|персон|человек)\S*$`)
	stepsHeaderRe       = regexp.MustCompile(`^(?:способ\s+приготовления|приготовление|пошаговое\s+приготовление|пошаговый\s+рецепт|как\s+приготовить|как\s+готовить|как\s+готовим|готовим(?:\s+так)?|делаем(?:\s+так)?|рецепт|шаги|техника\s+приготовления|тонкости\s+приготовления|инструкция|процесс\s+приготовления|процесс|этапы\s+приготовления|приготовление\s+пошагово)$`)
	videoPlaceholderRe  = regexp.MustCompile(`^(?:приготовление|способ\s+приготовления|рецепт|процесс\s+приготовления|процесс|как\s+готовить|подробный\s+рецепт|смотрите|смотри)[^.!?]{0,30}?\s(?:на|в)\s+видео`)
	videoTail           = regexp.MustCompile(`(?:рецепт|приготовление|подробнее)\s+(?:в|на)\s+видео.*$`)
	servingsRes         = []*regexp.Regexp{
		regexp.MustCompile(`(?:на|выход\s*:?)\s*(\d{1,2})(?:\s*[-–]\s*\d{1,2})?\s*(?:порци|персон|человек)`),
		regexp.MustCompile(`(\d{1,2})(?:\s*[-–]\s*\d{1,2})?\s*порци`),
		regexp.MustCompile(`(одна|одну|один|две|два|три|четыре|пять|шесть|семь|восемь)\s+порци`),
	}
	timeTempRe = regexp.MustCompile(`\d+\s*(?:°|градус|минут|мин(?:\.|\s|$)|час|сек)`)
)

// ingredientsHeader recognises «Ингредиенты:», «ИНГРЕДИЕНТЫ (2 порции)»,
// «Что нужно:», «Ингредиенты (для курочки):» and the like, but not a
// sentence that starts with the word.
func ingredientsHeader(ln line) bool {
	m := ingredientsHeaderRe.FindStringSubmatch(ln.low)
	if m == nil {
		return false
	}
	extra := strings.Trim(m[1], " :.")
	switch {
	case extra == "":
		return true
	case strings.HasPrefix(extra, "(") && strings.HasSuffix(extra, ")"):
		return true
	case headerServingsRe.MatchString(extra):
		return true
	case strings.HasPrefix(extra, "для ") && len(strings.Fields(extra)) <= 4:
		return true
	}
	return false
}

func stepsHeader(ln line) bool {
	return stepsHeaderRe.MatchString(strings.Trim(ln.low, " :.!"))
}

// isRecipeWord reports a line that only says «Рецепт» (often «Рецепт ⤵️»
// before the list).
func isRecipeWord(ln line) bool { return strings.Trim(ln.low, " :.!") == "рецепт" }

// sectionHeader recognises a sub-header of the list: «Начинка:», «🧾
// Тесто:», «Для соуса:», «Кроме того:».
func sectionHeader(ln line) bool {
	t := strings.TrimSpace(ln.text)
	switch {
	case strings.HasSuffix(t, ":"):
		t = strings.TrimSpace(strings.TrimSuffix(t, ":"))
	case strings.HasPrefix(ln.low, "для ") && len(ln.words()) <= 3 && !strings.ContainsAny(t, "0123456789,."):
	default:
		return false
	}
	words := letterWords(t)
	if len(words) == 0 || len(words) > 5 || strings.ContainsAny(t, "0123456789?!:") {
		return false
	}
	for _, w := range words {
		if isVerb(w) {
			return false
		}
	}
	return true
}

// servingsIn finds «на 4 порции», «(2 порции)», «Выход: 3-4 порции» or
// «одна порция» in a lower-case line.
func servingsIn(low string) int {
	for _, re := range servingsRes {
		m := re.FindStringSubmatch(low)
		if m == nil {
			continue
		}
		if n, ok := numberWords[m[1]]; ok {
			return int(n / 100)
		}
		n, _ := strconv.Atoi(m[1])
		if n >= 1 && n <= domain.MaxServings {
			return n
		}
	}
	return 0
}

// junkPhrases mark calls to action, credits, ads and sign-offs.
var junkPhrases = []string{
	"в закладк", "подпис", "в комментари", "коммент", "смайлик", "лайк",
	"поделит", "поделись", "репост", "не забудь", "заказыва",
	"по всем вопросам", "приятного аппетита", "приятного просмотра", "всем приятного",
	"реклама", "промокод", "артикул", "http", "www.", "в шапке", "ссылка в", "ссылку в",
	"в профиле", "в приложении", "на странице", "бон аппетит",
}

var junkStarts = []string{
	"автор:", "автор ", "фото:", "фото ", "p.s", "п.с", "ps:", "кбжу", "калорийность",
	"пищевая ценность", "всего ", "———", "---", "___",
}

// junkWords open a sign-off or an address to the readers.
var junkWords = map[string]bool{"друзья": true, "девочки": true, "готово": true}

// ctaRe matches calls to action whose verbs are also cooking verbs: «сохраните
// рецепт» but not «сохраните в холодильнике», «отправьте подруге» but not
// «отправьте в духовку».
var ctaRe = regexp.MustCompile(`(?:^|[^а-я])(?:обязательно\s+сохран|сохран[а-я]*\s+и\s|сохран[а-я]*\s+(?:себе|рецепт|пост|видео|идею|подборку|чтобы)|сохран[а-я]*,?\s+чтобы\s+не\s+потерять|отправ[а-я]*\s+(?:подруг|друг|тому|той|тем|рецепт|близк|маме|мужу|себе)|(?:пере|за)ходи(?:те)?(?:\s+по|\s+в\s+профиль)|(?:^|\s)жми(?:те)?(?:\s|$))`)

var (
	mentionRe = regexp.MustCompile(`@[a-z0-9_.]{3,}`)
	phoneRe   = regexp.MustCompile(`\+?\d[\d\s()-]{9,}\d`)
	domainRe  = regexp.MustCompile(`[a-z0-9-]+\.(?:ru|kz|com|net|org|by|ua)\b`)
)

// isJunk reports a line with nothing for the recipe: a call to action, a
// credit, an ad, a sign-off, hashtags, or a foreign-language repeat.
func (p *parser) isJunk(ln line) bool {
	if ln.hashtags {
		return true
	}
	low := ln.low
	for _, s := range junkStarts {
		if strings.HasPrefix(low, s) {
			return true
		}
	}
	if words := ln.words(); len(words) > 0 && junkWords[words[0]] {
		return true
	}
	for _, s := range junkPhrases {
		if strings.Contains(low, s) {
			return true
		}
	}
	if ctaRe.MatchString(low) {
		return true
	}
	if mentionRe.MatchString(low) || phoneRe.MatchString(low) || domainRe.MatchString(low) {
		return true
	}
	return p.cyrillic && foreignLetters(ln.text)
}

// narrativeStarts open chat with the reader rather than an instruction.
var narrativeStarts = map[string]bool{
	"я": true, "мы": true, "вы": true, "ты": true, "он": true, "она": true, "они": true,
	"оно": true, "если": true, "также": true, "кстати": true, "честно": true, "а": true,
	"но": true, "итак": true, "ведь": true, "вот": true, "это": true, "этот": true, "эта": true,
	"эти": true, "такое": true, "такой": true, "такая": true, "такие": true, "получается": true,
	"получилось": true, "получится": true, "спасибо": true, "можно": true, "совет": true,
	"лайфхак": true, "важно": true, "у": true, "мне": true, "нам": true, "вам": true, "когда": true,
}

func (ln line) narrative() bool {
	words := ln.words()
	if len(words) == 0 {
		return true
	}
	if strings.Contains(ln.text, "?") || narrativeStarts[words[0]] || isSecondPersonSingular(words[0]) {
		return true
	}
	return strings.HasPrefix(ln.low, "по желанию") || strings.HasPrefix(ln.low, "p.s") || strings.HasPrefix(ln.low, "п.с")
}

// stepLike reports an instruction: a verb or a time or temperature, and no
// chat with the reader.
func (ln line) stepLike() bool {
	if ln.narrative() {
		return false
	}
	if timeTempRe.MatchString(ln.low) {
		return true
	}
	for _, w := range ln.words() {
		if isVerb(w) {
			return true
		}
	}
	return false
}

// firstWordVerb reports an instruction that starts with its verb
// («Дать настояться…»).
func (ln line) firstWordVerb() bool {
	words := ln.words()
	return len(words) > 0 && !strings.Contains(ln.text, "?") && isVerb(words[0])
}

// ------------------------------------------------------------- parser --

type parseState int

const (
	statePreamble parseState = iota
	stateIngredients
	stateSteps
	stateAfter // after the steps or a «Приготовление на видео»
	stateDone
)

type parser struct {
	lines    []line
	cyrillic bool

	state      parseState
	preamble   []line
	items      []item
	steps      []string
	stepSrc    []string // the line that opened each step
	servings   int
	headerSeen bool
	multi      bool
	unparsed   int

	recipeEnded       bool
	lastWasIngredient bool

	// step block state
	sawNumbered, stepOpen, tail, prevBlank bool
	stepBullet                             string
}

func (p *parser) run() {
	for _, ln := range p.lines {
		if p.state == stateDone {
			break
		}
		if ln.blank {
			p.prevBlank = true
			continue
		}
		if ln.dot {
			p.prevBlank = true
			p.lastWasIngredient = false
			continue
		}
		if p.servings == 0 && !ln.hashtags {
			p.servings = servingsIn(ln.low)
		}
		if videoPlaceholderRe.MatchString(ln.low) {
			if len(p.items) > 0 || len(p.steps) > 0 {
				p.recipeEnded = true
				p.state = stateAfter
			}
			p.prevBlank = false
			continue
		}
		if p.isJunk(ln) {
			p.stepOpen = false
			if p.state == stateSteps && len(p.steps) > 0 && signOff(ln) {
				p.state = stateAfter
			}
			continue
		}
		p.handle(ln)
		p.prevBlank = false
	}
}

func (p *parser) handle(ln line) {
	if head, rest, ok := inlineHeader(ln); ok {
		p.handle(head)
		switch {
		case p.state == stateIngredients && ingredientsHeader(head):
			p.ingredientsLine(rest)
		case p.state == stateSteps && stepsHeader(head):
			p.stepLine(rest)
		}
		return
	}
	switch {
	case ingredientsHeader(ln):
		if (p.recipeEnded || p.state == stateSteps || p.state == stateAfter) && len(p.items) > 0 {
			p.multi = true
			p.state = stateDone
			return
		}
		p.state = stateIngredients
		p.headerSeen = true
		p.lastWasIngredient = true
		return
	case stepsHeader(ln) && (!isRecipeWord(ln) || len(p.items) > 0):
		if p.state == stateAfter {
			return
		}
		p.state = stateSteps
		p.sawNumbered, p.stepOpen, p.tail, p.stepBullet = false, false, false, ""
		return
	case isRecipeWord(ln):
		return
	case servingsOnly(ln):
		return
	}
	switch p.state {
	case stateAfter:
		return
	case stateSteps:
		p.stepLine(ln)
		return
	}
	if sectionHeader(ln) {
		p.lastWasIngredient = true
		return
	}
	if p.state == stateIngredients {
		p.ingredientsLine(ln)
		return
	}
	if items, ok := p.readIngredients(ln, false); ok {
		p.state = stateIngredients
		p.claimCounts()
		p.addItems(items)
		return
	}
	p.preamble = append(p.preamble, ln)
}

// inlineHeader splits a header that carries its section on the same line:
// «Ингредиенты: филе бедра 600 г, соевый соус 4 ст.л., …» (two or more
// foods with amounts after the colon) or «Приготовление: обжарить…» (an
// instruction after the colon). head is the header alone, rest the text
// after the colon.
func inlineHeader(ln line) (head, rest line, ok bool) {
	before, after, found := strings.Cut(ln.text, ":")
	if !found || strings.TrimSpace(after) == "" {
		return line{}, line{}, false
	}
	head = line{text: before + ":", low: lowerKeep(before + ":")}
	rest = prepareLine(after)
	switch {
	case rest.blank:
		return line{}, line{}, false
	case ingredientsHeader(head):
		quantified := 0
		for _, it := range parseLine(rest.text) {
			if it.q.hasQuantity() && validName(it.name) {
				quantified++
			}
		}
		return head, rest, quantified >= 2
	case stepsHeader(head) && !isRecipeWord(head):
		rest.text = capitalize(rest.text)
		return head, rest, hasVerb(rest) && !rest.narrative()
	}
	return line{}, line{}, false
}

// claimCounts moves the lines at the end of the preamble that are foods
// with a bare count («2 яйца», «1 огурец») into the list that starts right
// after them: before the first amount with a unit they looked like any
// other line.
func (p *parser) claimCounts() {
	n := len(p.preamble)
	var claimed []item
	for n > 0 {
		items, ok := quantifiedLine(p.preamble[n-1])
		if !ok {
			break
		}
		claimed = append(items, claimed...)
		n--
	}
	p.preamble = p.preamble[:n]
	p.items = append(p.items, claimed...)
}

// quantifiedLine reads a line outside any list as foods when every food
// has an amount or a measure word («Горсть орехов») and the line is short
// and not a sentence.
func quantifiedLine(ln line) ([]item, bool) {
	if strings.HasPrefix(ln.low, "можно ") {
		return nil, false
	}
	items := parseLine(ln.text)
	if len(items) == 0 {
		return nil, false
	}
	for _, it := range items {
		if !validName(it.name) || (!it.q.hasQuantity() && it.q.oov == "") {
			return nil, false
		}
	}
	return items, shortList(ln.text, len(items))
}

// shortList reports text that may be a list of n foods: no sentence, no
// question or exclamation, at most maxWordsPerItem words per food.
func shortList(text string, n int) bool {
	plain, _ := blankParens(text)
	return !sentenceEnd.MatchString(plain) && !strings.ContainsAny(plain, "?!") &&
		len(letterWords(plain)) <= maxWordsPerItem*n
}

// maxWordsPerItem bounds the words per food of a list read without a
// header; longer text is prose with incidental numbers.
const maxWordsPerItem = 10

func (p *parser) ingredientsLine(ln line) {
	// «Духовка 180°, 12-15 минут»: a time or a temperature with no unit of
	// food is an instruction, not a list line.
	if plain, _ := blankParens(ln.low); timeTempRe.MatchString(plain) && !withUnit(parseLine(ln.text)) {
		if !ln.tip {
			p.addStep(ln.stepText(), ln.text)
		}
		p.lastWasIngredient = false
		return
	}
	if items, ok := inlineLabel(ln); ok {
		p.addItems(items)
		return
	}
	if items, ok := p.readIngredients(ln, true); ok {
		p.addItems(items)
		return
	}
	if ln.numbered && ln.stepLike() {
		p.state = stateSteps
		p.sawNumbered, p.stepOpen, p.tail, p.stepBullet = false, false, false, ""
		p.stepLine(ln)
		return
	}
	if !ln.tip && ln.stepLike() {
		p.addStep(ln.stepText(), ln.text)
		p.lastWasIngredient = false
		return
	}
	if strings.ContainsAny(ln.text, "0123456789½¼¾⅓") && !timeTempRe.MatchString(ln.low) {
		p.unparsed++
	}
}

// withUnit reports a food with a unit of the fixed list among items.
func withUnit(items []item) bool {
	for _, it := range items {
		if it.q.unit != "" {
			return true
		}
	}
	return false
}

// inlineLabel reads «Специи: соль, паприка, чеснок сушеный» as a list.
func inlineLabel(ln line) ([]item, bool) {
	label, rest, ok := strings.Cut(ln.text, ":")
	if !ok {
		return nil, false
	}
	label, rest = strings.TrimSpace(label), strings.TrimSpace(rest)
	words := letterWords(label)
	if len(words) == 0 || len(words) > 3 || strings.ContainsAny(label, "0123456789") || rest == "" || !strings.Contains(rest, ",") {
		return nil, false
	}
	// «яйцо :4 шт» is one ingredient; «Соус: 50 г сметаны, 50 г майонеза»
	// is a list.
	if _, ok := scanQty(lowerKeep(rest)); ok && !strings.Contains(rest, ", ") {
		return nil, false
	}
	items := parseLine(rest)
	if len(items) == 0 {
		return nil, false
	}
	for _, it := range items {
		if !validName(it.name) || len(strings.Fields(it.name)) > 4 {
			return nil, false
		}
	}
	return items, true
}

// readIngredients reads a line as ingredients. Under an ingredients header
// (explicit) a short line without amounts counts too («Ванилин», «Соль,
// черный перец»); before any header only a line whose every food has an
// amount does, so prose with incidental numbers is not mistaken for a list.
func (p *parser) readIngredients(ln line, explicit bool) ([]item, bool) {
	text, optionalFood := ln.text, false
	if strings.HasPrefix(ln.low, "можно ") {
		optionalFood = true
		// «Можно грибы добавить» is an optional food; «Можно заменить на
		// паприку» is a note.
		var kept []string
		for _, w := range strings.Fields(text) {
			lw := lowerKeep(trimPunct(w))
			if lw == "можно" || lw == "добавить" || lw == "положить" || lw == "взять" {
				continue
			}
			kept = append(kept, w)
		}
		text = strings.Join(kept, " ")
		for _, w := range letterWords(text) {
			if isVerb(w) {
				return nil, false
			}
		}
		if !explicit || text == "" {
			return nil, false
		}
	}
	items := parseLine(text)
	if len(items) == 0 {
		return nil, false
	}
	quantified, measured := 0, 0
	for _, it := range items {
		if !validName(it.name) {
			return nil, false
		}
		if it.q.hasQuantity() {
			quantified++
		}
		if it.q.unit != "" || it.q.toTaste {
			measured++
		}
	}
	plain, _ := blankParens(text)
	sentence := sentenceEnd.MatchString(plain)
	if !explicit {
		return items, quantified == len(items) && measured > 0 && shortList(text, len(items))
	}
	if quantified > 0 {
		return items, true
	}
	ok := p.lastWasIngredient && !sentence && len(strings.Fields(plain)) <= maxNameWords &&
		!strings.ContainsAny(plain, "?!") && (optionalFood || !ln.narrative())
	return items, ok
}

func (p *parser) addItems(items []item) {
	p.items = append(p.items, items...)
	p.lastWasIngredient = true
}

func (p *parser) addStep(text, source string) {
	if text == "" || len(p.steps) >= maxSteps {
		return
	}
	p.steps = append(p.steps, text)
	p.stepSrc = append(p.stepSrc, source)
}

// stepLine handles a line under a steps header. Numbered steps may carry
// continuation lines and sub-bullets; bullet steps use one bullet; plain
// lines are one step each. After a blank line that ends a numbered or
// bulleted block only lines that start with a verb (or a bulleted
// instruction) continue the steps; anything else ends them.
func (p *parser) stepLine(ln line) {
	if (ln.pointsDown && !ln.numbered) || ln.tip || (sectionHeader(ln) && !ln.numbered) {
		p.stepOpen = false
		return
	}
	text := ln.stepText()
	if text == "" {
		return
	}
	structured := p.sawNumbered || p.stepBullet != ""
	switch {
	case ln.numbered:
		p.sawNumbered, p.tail = true, false
		p.addStep(text, ln.text)
		p.stepOpen = true
	case !p.sawNumbered && !p.tail && ln.bullet != "" && ln.bullet == p.stepBullet:
		p.addStep(text, ln.text)
		p.stepOpen = true
	case structured && !p.tail && !p.prevBlank && p.stepOpen && len(p.steps) > 0:
		p.steps[len(p.steps)-1] += " " + text
	case structured:
		p.tail = true
		if (ln.bullet != "" && ln.stepLike()) || ln.firstWordVerb() {
			p.addStep(text, ln.text)
			p.stepOpen = false
			return
		}
		p.state = stateAfter
	case ln.bullet != "" && !ln.narrative():
		p.stepBullet = ln.bullet
		p.addStep(text, ln.text)
		p.stepOpen = true
	default:
		if ln.narrative() {
			if len(p.steps) > 0 {
				p.state = stateAfter
			}
			return
		}
		if ln.stepLike() || len(ln.words()) >= 3 {
			p.addStep(text, ln.text)
		}
		p.stepOpen = false
	}
}

// ------------------------------------------------------------ results --

func (p *parser) result() Parsed {
	var warnings []string
	fromBullets := false
	if len(p.items) == 0 {
		p.items = bulletList(p.preamble)
		fromBullets = len(p.items) > 0
	}
	if (len(p.items) == 0 || fromBullets) && len(p.steps) == 0 {
		// Prose without a list or headers: two or more instruction lines
		// make a low-confidence recipe; one is a passing remark.
		var prose []line
		for _, ln := range p.preamble {
			if ln.stepLike() && hasVerb(ln) {
				prose = append(prose, ln)
			}
		}
		if len(prose) >= 2 {
			for _, ln := range prose {
				p.addStep(ln.stepText(), ln.text)
			}
		}
	}
	if len(p.items) == 0 && len(p.steps) > 0 {
		// «-Апероль ~60 мл»: no list, the steps name the amounts.
		for _, src := range p.stepSrc {
			for _, it := range parseLine(src) {
				if it.q.number && it.q.unit != "" && validName(it.name) {
					p.items = append(p.items, it)
				}
			}
		}
	}
	var (
		ings   []domain.Ingredient
		ranges []string
	)
	for _, it := range p.items {
		ing, err := domain.NormalizeIngredients([]domain.Ingredient{{Name: it.name, Quantity: it.q.quantity()}})
		if err != nil || len(ing) != 1 {
			continue
		}
		if it.q.rangeText != "" && len(ings) < domain.MaxIngredientsPerRecipe {
			ranges = append(ranges, ing[0].Name+" ("+strings.ReplaceAll(it.q.rangeText, "-", "–")+")")
		}
		ings = append(ings, ing[0])
	}
	if len(ings) > domain.MaxIngredientsPerRecipe {
		ings = ings[:domain.MaxIngredientsPerRecipe]
		warnings = append(warnings, fmt.Sprintf("Ингредиентов больше %d — лишние не добавлены", domain.MaxIngredientsPerRecipe))
	}
	if len(ranges) > maxRangeNotes {
		ranges = append(ranges[:maxRangeNotes], fmt.Sprintf("и ещё %d", len(ranges)-maxRangeNotes))
	}
	if len(ranges) > 0 {
		warnings = append(warnings, "Где был диапазон, взято меньшее значение: "+strings.Join(ranges, ", "))
	}
	if p.multi {
		warnings = append(warnings, "В подписи несколько рецептов — разобран первый")
	}
	if p.unparsed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d %s не %s", p.unparsed,
			pluralRu(p.unparsed, "строка", "строки", "строк"), pluralRu(p.unparsed, "распознана", "распознаны", "распознаны")))
	}
	out := Parsed{
		Title:       pickTitle(p.preamble),
		Servings:    p.servings,
		Ingredients: ings,
		Steps:       p.steps,
		Warnings:    capWarnings(warnings),
	}
	out.Confidence = p.confidence(ings)
	return out
}

// adWords mark a post that sells something instead of sharing a recipe.
var adWords = []string{"сборник", "скачива", "марафон", "промокод", "электронн", "купить", "отбеливател", "стирк"}

func (p *parser) confidence(ings []domain.Ingredient) float64 {
	quantified := 0
	for _, ing := range ings {
		if ing.Quantity != nil {
			quantified++
		}
	}
	var c float64
	switch {
	case quantified >= 2:
		c = 0.55
	case quantified == 1:
		c = 0.3
	case len(ings) >= 2 && p.headerSeen:
		c = 0.3
	case len(ings) >= 1:
		c = 0.15
	}
	if p.headerSeen && len(ings) > 0 {
		c += 0.15
	}
	if len(p.steps) > 0 {
		c += 0.25
	}
	if len(ings) >= 4 {
		c += 0.05
	}
	c -= math.Min(0.05*float64(p.unparsed), 0.2)
	var all strings.Builder
	for _, ln := range p.lines {
		all.WriteString(ln.low)
		all.WriteByte('\n')
	}
	for _, w := range adWords {
		if strings.Contains(all.String(), w) {
			c -= 0.3
			break
		}
	}
	c = math.Max(0, math.Min(c, 0.95))
	return math.Round(c*100) / 100
}

// ------------------------------------------------------------- title --

// titleStops are words a dish name never starts with.
var titleStops = map[string]bool{
	"и": true, "а": true, "но": true, "ну": true, "да": true, "вот": true, "это": true, "так": true,
	"итак": true, "как": true, "когда": true, "если": true, "ведь": true, "мы": true, "я": true,
	"вы": true, "ты": true, "он": true, "она": true, "они": true, "всем": true, "привет": true,
	"друзья": true, "девочки": true, "что": true, "кто": true, "где": true, "почему": true,
	"зачем": true, "у": true, "в": true, "на": true, "из": true, "для": true, "по": true,
	"мне": true, "нам": true, "вам": true, "сегодня": true, "хочу": true, "хотите": true,
	"сохрани": true, "сохраняй": true, "подписывайся": true, "смотрите": true, "согласны": true,
}

var (
	demonstrativeRe = regexp.MustCompile(`^(?:этот|эта|это|эти)\s+(.+?)\s+(?:стал|стала|стало|стали|—|-|получается|получился|получилась|готовится|покорил|покорила)(?:\s|$)`)
	whichRe         = regexp.MustCompile(`,\s*котор(?:ая|ый|ое|ые|ую|ого)\s`)
	quotedRe        = regexp.MustCompile(`«([^»]{2,80})»`)
	trailingParenRe = regexp.MustCompile(`\s*\([^()]*\)\s*$`)
	titleServingsRe = regexp.MustCompile(`(?i)\s*\(?\s*на\s+\d{1,2}(?:\s*[-–]\s*\d{1,2})?\s+(?:порци|персон)[а-яё]*\s*\)?\s*$`)
)

// pickTitle takes the dish name from the first meaningful lines before the
// list: cut at the first emoji or «, которая…», without a leading
// «Рецепт», a trailing parenthesis or punctuation, ALL CAPS lowered. A
// line that is a call to action, a sentence of prose or a generic «Самый
// идеальный рецепт» is skipped; "" means the caption never names the dish.
func pickTitle(pre []line) string {
	// After a long intro the dish name is often the heading right above the
	// list: «Все блюда простые… / Жаркое с курицей🔥 / ИНГРЕДИЕНТЫ:».
	if n := len(pre); n >= 3 {
		if t := headingTitle(pre[n-1]); t != "" {
			return t
		}
	}
	tried := 0
	for _, ln := range pre {
		if tried == 3 {
			break
		}
		tried++
		for i, seg := range ln.segments {
			// «Как же это вкусно! 🤤 Шоколадный кекс…»: an exclamation
			// before the name.
			if i+1 < len(ln.segments) && strings.HasSuffix(seg, "!") {
				continue
			}
			if t := titleFrom(seg); t != "" {
				return t
			}
		}
	}
	return ""
}

func titleFrom(seg string) string {
	s := collapseSpaces(seg)
	low := lowerKeep(s)
	if strings.HasPrefix(low, "рецепт") {
		rest := strings.TrimLeft(s[len("рецепт"):], " :")
		if rest == "" {
			return ""
		}
		r, _ := utf8.DecodeRuneInString(rest)
		if unicode.IsLower(r) {
			if m := quotedRe.FindStringSubmatch(rest); m != nil {
				return finishTitle(m[1])
			}
			return ""
		}
		s, low = rest, lowerKeep(rest)
	}
	if loc := sentenceEnd.FindStringIndex(s); loc != nil {
		return ""
	}
	if m := demonstrativeRe.FindStringSubmatchIndex(low); m != nil {
		return finishTitle(s[m[2]:m[3]])
	}
	if loc := whichRe.FindStringIndex(low); loc != nil {
		s, low = s[:loc[0]], low[:loc[0]]
	}
	words := letterWords(s)
	if len(words) == 0 || len(words) > 12 || titleStops[words[0]] {
		return ""
	}
	for _, w := range words {
		if isVerb(w) {
			return "" // an instruction, not a dish name
		}
	}
	unquoted := quotedRe.ReplaceAllString(low, "")
	if strings.Contains(unquoted, "рецепт") || strings.Contains(low, "ккал") || strings.Contains(s, "?") {
		return ""
	}
	if strings.Contains(strings.TrimRight(s, ": "), ":") {
		return ""
	}
	if ingredientsHeader(line{low: low, text: s}) {
		return ""
	}
	// «2 яйца», «1 огурец», «Горсть орехов»: a food with an amount, not a
	// dish name (a quoted name such as «3 стакана» is one).
	for _, it := range parseLine(unquoted) {
		if it.q.hasQuantity() || it.q.oov != "" {
			return ""
		}
	}
	return finishTitle(s)
}

func finishTitle(s string) string {
	s = trailingParenRe.ReplaceAllString(strings.TrimRight(s, " :.!…"), "")
	s = titleServingsRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(strings.TrimRight(s, " :.!…,"))
	if s == "" || utf8.RuneCountInString(s) > 100 {
		return ""
	}
	if isAllCaps(s, 4) {
		s = sentenceCase(s)
	}
	return truncateRunes(capitalize(s), domain.MaxTitleLen)
}

// signOff reports a closing line («Готово!», «Приятного аппетита») after
// which a caption no longer lists steps.
func signOff(ln line) bool {
	words := ln.words()
	return (len(words) > 0 && words[0] == "готово") || strings.Contains(ln.low, "приятного аппетита") ||
		strings.Contains(ln.low, "бон аппетит")
}

// servingsWords are the words of a line that only states the servings
// («Выход: 3-4 порции», «Рецепт на 4 порции»).
var servingsWords = map[string]bool{
	"на": true, "выход": true, "рецепт": true, "порция": true, "порции": true, "порций": true,
	"порцию": true, "персоны": true, "персон": true, "человек": true, "человека": true,
	"одна": true, "одну": true, "две": true, "два": true, "три": true, "четыре": true, "пять": true,
	"шесть": true, "семь": true, "восемь": true, "примерно": true, "около": true,
}

func servingsOnly(ln line) bool {
	if servingsIn(ln.low) == 0 {
		return false
	}
	for _, w := range ln.words() {
		if !servingsWords[w] {
			return false
		}
	}
	return true
}

// hasVerb reports an instruction verb in the line.
func hasVerb(ln line) bool {
	for _, w := range ln.words() {
		if isVerb(w) {
			return true
		}
	}
	return false
}

// headingTitle accepts a short heading line: a few words, no sentence
// punctuation.
func headingTitle(ln line) string {
	if len(ln.words()) > 6 || strings.ContainsAny(ln.text, ",!?:;") || len(ln.segments) == 0 {
		return ""
	}
	return titleFrom(ln.segments[0])
}

// bulletList finds an ingredient list without a header and without
// amounts before the steps: three or more lines with the same bullet, each
// a short food name («•филе куриное», «•картофель», «•сыр»).
func bulletList(pre []line) []item {
	var run, best []item
	runLines, bestLines := 0, 0
	bullet := ""
	flush := func() {
		if runLines > bestLines {
			best, bestLines = run, runLines
		}
		run, runLines, bullet = nil, 0, ""
	}
	for _, ln := range pre {
		items := parseLine(ln.text)
		ok := ln.bullet != "" && len(items) > 0 && !ln.narrative() && !hasVerb(ln)
		for _, it := range items {
			if it.q.hasQuantity() || !validName(it.name) || len(strings.Fields(it.name)) > 4 {
				ok = false
			}
		}
		if !ok || (bullet != "" && ln.bullet != bullet) {
			flush()
		}
		if ok {
			bullet = ln.bullet
			run = append(run, items...)
			runLines++
		}
	}
	flush()
	if bestLines < 3 {
		return nil
	}
	return best
}
