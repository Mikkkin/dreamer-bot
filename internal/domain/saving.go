package domain

import (
	"time"
	"unicode/utf8"
)

// SavingID identifies one contribution to a wish's savings.
type SavingID int64

// MaxSavingNoteLen caps the note of a contribution.
const MaxSavingNoteLen = 140

// MaxSavingsPerWish caps the contributions to one wish.
const MaxSavingsPerWish = 200

// Saving is money put aside for a wish («Копим»): who added how much, when.
// Savings of one wish are always in one currency — the currency of its price
// when it has one.
type Saving struct {
	ID        SavingID
	WishID    WishID
	Amount    Money
	UserID    UserID
	Note      string
	CreatedAt time.Time
}

// NewSaving validates a contribution for wish w: the wish has not come true
// yet, the amount is positive and in the wish's savings currency (see
// Wish.SavingsCurrency), and the note is short.
func NewSaving(w Wish, amount Money, user UserID, note string, now time.Time) (Saving, error) {
	if w.Status == StatusDone {
		return Saving{}, invalid("status", "мечта уже сбылась — копить на неё не нужно")
	}
	m, err := NewMoney(amount.Minor, amount.Currency)
	if err != nil {
		// NewMoney reports on "price"; for a saving the input field is "amount".
		if v, ok := AsValidation(err); ok && v.Field == "price" {
			return Saving{}, invalid("amount", v.Message)
		}
		return Saving{}, err
	}
	if c, ok := w.SavingsCurrency(); ok && c != m.Currency {
		return Saving{}, invalid("currency", "копим в "+string(c)+" — укажите сумму в этой валюте")
	}
	n := cleanText(note, false)
	if utf8.RuneCountInString(n) > MaxSavingNoteLen {
		return Saving{}, invalid("note", "заметка слишком длинная (максимум 140 символов)")
	}
	return Saving{WishID: w.ID, Amount: m, UserID: user, Note: n, CreatedAt: now.UTC()}, nil
}

// SavingsCurrency is the currency new savings must use: the price currency,
// otherwise the currency of what is already saved. ok is false when neither
// exists yet (any supported currency is accepted then).
func (w Wish) SavingsCurrency() (Currency, bool) {
	if w.Price != nil {
		return w.Price.Currency, true
	}
	if w.Saved != nil {
		return w.Saved.Currency, true
	}
	return "", false
}

// SavedPercent is how much of the price is saved, 0..100 (capped); ok is
// false without both a price and savings.
func (w Wish) SavedPercent() (int, bool) {
	if w.Price == nil || w.Saved == nil || w.Price.Currency != w.Saved.Currency {
		return 0, false
	}
	p := int(w.Saved.Minor * 100 / w.Price.Minor)
	return min(p, 100), true
}
