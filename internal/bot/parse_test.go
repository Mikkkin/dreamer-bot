package bot

import (
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// entityFor builds a Telegram entity for the first occurrence of sub in
// text, with offsets in UTF-16 units like the real API.
func entityFor(t *testing.T, typ models.MessageEntityType, text, sub, url string) models.MessageEntity {
	t.Helper()
	i := strings.Index(text, sub)
	if i < 0 {
		t.Fatalf("%q not in %q", sub, text)
	}
	return models.MessageEntity{
		Type:   typ,
		Offset: len(utf16.Encode([]rune(text[:i]))),
		Length: len(utf16.Encode([]rune(sub))),
		URL:    url,
	}
}

func money(minor int64, c domain.Currency) *domain.Money {
	return &domain.Money{Minor: minor, Currency: c}
}

func TestParseInputPrices(t *testing.T) {
	tests := []struct {
		in    string
		title string
		text  string
		price *domain.Money
	}{
		{"Поездка в Токио 1200€", "Поездка в Токио", "", money(120000, "EUR")},
		{"Поездка в Токио €1200", "Поездка в Токио", "", money(120000, "EUR")},
		{"Диван\n15 000 ₽\nсерый, в гостиную", "Диван", "серый, в гостиную", money(1500000, "RUB")},
		{"Диван 45 000 ₽", "Диван", "", money(4500000, "RUB")},
		{"Кроссовки 15000 руб", "Кроссовки", "", money(1500000, "RUB")},
		{"Кроссовки 15000 рублей", "Кроссовки", "", money(1500000, "RUB")},
		{"Плед 2500 р.", "Плед", "", money(250000, "RUB")},
		{"Сыр 99.90 eur", "Сыр", "", money(9990, "EUR")},
		{"Книга $50", "Книга", "", money(5000, "USD")},
		{"Книга 50 USD", "Книга", "", money(5000, "USD")},
		{"Чай 12 фунтов", "Чай", "", money(1200, "GBP")},
		{"€1 200,50 отель у моря", "отель у моря", "", money(120050, "EUR")},
		{"Отель — 1 200 €", "Отель", "", money(120000, "EUR")},
		{"Ноутбук 1,299.99 USD", "Ноутбук", "", money(129999, "USD")},
		{"Ноутбук 1.299,99 €", "Ноутбук", "", money(129999, "EUR")},
		{"Кроссовки за 150€", "Кроссовки", "", money(15000, "EUR")},
		{"Кроссовки Nike\nЦена: 120 EUR\nбелые", "Кроссовки Nike", "белые", money(12000, "EUR")},
		// A model number right before a price must not merge into it.
		{"Air Max 90 150€", "Air Max 90", "", money(15000, "EUR")},
		{"iPhone 17 999€", "iPhone 17", "", money(99900, "EUR")},
		// Numbers without a currency marker are never prices.
		{"iPhone 17", "iPhone 17", "", nil},
		{"Подарок на 1000", "Подарок на 1000", "", nil},
		{"Встреча 27.09.2026 в 19:00", "Встреча 27.09.2026 в 19:00", "", nil},
		{"5 раз в неделю", "5 раз в неделю", "", nil},
		{"Билеты 1200 europe", "Билеты 1200 europe", "", nil},
		// Zero is not a price.
		{"Бесплатно 0€", "Бесплатно 0€", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			p := parseInput(tt.in, nil)
			if p.Title != tt.title || p.Text != tt.text {
				t.Errorf("title/text = %q / %q, want %q / %q", p.Title, p.Text, tt.title, tt.text)
			}
			switch {
			case tt.price == nil && p.Price != nil:
				t.Errorf("price = %+v, want none", *p.Price)
			case tt.price != nil && (p.Price == nil || *p.Price != *tt.price):
				t.Errorf("price = %v, want %+v", p.Price, *tt.price)
			}
		})
	}
}

