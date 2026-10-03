package domain

import "testing"

// Text from strangers (an imported caption, a partner's note) must not
// carry invisible characters that reorder or hide what is shown around it.
func TestCleanTextStripsFormatCharacters(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"right-to-left override", "Пирог \u202eтеркцорп\u202c с яблоками", "Пирог теркцорп с яблоками"},
		{"bidi marks", "\u200fБорщ\u200e", "Борщ"},
		{"embeddings", "\u202aСуп\u202b и \u202dщи\u202c", "Суп и щи"},
		{"isolates", "Каша \u2066овсяная\u2069 \u2067и\u2068", "Каша овсяная и"},
		{"arabic letter mark", "Плов\u061c", "Плов"},
		{"zero width and soft hyphen", "Ка\u200bр\u00adто\u2060фель\ufeff", "Картофель"},
		{"joiner between letters", "Мо\u200dлоко", "Молоко"},
		{"joiner at the end", "Сыр \U0001F9C0\u200d", "Сыр \U0001F9C0"},
		{"joiner before an override", "\U0001F9C0\u200d\u202e", "\U0001F9C0"},
		{"tag outside a flag", "Чай\U000E0061\U000E007F", "Чай"},
		{"controls and tabs", "Соль\x00\tперец\x7f", "Соль перец"},
		// emoji sequences survive
		{"profession", "\U0001F469\u200d\U0001F4BB", "\U0001F469\u200d\U0001F4BB"},
		{"skin tone and profession", "\U0001F469\U0001F3FD\u200d\U0001F373 Шеф", "\U0001F469\U0001F3FD\u200d\U0001F373 Шеф"},
		{"family", "\U0001F468\u200d\U0001F469\u200d\U0001F467", "\U0001F468\u200d\U0001F469\u200d\U0001F467"},
		{"variation selector", "❤\ufe0f\u200d\U0001F525", "❤\ufe0f\u200d\U0001F525"},
		{"subdivision flag", "\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", "\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"},
		{"plain text", "Курица терияки", "Курица терияки"},
	}
	for _, tt := range tests {
		if got := cleanText(tt.in, false); got != tt.want {
			t.Errorf("%s: cleanText(%+q) = %+q, want %+q", tt.name, tt.in, got, tt.want)
		}
	}
	if got := cleanText("Шаг 1\u202e\nШаг 2", true); got != "Шаг 1\nШаг 2" {
		t.Errorf("newlines kept: got %+q", got)
	}
}

func TestNormalizersStripBidiControls(t *testing.T) {
	if got, err := NormalizeTitle("\u202eПирог\u202c"); err != nil || got != "Пирог" {
		t.Errorf("NormalizeTitle = %+q, %v", got, err)
	}
	if _, err := NormalizeTitle("\u202e\u2066\u200f"); err == nil {
		t.Error("a title of format characters only must be empty")
	}
	if got, err := NormalizeNote("Заметка\u2067 \u2069конец"); err != nil || got != "Заметка конец" {
		t.Errorf("NormalizeNote = %+q, %v", got, err)
	}
	if got, err := NormalizeEmoji("\U0001F469\u200d\U0001F373"); err != nil || got != "\U0001F469\u200d\U0001F373" {
		t.Errorf("NormalizeEmoji = %+q, %v; an emoji sequence must survive", got, err)
	}
}
