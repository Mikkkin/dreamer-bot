package bot

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// parsedInput is what the quick-add parser extracted from a message.
type parsedInput struct {
	Title string
	// Text holds the remaining lines: the note of a wish or the body of a
	// recipe.
	Text  string
	Link  *string
	Price *domain.Money
}

// span is a half-open byte range of the message text.
type span struct{ start, end int }

// cutMark replaces removed fragments until the lines are tidied up. NUL
// never survives domain text normalisation, so it cannot collide with
// meaningful user content.
const cutMark = "\x00"

// parseInput turns a free-form message into draft fields. A link comes from
// the message entities (or a plain http(s) URL), a price only from an amount
// with an explicit currency marker, so "iPhone 17" never becomes a price.
// The first remaining line is the title; the rest is the note or the body.
func parseInput(text string, entities []models.MessageEntity) parsedInput {
	var (
		p    parsedInput
		cuts []span
	)
	if link, sp, ok := findLink(text, entities); ok {
		p.Link = &link
		if sp.end > sp.start {
			cuts = append(cuts, sp)
		}
	}
	// Digits inside the URL must not be mistaken for a price, so the price
	// is searched in a copy with the URL blanked out (same byte offsets).
	if m, sp, ok := findPrice(blank(text, cuts)); ok {
		p.Price = &m
		cuts = append(cuts, sp)
	}
	p.Title, p.Text = splitTitle(cutLines(text, cuts))
	return p
}

// urlPattern is the fallback for text without entities (Telegram normally
// marks URLs itself).
var urlPattern = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"'«»]+`)

// findLink returns the first valid http(s) link and the span of its visible
// text. A text_link keeps its anchor text, so its span is empty.
func findLink(text string, entities []models.MessageEntity) (string, span, bool) {
	for _, e := range entities {
		switch e.Type {
		case models.MessageEntityTypeURL:
			start, end, ok := utf16Span(text, e.Offset, e.Length)
			if !ok {
				continue
			}
			if link, err := domain.NormalizeLink(text[start:end]); err == nil {
				return link, span{start, end}, true
			}
		case models.MessageEntityTypeTextLink:
			if link, err := domain.NormalizeLink(e.URL); err == nil {
				return link, span{}, true
			}
		}
	}
	for _, loc := range urlPattern.FindAllStringIndex(text, -1) {
		end := trimURLTail(text[loc[0]:loc[1]]) + loc[0]
		if link, err := domain.NormalizeLink(text[loc[0]:end]); err == nil {
			return link, span{loc[0], end}, true
		}
	}
	return "", span{}, false
}

// trimURLTail drops sentence punctuation glued to the end of a URL and
// returns the new length.
func trimURLTail(u string) int {
	for u != "" {
		r, size := utf8.DecodeLastRuneInString(u)
		switch {
		case strings.ContainsRune(".,;:!?'\"…", r):
		case r == ')' && !strings.Contains(u, "("):
		default:
			return len(u)
		}
		u = u[:len(u)-size]
	}
	return 0
}

// utf16Span converts a Telegram entity (offsets in UTF-16 code units) into a
// byte range of s.
func utf16Span(s string, offset, length int) (int, int, bool) {
	if offset < 0 || length <= 0 {
		return 0, 0, false
	}
	start, end, units := -1, -1, 0
	for i, r := range s {
		if units == offset {
			start = i
		}
		if units == offset+length {
			end = i
			break
		}
		units += utf16.RuneLen(r)
	}
	if end < 0 && units == offset+length {
		end = len(s)
	}
	if start < 0 || end < start {
		return 0, 0, false
	}
	return start, end, true
}

// Price patterns. An amount is either grouped by spaces ("15 000", "1 200,50")
// or plain ("1200", "1,200.50", "99.90"). Space grouping is ambiguous after
// a model number ("Air Max 90 150€"), so it is only accepted right after a
// currency sign, at the start of a line or after punctuation, or with
// roubles, where "12 990 ₽" is the everyday notation.
const (
	amountSpaced = `\d{1,3}(?:[ \x{00A0}\x{202F}\x{2009}]\d{3})+(?:[.,]\d{1,2})?`
	amountPlain  = `\d{1,3}(?:[.,']\d{3})+(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?`
	gap          = `[ \x{00A0}\x{202F}]?`
	curBefore    = `€|\$|£|₽|eur|usd|rub|gbp`
	curAfter     = `€|\$|£|₽|euros?|eur|евро|usd|dollars?|долларов|доллара|доллар|долл\.?|` +
		`rub|рублей|рубля|рубль|руб\.?|р\.?|gbp|pounds?|фунтов|фунта|фунт`
	curRouble = `₽|rub|рублей|рубля|рубль|руб\.?|р\.?`
	edgeAfter = `(?:[^\p{L}\p{N}]|$)`
)

var pricePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?im)(?:^|[^\p{L}\p{N}])(?P<price>(?P<cur>` + curBefore + `)` + gap +
		`(?P<amount>` + amountSpaced + `|` + amountPlain + `))` + edgeAfter),
	regexp.MustCompile(`(?im)(?:^|[^\p{L}\p{N}\s.,])[ \t]*(?P<price>(?P<amount>` + amountSpaced + `)` + gap +
		`(?P<cur>` + curAfter + `))` + edgeAfter),
	regexp.MustCompile(`(?im)(?:^|[^\p{L}\p{N}.,])(?P<price>(?P<amount>` + amountSpaced + `)` + gap +
		`(?P<cur>` + curRouble + `))` + edgeAfter),
	regexp.MustCompile(`(?im)(?:^|[^\p{L}\p{N}.,])(?P<price>(?P<amount>` + amountPlain + `)` + gap +
		`(?P<cur>` + curAfter + `))` + edgeAfter),
}

// findPrice returns the earliest amount with a currency marker in s.
func findPrice(s string) (domain.Money, span, bool) {
	var (
		best  domain.Money
		where = span{start: -1}
	)
	for _, re := range pricePatterns {
		iPrice, iAmount, iCur := re.SubexpIndex("price"), re.SubexpIndex("amount"), re.SubexpIndex("cur")
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			start, end := m[2*iPrice], m[2*iPrice+1]
			if where.start >= 0 && (start > where.start || (start == where.start && end <= where.end)) {
				continue
			}
			cur, ok := currencyOf(s[m[2*iCur]:m[2*iCur+1]])
			if !ok {
				continue
			}
			money, err := domain.ParseAmount(s[m[2*iAmount]:m[2*iAmount+1]], cur)
			if err != nil {
				continue
			}
			best, where = money, span{start, end}
		}
	}
	return best, where, where.start >= 0
}

// currencyOf maps a currency sign or word to its ISO code.
func currencyOf(token string) (domain.Currency, bool) {
	t := strings.TrimSuffix(strings.ToLower(token), ".")
	switch {
	case t == "€" || strings.HasPrefix(t, "eur") || t == "евро":
		return "EUR", true
	case t == "$" || t == "usd" || strings.HasPrefix(t, "dollar") || strings.HasPrefix(t, "долл"):
		return "USD", true
	case t == "₽" || t == "rub" || strings.HasPrefix(t, "руб") || t == "р":
		return "RUB", true
	case t == "£" || t == "gbp" || strings.HasPrefix(t, "pound") || strings.HasPrefix(t, "фунт"):
		return "GBP", true
	}
	return "", false
}

// parseMoneyInput parses an amount typed on request: an explicit currency
// wins, otherwise fallback is used ("1200" -> 1 200 €).
func parseMoneyInput(s string, fallback domain.Currency) (domain.Money, error) {
	if m, _, ok := findPrice(s); ok {
		return m, nil
	}
	return domain.ParseAmount(s, fallback)
}

// linkInput extracts a link typed on request.
func linkInput(s string, entities []models.MessageEntity) (string, error) {
	if link, _, ok := findLink(s, entities); ok {
		return link, nil
	}
	return domain.NormalizeLink(s)
}

// blank replaces the bytes of every span with spaces, keeping offsets.
func blank(s string, spans []span) string {
	if len(spans) == 0 {
		return s
	}
	b := []byte(s)
	for _, sp := range spans {
		for i := sp.start; i < sp.end; i++ {
			b[i] = ' '
		}
	}
	return string(b)
}

// cutLines removes the spans from s and splits it into lines. Lines that
// lost a fragment are tidied: leftover separators and dangling words such as
// «за» in «Кроссовки за 150€» are dropped.
func cutLines(s string, spans []span) []string {
	if len(spans) > 0 {
		spans = slices.Clone(spans)
		slices.SortFunc(spans, func(a, b span) int { return b.start - a.start })
		for _, sp := range spans {
			s = s[:sp.start] + cutMark + s[sp.end:]
		}
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.Contains(line, cutMark) {
			lines[i] = tidyCutLine(line)
		}
	}
	return lines
}

var danglingWords = []string{"за", "около", "примерно", "цена", "стоимость", "price", "for", "~", "≈"}

func tidyCutLine(line string) string {
	line = strings.Join(strings.Fields(strings.ReplaceAll(line, cutMark, " ")), " ")
	for {
		next := strings.TrimFunc(line, isFiller)
		for _, w := range danglingWords {
			lower := strings.ToLower(next)
			if lower == w {
				next = ""
			} else if strings.HasSuffix(lower, " "+w) {
				next = next[:len(next)-len(w)-1]
			}
		}
		if next == line {
			return line
		}
		line = next
	}
}

func isFiller(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("-—–:;,.!?…|·•/", r)
}

// splitTitle takes the first non-empty line as the title and the rest as
// the text. A title over the limit is cut at a word boundary and the
// overflow moves to the start of the text, so nothing is lost.
func splitTitle(lines []string) (string, string) {
	first := slices.IndexFunc(lines, func(l string) bool { return strings.TrimSpace(l) != "" })
	if first < 0 {
		return "", ""
	}
	title := strings.TrimSpace(lines[first])
	rest := collapseBlankLines(strings.TrimSpace(strings.Join(lines[first+1:], "\n")))
	head, tail := splitAtWord(title, domain.MaxTitleLen)
	return head, joinLines(tail, rest)
}

var blankLines = regexp.MustCompile(`\n[ \t]*\n(?:[ \t]*\n)+`)

func collapseBlankLines(s string) string { return blankLines.ReplaceAllString(s, "\n\n") }

// splitAtWord cuts s to at most limit runes, preferring the last space in
// the second half of the allowed length.
func splitAtWord(s string, limit int) (string, string) {
	runes := []rune(s)
	if len(runes) <= limit {
		return s, ""
	}
	cut := limit
	for i := limit; i >= limit/2; i-- {
		if unicode.IsSpace(runes[i]) {
			cut = i
			break
		}
	}
	return strings.TrimSpace(string(runes[:cut])), strings.TrimSpace(string(runes[cut:]))
}
