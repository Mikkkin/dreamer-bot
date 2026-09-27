package bot

import (
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Excerpt lengths (runes) of long free text in chat messages; the Mini App
// always shows the full text.
const (
	draftTextExcerpt   = 300
	captionNoteExcerpt = 300
	messageNoteExcerpt = 1500
	captionBodyExcerpt = 600
	messageBodyExcerpt = 3000
	dateLayout         = "02.01.2006"
)

// card is a message about one wish or recipe: a cover photo with a caption
// when there is one, a plain text message otherwise (or when the photo
// cannot be sent).
type card struct {
	cover   *domain.ImageID
	caption string // HTML within the caption limit
	text    string // HTML within the message limit
	kb      *models.InlineKeyboardMarkup
}

func coverOf(images []domain.Image) *domain.ImageID {
	if len(images) == 0 {
		return nil
	}
	id := images[0].ID
	return &id
}

// entityMeta is what a card shows beyond the entity itself.
type entityMeta struct {
	category string // category label, "" when uncategorized
	author   string
}

func renderDraft(d draft) string {
	h := newHTML(maxMessageLen)
	if d.kind == kindWish {
		h.Text("✨ ").Bold("Новое желание")
	} else {
		h.Text("🍳 ").Bold("Новый рецепт")
	}
	h.NL().NL()
	if d.title != "" {
		h.Bold(d.title)
	} else {
		h.Italic("Без названия — нажмите «✏️ Название»")
	}
	if d.kind == kindWish {
		if d.category != nil {
			h.NL().Text("🏷 " + d.categoryLabel)
		}
		if d.price != nil {
			h.NL().Text("💰 " + d.price.Format())
		}
	}
	if d.link != nil {
		h.NL().Text("🔗 ").Link(*d.link, linkLabel(*d.link))
	}
	if d.kind == kindWish && d.hot {
		h.NL().Text("🔥 Очень хочу")
	}
	if n := len(d.photos); n > 0 {
		h.NL().Text("📷 Фото: " + strconv.Itoa(n))
		if n >= d.photoLimit() {
			h.Text(" (больше не поместится)")
		}
	}
	if d.text != "" {
		text, _ := excerpt(d.text, draftTextExcerpt)
		h.NL().NL().Italic(text)
	}
	h.NL().NL()
	if d.view == viewCategories {
		h.Text("Выберите категорию 👇")
	} else {
		h.Italic("Проверьте и нажмите «✅ Сохранить»")
	}
	return h.String()
}

// renderSaved is the final state of a draft card.
func renderSaved(d draft, failedPhotos int) string {
	h := newHTML(maxMessageLen)
	h.Text("✅ ").Bold("Сохранено ✨").NL()
	h.Text("«" + d.title + "»")
	if d.kind == kindWish {
		if d.category != nil {
			h.Text(" · " + d.categoryLabel)
		}
		if d.price != nil {
			h.Text(" · " + d.price.Format())
		}
	}
	if saved := len(d.photos) - failedPhotos; saved > 0 {
		h.NL().Text("📷 Фото: " + strconv.Itoa(saved))
	}
	if failedPhotos > 0 {
		h.NL().Text("⚠️ Не получилось добавить фото: " + strconv.Itoa(failedPhotos) + ". Их можно загрузить в приложении.")
	}
	if d.kind == kindWish {
		h.NL().NL().Text("Все желания — /list")
	} else {
		h.NL().NL().Text("Все рецепты — /recipes")
	}
	return h.String()
}

func wishCard(w domain.Wish, meta entityMeta, loc *time.Location, openURL string) card {
	return card{
		cover:   coverOf(w.Images),
		caption: renderWish(w, meta, loc, maxCaptionLen, captionNoteExcerpt),
		text:    renderWish(w, meta, loc, maxMessageLen, messageNoteExcerpt),
		kb:      wishCardKeyboard(w, openURL),
	}
}

func renderWish(w domain.Wish, meta entityMeta, loc *time.Location, limit, noteLimit int) string {
	h := newHTML(limit)
	h.Bold(w.Title)
	if w.Hot {
		h.Text(" 🔥")
	}
	if meta.category != "" {
		h.NL().Text("🏷 " + meta.category)
	}
	if w.Price != nil {
		h.NL().Text("💰 " + w.Price.Format())
	}
	if w.Link != nil {
		h.NL().Text("🔗 ").Link(*w.Link, linkLabel(*w.Link))
	}
	if w.Note != "" {
		note, _ := excerpt(w.Note, noteLimit)
		h.NL().NL().Italic(note)
	}
	h.NL().NL().Text(meta.author + " · " + w.CreatedAt.In(loc).Format(dateLayout))
	h.NL().Text(w.Status.Emoji() + " " + w.Status.Label())
	if w.Status == domain.StatusDone && w.FulfilledAt != nil {
		h.Text(" · " + w.FulfilledAt.In(loc).Format(dateLayout))
	}
	return h.String()
}

func recipeCard(r domain.Recipe, meta entityMeta, loc *time.Location, openURL string, reroll bool) card {
	return card{
		cover:   coverOf(r.Images),
		caption: renderRecipe(r, meta, loc, maxCaptionLen, captionBodyExcerpt),
		text:    renderRecipe(r, meta, loc, maxMessageLen, messageBodyExcerpt),
		kb:      recipeCardKeyboard(r, openURL, reroll),
	}
}

func renderRecipe(r domain.Recipe, meta entityMeta, loc *time.Location, limit, bodyLimit int) string {
	h := newHTML(limit)
	h.Text("🍳 ").Bold(r.Title)
	if r.Link != nil {
		h.NL().Text("🔗 ").Link(*r.Link, linkLabel(*r.Link))
	}
	if r.Body != "" {
		body, cut := excerpt(r.Body, bodyLimit)
		h.NL().NL().Text(body)
		if cut {
			h.NL().Italic(fullTextHint)
		}
	}
	h.NL().NL().Text(meta.author + " · " + r.CreatedAt.In(loc).Format(dateLayout))
	return h.String()
}

const fullTextHint = "Полный текст — в приложении"

func renderWishList(status domain.Status, count int) string {
	h := newHTML(maxMessageLen)
	h.Bold("Наши желания").Text(" · " + status.Emoji() + " " + status.Label()).NL()
	switch {
	case count > 0:
		h.Text("Нажмите на желание, чтобы открыть карточку.")
	case status == domain.StatusWant:
		h.Text("Здесь пока пусто. Пришлите мне текст, ссылку или фото — и я предложу сохранить желание ✨")
	default:
		h.Text("Здесь пока пусто.")
	}
	return h.String()
}

func renderRecipeList(total int) string {
	h := newHTML(maxMessageLen)
	h.Text("🍳 ").Bold("Что приготовить").Text(" · " + strconv.Itoa(total) + " " + plural(total, "рецепт", "рецепта", "рецептов")).NL()
	if total == 0 {
		h.Text(noRecipesHint)
	} else {
		h.Text("Нажмите на рецепт, чтобы открыть его.")
	}
	return h.String()
}

const noRecipesHint = "Пока нет ни одного рецепта. Пришлите мне ссылку или текст рецепта и выберите «🍳 Рецепт» — он появится здесь."

func renderStats(s domain.Stats) string {
	h := newHTML(maxMessageLen)
	h.Text("📊 ").Bold("Статистика").NL()
	total := 0
	for _, st := range domain.Statuses {
		total += s.Overall[st].Count
	}
	if total == 0 && s.Recipes == 0 {
		h.NL().Text("Пока пусто — пришлите мне первое желание ✨")
		return h.String()
	}
	for _, c := range s.Categories {
		line := statusLine(c.ByStatus)
		if line == "" {
			continue
		}
		label := "📦 Без категории"
		if c.Category != nil {
			label = c.Category.Label()
		}
		h.NL().Text(label + " — " + line)
	}
	if total > 0 {
		h.NL().NL().Bold("Всего").Text(": " + statusCounts(s.Overall))
		if sums := openSums(s.Overall); sums != "" {
			h.NL().Text("💰 Хотим и копим: " + sums)
		}
	}
	h.NL().Text("✨ Сбылось в этом году: " + strconv.Itoa(s.FulfilledThisYear))
	h.NL().Text("🍳 Рецептов: " + strconv.Itoa(s.Recipes))
	return h.String()
}

// statusLine is "3 хотим · 1 копим · 2 сбылось · 3 450 €" (empty when the
// category has no wishes).
func statusLine(by map[domain.Status]domain.StatusTotals) string {
	counts := statusCounts(by)
	if counts == "" {
		return ""
	}
	if sums := openSums(by); sums != "" {
		return counts + " · " + sums
	}
	return counts
}

func statusCounts(by map[domain.Status]domain.StatusTotals) string {
	var parts []string
	for _, st := range domain.Statuses {
		if n := by[st].Count; n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+strings.ToLower(st.Label()))
		}
	}
	return strings.Join(parts, " · ")
}

