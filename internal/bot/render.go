package bot

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/nutrition"
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
	// Ingredients listed on a recipe card sent as a text message; a caption
	// only has room for their count.
	messageIngredients = 15
	ingredientNameLen  = 40
	commentExcerpt     = 280
	progressCells      = 10
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
	tags     string // recipe cuisine and course labels, "" when untagged
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
	if d.kind == kindRecipe {
		if d.cuisine != nil {
			h.NL().Text(d.cuisine.label)
		}
		if len(d.courses) > 0 {
			h.NL().Text(d.courseLabels())
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
	switch d.view {
	case viewCategories:
		h.Text("Выберите категорию 👇")
	case viewCuisines:
		h.Text("Выберите кухню 👇")
	case viewCourses:
		h.Text("Выберите тип блюда — можно несколько 👇")
	default:
		if d.source != "" {
			h.Text("📥 Похоже на рецепт — могу разложить ингредиенты и шаги сам.").NL()
		}
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
	} else {
		if d.cuisine != nil {
			h.Text(" · " + d.cuisine.label)
		}
		if len(d.courses) > 0 {
			h.Text(" · " + d.courseLabels())
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
	saved, percent, ofPrice := savings(w)
	if w.Price != nil && !ofPrice {
		h.NL().Text("💰 " + w.Price.Format())
	}
	if saved != "" {
		h.NL().Text("💰 " + saved)
		if ofPrice {
			h.NL().Text(progressBar(percent))
		}
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

// renderRecipe renders a recipe card. Within a caption (limit ≤ 1024) the
// ingredients are only counted; a text message lists the first of them.
func renderRecipe(r domain.Recipe, meta entityMeta, loc *time.Location, limit, bodyLimit int) string {
	h := newHTML(limit)
	h.Text("🍳 ").Bold(r.Title)
	if meta.tags != "" {
		h.NL().Text(meta.tags)
	}
	if line := cookingLine(r.Cooking); line != "" {
		h.NL().Text(line)
	}
	if r.Link != nil {
		h.NL().Text("🔗 ").Link(*r.Link, linkLabel(*r.Link))
	}
	if r.Servings != nil {
		h.NL().Text("🍽 " + servingsText(*r.Servings))
	}
	if line := recipeNutritionLine(r); line != "" {
		h.NL().Text(line)
	}
	if n := len(r.Ingredients); n > 0 {
		if limit <= maxCaptionLen {
			h.NL().Text("🧾 " + strconv.Itoa(n) + " " + plural(n, "ингредиент", "ингредиента", "ингредиентов"))
		} else {
			renderIngredients(h, r.Ingredients, messageIngredients)
		}
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
	if saved := joinMoney(s.Saved); saved != "" {
		h.NL().Text("🐷 Отложено: " + saved)
	}
	h.NL().Text("✨ Сбылось в этом году: " + strconv.Itoa(s.FulfilledThisYear))
	h.NL().Text("🍳 Рецептов: " + strconv.Itoa(s.Recipes))
	if n := s.RecipesCooked; n > 0 {
		h.Text(" · готовили " + timesText(n))
	}
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
	return joinMoney(all)
}

// joinMoney is "3 450 € + 50 $": the amounts summed per currency.
func joinMoney(amounts []domain.Money) string {
	sums := domain.SumByCurrency(amounts)
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

// renderRecipeUpdated coalesces a burst of edits into one notice.
func renderRecipeUpdated(actor string, r domain.Recipe, limit int) string {
	h := newHTML(limit)
	h.Text("✏️ ").Bold(actor).Text(" · изменения в рецепте").NL()
	h.Text("«" + r.Title + "»")
	return h.String()
}

// renderRecipeCooked tells a partner about a cooking. authorStars is the
// cook's own rating (0 = none); yourStars is the partner's rating once given
// from the notice (0 = not yet, which invites to rate).
func renderRecipeCooked(actor string, title string, authorStars, yourStars, limit int) string {
	h := newHTML(limit)
	h.Text("🍳 ").Bold(actor).Text(" · приготовлено «" + title + "»")
	if authorStars > 0 {
		h.Text(" " + starsText(authorStars))
	}
	if yourStars > 0 {
		h.NL().Text("Ваша оценка: " + starsText(yourStars) + " — спасибо!")
	} else {
		h.NL().Text("Оцените тоже:")
	}
	return h.String()
}

func renderRecipeRated(actor string, r domain.Recipe, rating domain.Rating, limit int) string {
	h := newHTML(limit)
	h.Text("⭐ ").Bold(actor).Text(" · оценка " + strconv.Itoa(rating.Stars) + "/5 — «" + r.Title + "»")
	if rating.Comment != "" {
		comment, _ := excerpt(rating.Comment, commentExcerpt)
		h.NL().Italic(comment)
	}
	return h.String()
}

func renderWishSaved(actor string, w domain.Wish, s domain.Saving, limit int) string {
	h := newHTML(limit)
	h.Text("💰 ").Bold(actor).Text(" · отложено " + s.Amount.Format() + " на «" + w.Title + "»")
	if w.Saved != nil {
		saved, percent, ofPrice := savings(w)
		h.NL().Text(saved)
		if ofPrice && percent >= 100 {
			h.NL().Text("🎉 Всё накоплено!")
		}
	}
	return h.String()
}

// renderSavingAdded confirms a contribution made from the chat.
func renderSavingAdded(w domain.Wish, s domain.Saving) string {
	h := newHTML(maxMessageLen)
	h.Text("💰 ").Bold("Отложено " + s.Amount.Format()).Text(" на «" + w.Title + "»")
	if saved, percent, ofPrice := savings(w); saved != "" {
		h.NL().Text(saved)
		if ofPrice {
			h.NL().Text(progressBar(percent))
			if percent >= 100 {
				h.NL().NL().Text("🎉 Всё накоплено! Можно отмечать «Сбылось».")
			}
		}
	}
	return h.String()
}

func renderShopping(v shopView) string {
	h := newHTML(maxMessageLen)
	h.Text("🛒 ").Bold("Список покупок").NL()
	switch {
	case v.total == 0 && len(v.checked) == 0:
		h.Text("Пока пусто. Добавьте ингредиенты кнопкой «🛒 В покупки» на карточке рецепта или откройте список в приложении.")
		return h.String()
	case v.total == 0:
		h.Text("Всё куплено 🎉")
	default:
		h.Text("Нужно купить: " + strconv.Itoa(v.total))
		if n := len(v.checked); n > 0 {
			h.Text(" · куплено: " + strconv.Itoa(n))
		}
		h.NL().Text("Нажмите на позицию, чтобы отметить её купленной.")
	}
	if n := len(v.checked) - shopCheckedShown; n > 0 {
		h.NL().Italic("Ещё куплено: " + strconv.Itoa(n))
	}
	return h.String()
}

// recipeTagLine is "🍝 Итальянская · 🌙 Ужин · 🍲 Первое": the cuisine, then
// the courses in the recipe's order. Unknown (deleted) tags are skipped.
func recipeTagLine(r domain.Recipe, tags []domain.RecipeTag) string {
	byID := make(map[domain.RecipeTagID]domain.RecipeTag, len(tags))
	for _, t := range tags {
		byID[t.ID] = t
	}
	var labels []string
	if r.CuisineID != nil {
		if t, ok := byID[*r.CuisineID]; ok && t.Kind == domain.TagCuisine {
			labels = append(labels, t.Label())
		}
	}
	for _, id := range r.CourseIDs {
		if t, ok := byID[id]; ok && t.Kind == domain.TagCourse {
			labels = append(labels, t.Label())
		}
	}
	return strings.Join(labels, " · ")
}

// cookingLine is "⭐ 4,5 · готовили 3 раза", "🍳 Готовили 1 раз" or "".
func cookingLine(s domain.CookingSummary) string {
	if s.Count <= 0 {
		return ""
	}
	if avg, ok := s.AverageTenths(); ok {
		return "⭐ " + domain.FormatTenths(avg) + " · готовили " + timesText(s.Count)
	}
	return "🍳 Готовили " + timesText(s.Count)
}

func timesText(n int) string {
	return strconv.Itoa(n) + " " + plural(n, "раз", "раза", "раз")
}

// recipeNutritionLine is the КБЖУ on a recipe card: the recipe's own
// values when they are set, otherwise the estimate from its ingredients,
// marked «≈» and with how many ingredients it covers.
func recipeNutritionLine(r domain.Recipe) string {
	if r.Nutrition != nil {
		return nutritionLine(r.Nutrition)
	}
	res, ok := estimate(r)
	if !ok {
		return ""
	}
	v, per := res.Per100, "на 100 г"
	if res.PerServing != nil {
		v, per = *res.PerServing, "на порцию"
	}
	return "🔥 ≈" + kcalText(v.Kcal) + " ккал · Б " + domain.FormatTenths(v.Protein) +
		" · Ж " + domain.FormatTenths(v.Fat) + " · У " + domain.FormatTenths(v.Carbs) +
		" (" + per + ", " + coverageText(res.Coverage) + ")"
}

// estimate computes the КБЖУ of the recipe from its ingredients with the
// built-in food table; ok is false when no ingredient could be counted.
func estimate(r domain.Recipe) (nutrition.Result, bool) {
	if len(r.Ingredients) == 0 {
		return nutrition.Result{}, false
	}
	servings := 0
	if r.Servings != nil {
		servings = *r.Servings
	}
	res := nutrition.Default().Compute(r.Ingredients, servings)
	return res, res.Coverage.Counted > 0
}

// coverageText is «по 5 из 6 ингредиентов» or «по всем ингредиентам»;
// «по вкусу» lines do not count.
func coverageText(c nutrition.Coverage) string {
	if c.Counted >= c.Total {
		return "по всем ингредиентам"
	}
	return "по " + strconv.Itoa(c.Counted) + " из " + strconv.Itoa(c.Total) + " " +
		plural(c.Total, "ингредиента", "ингредиентов", "ингредиентов")
}

// kcalText rounds tenths of kcal to whole kcal: an estimate has no
// meaningful decimals.
func kcalText(tenths int) string { return strconv.Itoa((tenths + 5) / 10) }

func servingsText(n int) string {
	return strconv.Itoa(n) + " " + plural(n, "порция", "порции", "порций")
}

// recipeSummary is «11 ингредиентов · 6 шагов · 4 порции · ≈540 ккал/порц»;
// unknown parts are left out.
func recipeSummary(r domain.Recipe) string {
	var parts []string
	if n := len(r.Ingredients); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" "+plural(n, "ингредиент", "ингредиента", "ингредиентов"))
	}
	if n := countSteps(r.Body); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" "+plural(n, "шаг", "шага", "шагов"))
	}
	if r.Servings != nil {
		parts = append(parts, servingsText(*r.Servings))
	}
	if kcal := kcalPerServing(r); kcal != "" {
		parts = append(parts, kcal)
	}
	return strings.Join(parts, " · ")
}

// kcalPerServing is «540 ккал/порц» from the recipe's own КБЖУ, «≈540
// ккал/порц» estimated from the ingredients, or "" when the servings (or,
// for the own values, the dish weight) are unknown.
func kcalPerServing(r domain.Recipe) string {
	if r.Nutrition != nil {
		if m, ok := r.Nutrition.PerServing(); ok {
			return kcalText(m.Kcal) + " ккал/порц"
		}
		return ""
	}
	if res, ok := estimate(r); ok && res.PerServing != nil {
		return "≈" + kcalText(res.PerServing.Kcal) + " ккал/порц"
	}
	return ""
}

// stepLine is a numbered step of a recipe body: «1. …» or «1) …».
var stepLine = regexp.MustCompile(`^\d{1,3}[.)]\s`)

// countSteps counts the numbered lines of a recipe body, which is how
// imported steps are written. The original text an unsure import keeps
// below sourceHeading does not count.
func countSteps(body string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == sourceHeading {
			break
		}
		if stepLine.MatchString(line) {
			n++
		}
	}
	return n
}

// nutritionLine is the short КБЖУ of a recipe: per serving when known,
// per 100 g otherwise.
func nutritionLine(n *domain.Nutrition) string {
	if n == nil {
		return ""
	}
	m, per := n.Per100(), "на 100 г"
	if s, ok := n.PerServing(); ok {
		m, per = s, "на порцию"
	}
	return "🔥 " + domain.FormatTenths(m.Kcal) + " ккал · Б " + domain.FormatTenths(m.Protein) +
		" · Ж " + domain.FormatTenths(m.Fat) + " · У " + domain.FormatTenths(m.Carbs) + " (" + per + ")"
}

func renderIngredients(h *htmlText, list []domain.Ingredient, shown int) {
	h.NL().NL().Bold("🧾 Ингредиенты · " + strconv.Itoa(len(list)))
	for _, ing := range list[:min(len(list), shown)] {
		name, _ := excerpt(ing.Name, ingredientNameLen)
		h.NL().Text("• " + name)
		if ing.Quantity != nil {
			if q := ing.Quantity.Format(); q != "" {
				h.Text(" — " + q)
			}
		}
	}
	if rest := len(list) - shown; rest > 0 {
		h.NL().Italic("…и ещё " + strconv.Itoa(rest))
	}
}

// savings describes the money put aside for w: the text after «💰», the
// percent of the price, and whether the text is measured against the price.
func savings(w domain.Wish) (string, int, bool) {
	sameCurrency := w.Price != nil && w.Saved != nil && w.Price.Currency == w.Saved.Currency
	switch {
	case sameCurrency:
		p, _ := w.SavedPercent()
		return "Накоплено " + amountDigits(*w.Saved) + " из " + w.Price.Format() + " (" + strconv.Itoa(p) + "%)", p, true
	case w.Saved != nil:
		return "Накоплено " + w.Saved.Format(), 0, false
	case w.Status == domain.StatusProgress && w.Price != nil:
		return "Накоплено 0 из " + w.Price.Format() + " (0%)", 0, true
	case w.Status == domain.StatusProgress:
		return "Пока ничего не отложено", 0, false
	}
	return "", 0, false
}

// amountDigits is the formatted amount without the currency sign.
func amountDigits(m domain.Money) string {
	return strings.TrimSuffix(m.Format(), " "+m.Currency.Symbol())
}

// progressBar draws percent (0..100) as ten cells; any progress shows.
func progressBar(percent int) string {
	percent = min(max(percent, 0), 100)
	filled := percent * progressCells / 100 // rounds down: only 100% fills every cell
	if filled == 0 && percent > 0 {
		filled = 1
	}
	return strings.Repeat("▰", filled) + strings.Repeat("▱", progressCells-filled)
}

func starsText(n int) string { return strings.Repeat("⭐", min(max(n, 0), maxStars)) }

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