func TestParseInputLinks(t *testing.T) {
	t.Run("url entity is removed from the title", func(t *testing.T) {
		text := "Смотри https://shop.example/item?id=5 классная лампа"
		p := parseInput(text, []models.MessageEntity{
			entityFor(t, models.MessageEntityTypeURL, text, "https://shop.example/item?id=5", ""),
		})
		if p.Link == nil || *p.Link != "https://shop.example/item?id=5" {
			t.Fatalf("link = %v", p.Link)
		}
		if p.Title != "Смотри классная лампа" {
			t.Errorf("title = %q", p.Title)
		}
	})
	t.Run("entity offsets are UTF-16", func(t *testing.T) {
		text := "🔥🔥 Лампа ozon.ru/item/42"
		p := parseInput(text, []models.MessageEntity{
			entityFor(t, models.MessageEntityTypeURL, text, "ozon.ru/item/42", ""),
		})
		if p.Link == nil || *p.Link != "https://ozon.ru/item/42" {
			t.Fatalf("link = %v", p.Link)
		}
		if p.Title != "🔥🔥 Лампа" {
			t.Errorf("title = %q", p.Title)
		}
	})
	t.Run("text_link keeps the anchor text", func(t *testing.T) {
		text := "Кроссовки мечты"
		p := parseInput(text, []models.MessageEntity{
			entityFor(t, models.MessageEntityTypeTextLink, text, "Кроссовки", "https://shop.example/x"),
		})
		if p.Link == nil || *p.Link != "https://shop.example/x" || p.Title != text {
			t.Errorf("got link %v title %q", p.Link, p.Title)
		}
	})
	t.Run("unsafe text_link is ignored", func(t *testing.T) {
		text := "Нажми"
		for _, bad := range []string{"javascript:alert(1)", "tg://user?id=1", "https://user:pw@evil.example/"} {
			p := parseInput(text, []models.MessageEntity{
				entityFor(t, models.MessageEntityTypeTextLink, text, "Нажми", bad),
			})
			if p.Link != nil {
				t.Errorf("%q accepted as %q", bad, *p.Link)
			}
		}
	})
	t.Run("plain URL without entities, trailing punctuation dropped", func(t *testing.T) {
		p := parseInput("Лампа https://ikea.example/lamp.", nil)
		if p.Link == nil || *p.Link != "https://ikea.example/lamp" || p.Title != "Лампа" {
			t.Errorf("got link %v title %q", p.Link, p.Title)
		}
	})
	t.Run("digits in the URL are not a price", func(t *testing.T) {
		text := "Часы https://shop.example/1200€/item"
		p := parseInput(text, nil)
		if p.Price != nil {
			t.Errorf("price = %+v from inside the URL", *p.Price)
		}
	})
	t.Run("link and price together", func(t *testing.T) {
		text := "Отель https://hotel.example 250 € за ночь"
		p := parseInput(text, []models.MessageEntity{
			entityFor(t, models.MessageEntityTypeURL, text, "https://hotel.example", ""),
		})
		if p.Link == nil || p.Price == nil || p.Price.Minor != 25000 || p.Title != "Отель за ночь" {
			t.Errorf("got link %v price %v title %q", p.Link, p.Price, p.Title)
		}
	})
	t.Run("only a URL leaves the title empty", func(t *testing.T) {
		p := parseInput("https://example.com/x", nil)
		if p.Title != "" || p.Link == nil {
			t.Errorf("got title %q link %v", p.Title, p.Link)
		}
	})
}

