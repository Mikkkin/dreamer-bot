package recipeimport

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// isEmoji reports whether r is a pictograph, a symbol used as one, or a
// part of an emoji sequence (variation selector, joiner, keycap, tag).
// Captions use them as bullets, decoration and section markers.
func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF, // pictographs, emoticons, flags, skin tones
		r >= 0x2600 && r <= 0x27BF,   // miscellaneous symbols and dingbats (✨ ❗ ✔ ✅ ❤)
		r >= 0x2B00 && r <= 0x2BFF,   // arrows and stars (⬇ ⭐)
		r >= 0x2900 && r <= 0x297F,   // supplemental arrows (⤵)
		r >= 0x2190 && r <= 0x21FF,   // arrows
		r >= 0x2300 && r <= 0x23FF,   // technical symbols (⌚ ⏰)
		r >= 0x25A0 && r <= 0x25FF,   // geometric shapes (▪ ▶)
		r >= 0xE0020 && r <= 0xE007F, // tag sequences
		r >= 0xFE00 && r <= 0xFE0F,   // variation selectors
		r == 0x200D, r == 0x20E3, r == 0x3030, r == 0x303D, r == 0x3297, r == 0x3299,
		r == 0x00A9, r == 0x00AE, r == 0x2122, r == 0x2139, r == 0x2049, r == 0x203C:
		return true
	}
	return false
}

// normalizeInvisible removes zero-width characters and turns exotic spaces
// (TAB, NBSP, the Braille blank U+2800 that Instagram users put on "empty"
// lines) into plain spaces. «и» followed by U+200C is a broken «й» from
// some keyboards and is repaired.
func normalizeInvisible(s string) string {
	s = strings.NewReplacer("и\u200c", "й", "И\u200c", "Й", "\r\n", "\n", "\r", "\n").Replace(s)
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u2060', '\ufeff', '\u00ad':
			return -1
		case '\t', '\u00a0', '\u2800', '\u202f', '\u2007', '\u2009', '\u200a', '\u3000':
			return ' '
		}
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// lowerKeep lower-cases s and folds ё into е without changing any byte
// offset, so that positions found in the result index the original too.
func lowerKeep(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		l := unicode.ToLower(r)
		if l == 'ё' {
			l = 'е'
		}
		if utf8.RuneLen(l) != utf8.RuneLen(r) {
			l = r
		}
		b.WriteRune(l)
	}
	return b.String()
}

// collapseSpaces trims s and squeezes every run of white space into one
// space.
func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// stripEmoji replaces every emoji with a space.
func stripEmoji(s string) string {
	return strings.Map(func(r rune) rune {
		if isEmoji(r) {
			return ' '
		}
		return r
	}, s)
}

// letterWords splits s into words of letters (a hyphen inside a word is
// kept: «по-корейски»), lower-cased with ё folded.
func letterWords(s string) []string {
	fields := strings.FieldsFunc(lowerKeep(s), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '-'
	})
	words := fields[:0]
	for _, f := range fields {
		if f = strings.Trim(f, "-"); f != "" {
			words = append(words, f)
		}
	}
	return words
}

// capitalize upper-cases the first letter of s.
func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError || !unicode.IsLower(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// isAllCaps reports whether s has at least min letters and no lower-case
// ones.
func isAllCaps(s string, min int) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= min
}

// sentenceCase turns an ALL CAPS phrase into «Sentence case».
func sentenceCase(s string) string { return capitalize(strings.ToLower(s)) }

// truncateRunes cuts s to at most n runes, at the last space when there is
// one in the second half.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)[:n]
	cut := string(runes)
	if i := strings.LastIndexByte(cut, ' '); i > len(cut)/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-–—")
}

// hasCyrillic reports whether s contains a Cyrillic letter.
func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// foreignLetters reports whether s has at least three letters and none of
// them Cyrillic: a Greek or English half that repeats the recipe.
func foreignLetters(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return false
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 3
}

// blankParens replaces every parenthesised part of s (nested parentheses
// included; an unclosed one runs to the end) with spaces of the same byte
// length, and returns the contents. Commas and dashes inside parentheses
// then never split or separate anything.
func blankParens(s string) (string, []string) {
	b := []byte(s)
	var inside []string
	depth, start := 0, 0
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '(':
			if depth == 0 {
				start = i
			}
			depth++
		case ')':
			if depth == 0 {
				b[i] = ' '
				continue
			}
			depth--
			if depth == 0 {
				inside = append(inside, string(b[start+1:i]))
				for j := start; j <= i; j++ {
					b[j] = ' '
				}
			}
		}
	}
	if depth > 0 {
		inside = append(inside, string(b[start+1:]))
		for j := start; j < len(b); j++ {
			b[j] = ' '
		}
	}
	return string(b), inside
}

// trimPunct trims spaces and list punctuation from both ends of s.
func trimPunct(s string) string {
	return strings.Trim(s, " \t,.;:!-–—~•·*+")
}

// pluralRu picks the Russian form for n: one («строка»), few («строки») or
// many («строк»).
func pluralRu(n int, one, few, many string) string {
	switch last, lastTwo := n%10, n%100; {
	case last == 1 && lastTwo != 11:
		return one
	case last >= 2 && last <= 4 && (lastTwo < 12 || lastTwo > 14):
		return few
	default:
		return many
	}
}