// openSums totals what is still wanted or being saved for, per currency.
func openSums(by map[domain.Status]domain.StatusTotals) string {
	var all []domain.Money
	all = append(all, by[domain.StatusWant].Sums...)
	all = append(all, by[domain.StatusProgress].Sums...)
	sums := domain.SumByCurrency(all)
	parts := make([]string, 0, len(sums))
	for _, m := range sums {
		if m.Minor > 0 {
			parts = append(parts, m.Format())
		}
	}
	return strings.Join(parts, " + ")
}

// Partner notifications.

func renderWishCreated(actor string, w domain.Wish, limit int) string {
	h := newHTML(limit)
	h.Text("💫 ").Bold(actor).Text(" · новое желание").NL()
	h.Text("«" + w.Title + "»")
	if w.Price != nil {
		h.Text(" · " + w.Price.Format())
	}
	return h.String()
}

func renderWishFulfilled(actor string, w domain.Wish, limit int) string {
	h := newHTML(limit)
	h.Text("✨ ").Bold("Сбылось!").Text(" «" + w.Title + "» — отметил(а) " + actor)
	return h.String()
}

func renderRecipeCreated(actor string, r domain.Recipe, limit int) string {
	h := newHTML(limit)
	h.Text("🍳 ").Bold(actor).Text(" · новый рецепт").NL()
	h.Text("«" + r.Title + "»")
	return h.String()
}

// plural picks the Russian plural form for n: 1 рецепт, 2 рецепта, 5 рецептов.
func plural(n int, one, few, many string) string {
	n10, n100 := n%10, n%100
	switch {
	case n10 == 1 && n100 != 11:
		return one
	case n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14):
		return few
	}
	return many
}