func TestParseInputTitles(t *testing.T) {
	t.Run("first non-empty line is the title, the rest the text", func(t *testing.T) {
		p := parseInput("\n\n  Паста карбонара  \n1. Сварить пасту\n\n\n\n2. Смешать\n", nil)
		if p.Title != "Паста карбонара" {
			t.Errorf("title = %q", p.Title)
		}
		if p.Text != "1. Сварить пасту\n\n2. Смешать" {
			t.Errorf("text = %q", p.Text)
		}
	})
	t.Run("list bullets in the text are kept", func(t *testing.T) {
		p := parseInput("Блины\n- мука\n- молоко", nil)
		if p.Text != "- мука\n- молоко" {
			t.Errorf("text = %q", p.Text)
		}
	})
	t.Run("long title overflows into the text at a word", func(t *testing.T) {
		long := strings.Repeat("слово ", 40) // 240 runes
		p := parseInput(long+"\nзаметка", nil)
		if n := utf8.RuneCountInString(p.Title); n > domain.MaxTitleLen || n < domain.MaxTitleLen/2 {
			t.Fatalf("title has %d runes", n)
		}
		if strings.HasSuffix(p.Title, " ") || strings.HasPrefix(p.Text, " ") {
			t.Errorf("split mid-space: %q | %q", p.Title, p.Text)
		}
		if !strings.HasSuffix(p.Text, "\nзаметка") {
			t.Errorf("text = %q", p.Text)
		}
		joined := strings.Join(strings.Fields(p.Title+" "+p.Text), " ")
		if joined != strings.TrimSpace(long)+" заметка" {
			t.Error("title overflow lost content")
		}
	})
	t.Run("long title without spaces is cut hard", func(t *testing.T) {
		long := strings.Repeat("я", 150)
		p := parseInput(long, nil)
		if utf8.RuneCountInString(p.Title) != domain.MaxTitleLen || utf8.RuneCountInString(p.Text) != 30 {
			t.Errorf("title %d runes, text %d runes", utf8.RuneCountInString(p.Title), utf8.RuneCountInString(p.Text))
		}
	})
	t.Run("empty input", func(t *testing.T) {
		if p := parseInput("  \n ", nil); p != (parsedInput{}) {
			t.Errorf("got %+v", p)
		}
	})
}

func TestParseMoneyInput(t *testing.T) {
	tests := []struct {
		in       string
		fallback domain.Currency
		want     domain.Money
		wantErr  bool
	}{
		{"1200", "EUR", domain.Money{Minor: 120000, Currency: "EUR"}, false},
		{"15 000 ₽", "EUR", domain.Money{Minor: 1500000, Currency: "RUB"}, false},
		{"15 000", "RUB", domain.Money{Minor: 1500000, Currency: "RUB"}, false},
		{"€1 200,50", "USD", domain.Money{Minor: 120050, Currency: "EUR"}, false},
		{"99,9", "GBP", domain.Money{Minor: 9990, Currency: "GBP"}, false},
		{"abc", "EUR", domain.Money{}, true},
		{"0", "EUR", domain.Money{}, true},
		{"-5", "EUR", domain.Money{}, true},
	}
	for _, tt := range tests {
		got, err := parseMoneyInput(tt.in, tt.fallback)
		if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
			t.Errorf("parseMoneyInput(%q) = %+v, %v; want %+v, err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
		if err != nil {
			if _, ok := domain.AsValidation(err); !ok {
				t.Errorf("parseMoneyInput(%q) error %v is not a validation error", tt.in, err)
			}
		}
	}
}

func TestUTF16Span(t *testing.T) {
	s := "a🔥b"
	tests := []struct {
		off, n     int
		want       string
		wantOK     bool
		name       string
		wantString string
	}{
		{off: 0, n: 1, want: "a", wantOK: true, name: "ascii"},
		{off: 1, n: 2, want: "🔥", wantOK: true, name: "surrogate pair"},
		{off: 3, n: 1, want: "b", wantOK: true, name: "after pair"},
		{off: 2, n: 1, wantOK: false, name: "inside pair"},
		{off: 0, n: 10, wantOK: false, name: "past the end"},
		{off: -1, n: 1, wantOK: false, name: "negative"},
		{off: 0, n: 0, wantOK: false, name: "empty"},
	}
	for _, tt := range tests {
		start, end, ok := utf16Span(s, tt.off, tt.n)
		if ok != tt.wantOK || (ok && s[start:end] != tt.want) {
			t.Errorf("%s: got %d..%d ok=%v", tt.name, start, end, ok)
		}
	}
}
