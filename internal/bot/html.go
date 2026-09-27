package bot

import (
	"html"
	"net/url"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// htmlText builds Telegram HTML within a budget of visible characters.
// Every piece of text is escaped here and nowhere else, so user input can
// never inject markup, and truncation never cuts through a tag or a rune.
// The budget is counted in UTF-16 code units, which is how Telegram measures
// message and caption length.
type htmlText struct {
	b    strings.Builder
	left int
	cut  bool
}

func newHTML(limit int) *htmlText { return &htmlText{left: limit} }

// Text appends plain text.
func (h *htmlText) Text(s string) *htmlText { return h.wrap("", "", s) }

// Bold appends bold text.
func (h *htmlText) Bold(s string) *htmlText { return h.wrap("<b>", "</b>", s) }

// Italic appends italic text.
func (h *htmlText) Italic(s string) *htmlText { return h.wrap("<i>", "</i>", s) }

// Code appends monospace text.
func (h *htmlText) Code(s string) *htmlText { return h.wrap("<code>", "</code>", s) }

// Link appends a hyperlink for a validated http(s) URL. Anything else is
// rendered as plain label text without a link.
func (h *htmlText) Link(href, label string) *htmlText {
	if !isWebURL(href) {
		return h.Text(label)
	}
	return h.wrap(`<a href="`+html.EscapeString(href)+`">`, "</a>", label)
}

// NL appends a line break.
func (h *htmlText) NL() *htmlText { return h.Text("\n") }

// Truncated reports whether some content did not fit.
func (h *htmlText) Truncated() bool { return h.cut }

func (h *htmlText) String() string { return h.b.String() }

func (h *htmlText) wrap(open, closing, s string) *htmlText {
	if s == "" || h.cut {
		return h
	}
	fit, whole := clip(s, h.left)
	if fit != "" {
		h.b.WriteString(open)
		h.b.WriteString(html.EscapeString(fit))
		h.b.WriteString(closing)
		h.left -= utf16Len(fit)
	}
	// Once something is cut, later pieces are dropped: a caption that ends
	// in "…" followed by more lines would read as garbled.
	h.cut = !whole
	return h
}

// clip returns the longest prefix of s that fits into limit UTF-16 units.
// When s does not fit, the prefix ends with "…" (counted in the budget).
func clip(s string, limit int) (string, bool) {
	if utf16Len(s) <= limit {
		return s, true
	}
	if limit <= 0 {
		return "", false
	}
	budget := limit - 1 // room for the ellipsis
	var b strings.Builder
	for _, r := range s {
		n := utf16.RuneLen(r)
		if n > budget {
			break
		}
		budget -= n
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " \n") + "…", false
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// excerpt shortens s to at most limit runes, adding "…" when it was cut.
func excerpt(s string, limit int) (string, bool) {
	if utf8.RuneCountInString(s) <= limit {
		return s, false
	}
	runes := []rune(s)
	return strings.TrimRight(string(runes[:limit-1]), " \n") + "…", true
}

// isWebURL reports whether s is an absolute http(s) URL with a host and no
// credentials. It re-checks stored links before they become clickable.
func isWebURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.User != nil || u.Hostname() == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// linkLabel is the short visible text of a link: its host without "www.".
func linkLabel(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return "ссылка"
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
