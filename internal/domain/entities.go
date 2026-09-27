package domain

import (
	"strings"
	"time"
)

// Category groups wishes, e.g. «✈️ Путешествия».
type Category struct {
	ID        CategoryID
	Name      string
	Emoji     string
	Position  int
	CreatedAt time.Time
}

// NewCategory validates and builds a category.
func NewCategory(name, emoji string, now time.Time) (Category, error) {
	c := Category{CreatedAt: now.UTC()}
	if err := c.Rename(name, emoji); err != nil {
		return Category{}, err
	}
	return c, nil
}

// Rename validates and sets the name and the emoji.
func (c *Category) Rename(name, emoji string) error {
	n, err := NormalizeCategoryName(name)
	if err != nil {
		return err
	}
	e, err := NormalizeEmoji(emoji)
	if err != nil {
		return err
	}
	c.Name, c.Emoji = n, e
	return nil
}

// Label is the emoji and the name, e.g. "✈️ Путешествия".
func (c Category) Label() string { return c.Emoji + " " + c.Name }

// CategoryPatch is a partial update; only fields with Set=true change.
type CategoryPatch struct {
	Name  Optional[string]
	Emoji Optional[string]
}

// Apply merges the patch, validates the result and only then mutates c.
func (c *Category) Apply(p CategoryPatch) error {
	name, emoji := c.Name, c.Emoji
	if p.Name.Set {
		name = p.Name.Value
	}
	if p.Emoji.Set {
		emoji = p.Emoji.Value
	}
	next := *c
	if err := next.Rename(name, emoji); err != nil {
		return err
	}
	*c = next
	return nil
}

// DefaultCategories are seeded on the first start.
func DefaultCategories() []Category {
	seed := []struct{ emoji, name string }{
		{"🛍", "Покупки"},
		{"✈️", "Путешествия"},
		{"🎉", "Впечатления"},
		{"🍽", "Рестораны"},
		{"🏠", "Для дома"},
		{"🎁", "Подарки"},
	}
	out := make([]Category, len(seed))
	for i, s := range seed {
		out[i] = Category{Name: s.name, Emoji: s.emoji, Position: i}
	}
	return out
}

// Image is a processed photo attached to a wish or a recipe (the owner is
// tracked by storage). Key is a server-generated random identifier used for
// file names; it never comes from user input.
type Image struct {
	ID        ImageID
	Key       string
	Width     int
	Height    int
	Bytes     int64
	Position  int
	CreatedAt time.Time
}

// User is a whitelisted Telegram user known to the bot.
type User struct {
	ID        UserID
	FirstName string
	LastName  string
	Username  string
	// HasChat is true once the user has started a private chat with the bot,
	// i.e. the bot is allowed to send them notifications.
	HasChat   bool
	UpdatedAt time.Time
}

// DisplayName returns the best human-readable name of the user.
func (u User) DisplayName() string {
	if name := strings.TrimSpace(u.FirstName + " " + u.LastName); name != "" {
		return name
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return "Кто-то"
}

// StatusTotals aggregates the wishes of one status.
type StatusTotals struct {
	Count int
	// Sums holds one entry per currency; currencies are never converted.
	Sums []Money
}

// CategoryStats aggregates the wishes of a single category.
type CategoryStats struct {
	// Category is nil for wishes without a category.
	Category *Category
	ByStatus map[Status]StatusTotals
}

// Stats is the per-category summary shown in /stats and the Mini App.
type Stats struct {
	Categories []CategoryStats
	Overall    map[Status]StatusTotals
	// Recipes is the number of saved recipes.
	Recipes int
	// FulfilledThisYear counts wishes fulfilled since January 1st (local time).
	FulfilledThisYear int
}
