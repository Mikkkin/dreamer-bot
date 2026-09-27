package domain

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Field limits shared by the bot, the API and the Mini App.
const (
	MaxTitleLen        = 120
	MaxNoteLen         = 2000
	MaxLinkLen         = 2048
	MaxCategoryNameLen = 32
	MaxEmojiLen        = 16
)

// cleanText trims the string and removes control characters except newlines
// (when allowed). It never changes printable content.
func cleanText(raw string, keepNewlines bool) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && keepNewlines:
			return r
		case r == '\t':
			return ' '
		case unicode.IsControl(r), r == utf8.RuneError:
			return -1
		default:
			return r
		}
	}, raw)
	return strings.TrimSpace(s)
}

// NormalizeTitle validates a wish title.
func NormalizeTitle(raw string) (string, error) {
	s := cleanText(raw, false)
	switch n := utf8.RuneCountInString(s); {
	case n == 0:
		return "", invalid("title", "название обязательно")
	case n > MaxTitleLen:
		return "", invalid("title", "название слишком длинное (максимум 120 символов)")
	}
	return s, nil
}

// NormalizeNote validates an optional free-form note. Empty is allowed.
func NormalizeNote(raw string) (string, error) {
	s := cleanText(raw, true)
	if utf8.RuneCountInString(s) > MaxNoteLen {
		return "", invalid("note", "заметка слишком длинная (максимум 2000 символов)")
	}
	return s, nil
}

// NormalizeLink validates a user-supplied link. Only absolute http(s) URLs
// with a host and without embedded credentials are accepted; a bare domain
// such as "ozon.ru/item" is upgraded to https. The link is stored and shown,
// but the server never fetches it.
func NormalizeLink(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", invalid("link", "ссылка пустая")
	}
	if len(s) > MaxLinkLen {
		return "", invalid("link", "ссылка слишком длинная")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", invalid("link", "это не похоже на ссылку")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", invalid("link", "поддерживаются только ссылки http и https")
	}
	if u.Hostname() == "" || strings.ContainsAny(u.Host, " \t") {
		return "", invalid("link", "в ссылке нет адреса сайта")
	}
	if u.User != nil {
		return "", invalid("link", "ссылки с логином и паролем не поддерживаются")
	}
	u.Scheme = scheme
	out := u.String()
	if len(out) > MaxLinkLen {
		return "", invalid("link", "ссылка слишком длинная")
	}
	return out, nil
}

// NormalizeCategoryName validates a category name.
func NormalizeCategoryName(raw string) (string, error) {
	s := cleanText(raw, false)
	switch n := utf8.RuneCountInString(s); {
	case n == 0:
		return "", invalid("name", "название категории обязательно")
	case n > MaxCategoryNameLen:
		return "", invalid("name", "название категории слишком длинное (максимум 32 символа)")
	}
	return s, nil
}

// NormalizeEmoji validates the icon of a category: a short, non-empty token
// without spaces, letters or digits.
func NormalizeEmoji(raw string) (string, error) {
	s := cleanText(raw, false)
	if s == "" {
		return "", invalid("emoji", "выберите эмодзи")
	}
	if utf8.RuneCountInString(s) > MaxEmojiLen {
		return "", invalid("emoji", "эмодзи слишком длинный")
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return "", invalid("emoji", "используйте один эмодзи")
		}
	}
	return s, nil
}
