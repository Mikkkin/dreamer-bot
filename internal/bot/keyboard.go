package bot

import (
	"strconv"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Button styles (Bot API 9.4); older clients ignore them.
const (
	stylePrimary = "primary"
	styleSuccess = "success"
	styleDanger  = "danger"
)

const (
	listPageSize   = 8
	buttonTitleLen = 40
	selectedMark   = "• "
)

type button = models.InlineKeyboardButton

func cbButton(text string, c callback) button {
	return button{Text: text, CallbackData: c.String()}
}

func webAppButton(text, url string) button {
	return button{Text: text, WebApp: &models.WebAppInfo{URL: url}, Style: stylePrimary}
}

func keyboard(rows ...[]button) *models.InlineKeyboardMarkup {
	var kept [][]button
	for _, r := range rows {
		if len(r) > 0 {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: kept}
}

// openKeyboard is a single «open in the Mini App» button, or nil when the
// Mini App URL is not known yet.
func openKeyboard(text, url string) *models.InlineKeyboardMarkup {
	if url == "" {
		return nil
	}
	return keyboard([]button{webAppButton(text, url)})
}

func marked(on bool, text string) string {
	if on {
		return selectedMark + text
	}
	return text
}

// draftKeyboard is the keyboard of a draft card. categories and tags are
// only needed (and only loaded) while the matching picker is open.
func draftKeyboard(d draft, categories []domain.Category, tags []domain.RecipeTag) *models.InlineKeyboardMarkup {
	op := func(o cbOp) callback { return callback{op: o, draft: d.id} }
	field := func(f draftField) callback { return callback{op: opDraftField, draft: d.id, field: f} }

	switch d.view {
	case viewCategories:
		return categoryKeyboard(d, categories)
	case viewCuisines, viewCourses:
		return tagKeyboard(d, tags)
	}

	rows := [][]button{{
		cbButton(marked(d.kind == kindWish, "✨ Желание"), callback{op: opDraftKind, draft: d.id, kind: kindWish}),
		cbButton(marked(d.kind == kindRecipe, "🍳 Рецепт"), callback{op: opDraftKind, draft: d.id, kind: kindRecipe}),
	}}
	link := "🔗 Ссылка"
	if d.link != nil {
		link += " ✓"
	}
	if d.kind == kindWish {
		category := "🏷 Категория"
		if d.category != nil {
			category = "🏷 " + d.categoryLabel
		}
		price := "💰 Сумма"
		if d.price != nil {
			price = "💰 " + d.price.Format()
		}
		hot := "🔥 Очень хочу —"
		if d.hot {
			hot = "🔥 Очень хочу ✓"
		}
		rows = append(rows,
			[]button{cbButton(category, op(opDraftCategories))},
			[]button{cbButton(price, field(fieldPrice)), cbButton(link, field(fieldLink))},
			[]button{cbButton(hot, op(opDraftHot))},
		)
	} else {
		text := "📝 Текст рецепта"
		if d.text != "" {
			text += " ✓"
		}
		cuisine := "🍜 Кухня"
		if d.cuisine != nil {
			cuisine = d.cuisine.label
		}
		course := "🍽 Тип"
		if n := len(d.courses); n > 0 {
			course = d.courses[0].label
			if n > 1 {
				course += " +" + strconv.Itoa(n-1)
			}
		}
		rows = append(rows,
			[]button{cbButton(cuisine, op(opDraftCuisines)), cbButton(course, op(opDraftCourses))},
			[]button{cbButton(link, field(fieldLink)), cbButton(text, field(fieldText))},
		)
	}
	save := cbButton("✅ Сохранить", op(opDraftSave))
	save.Style = styleSuccess
	rows = append(rows,
		[]button{cbButton("✏️ Название", field(fieldTitle))},
		[]button{save, cbButton("✖️ Отмена", op(opDraftCancel))},
	)
	return keyboard(rows...)
}

func categoryKeyboard(d draft, categories []domain.Category) *models.InlineKeyboardMarkup {
	var rows [][]button
	var row []button
	for _, c := range categories {
		selected := d.category != nil && *d.category == c.ID
		row = append(row, cbButton(marked(selected, c.Label()), callback{op: opDraftCategory, draft: d.id, id: int64(c.ID)}))
		if len(row) == 2 {
			rows, row = append(rows, row), nil
		}
	}
	rows = append(rows, row,
		[]button{cbButton(marked(d.category == nil, "Без категории"), callback{op: opDraftCategory, draft: d.id})},
		[]button{cbButton("← Назад", callback{op: opDraftBack, draft: d.id})},
	)
	return keyboard(rows...)
}

// tagKeyboard is the cuisine picker (one choice, back to the card on tap)
// or the course picker (several choices, stays open until «Готово»).
func tagKeyboard(d draft, tags []domain.RecipeTag) *models.InlineKeyboardMarkup {
	kind, pick, none, done := domain.TagCuisine, opDraftCuisine, "Без кухни", "← Назад"
	empty := d.cuisine == nil
	selected := func(id domain.RecipeTagID) bool { return d.cuisine != nil && d.cuisine.id == id }
	if d.view == viewCourses {
		kind, pick, none, done = domain.TagCourse, opDraftCourse, "Без типа", "✅ Готово"
		empty, selected = len(d.courses) == 0, d.hasCourse
	}
	var rows [][]button
	var row []button
	for _, t := range tags {
		if t.Kind != kind {
			continue
		}
		row = append(row, cbButton(marked(selected(t.ID), t.Label()), callback{op: pick, draft: d.id, id: int64(t.ID)}))
		if len(row) == 2 {
			rows, row = append(rows, row), nil
		}
	}
	rows = append(rows, row,
		[]button{cbButton(marked(empty, none), callback{op: pick, draft: d.id})},
		[]button{cbButton(done, callback{op: opDraftBack, draft: d.id})},
	)
	return keyboard(rows...)
}

// pager is the ◀️ n/m ▶️ row; empty for a single page.
func pager(page, pages int, at func(page int) callback) []button {
	if pages <= 1 {
		return nil
	}
	var row []button
	if page > 0 {
		row = append(row, cbButton("◀️", at(page-1)))
	}
	row = append(row, cbButton(strconv.Itoa(page+1)+"/"+strconv.Itoa(pages), callback{op: opNoop}))
	if page < pages-1 {
		row = append(row, cbButton("▶️", at(page+1)))
	}
	return row
}

// pageBounds clamps page into range and returns the slice bounds of it.
func pageBounds(total, page int) (int, int, int, int) {
	return pageBoundsOf(total, page, listPageSize)
}

// pageBoundsOf is pageBounds for pages of size items.
func pageBoundsOf(total, page, size int) (int, int, int, int) {
	pages := max(1, (total+size-1)/size)
	page = min(max(page, 0), pages-1)
	from := page * size
	return page, pages, from, min(from+size, total)
}

func wishListKeyboard(status domain.Status, counts map[domain.Status]int, items []domain.Wish, page, pages int) *models.InlineKeyboardMarkup {
	tabs := make([]button, 0, len(domain.Statuses))
	for _, s := range domain.Statuses {
		text := s.Emoji() + " " + s.Label() + " " + strconv.Itoa(counts[s])
		tabs = append(tabs, cbButton(marked(s == status, text), callback{op: opWishList, status: s}))
	}
	rows := [][]button{tabs}
	for _, w := range items {
		rows = append(rows, []button{cbButton(wishButtonText(w), callback{op: opWishOpen, id: int64(w.ID)})})
	}
	rows = append(rows, pager(page, pages, func(p int) callback {
		return callback{op: opWishList, status: status, page: p}
	}))
	return keyboard(rows...)
}

func wishButtonText(w domain.Wish) string {
	title, _ := excerpt(w.Title, buttonTitleLen)
	if w.Hot {
		title = "🔥 " + title
	}
	if w.Price != nil {
		title += " · " + w.Price.Format()
	}
	if p, ok := w.SavedPercent(); ok {
		title += " · " + strconv.Itoa(p) + "%"
	}
	return title
}

func wishCardKeyboard(w domain.Wish, openURL string) *models.InlineKeyboardMarkup {
	statuses := make([]button, 0, len(domain.Statuses))
	for _, s := range domain.Statuses {
		statuses = append(statuses, cbButton(marked(s == w.Status, s.Emoji()+" "+s.Label()),
			callback{op: opWishStatus, id: int64(w.ID), status: s}))
	}
	var save, open []button
	if canSave(w) {
		save = []button{cbButton("💰 Отложить", callback{op: opWishSave, id: int64(w.ID)})}
	}
	if openURL != "" {
		open = []button{webAppButton("📱 Открыть", openURL)}
	}
	return keyboard(statuses, save, open, []button{cbButton("🗑 Удалить", callback{op: opWishAskDelete, id: int64(w.ID)})})
}

// canSave reports whether the card offers «Отложить»: for wishes being
// saved for, and for any wish not yet fulfilled that already has savings.
func canSave(w domain.Wish) bool {
	return w.Status == domain.StatusProgress || (w.Status != domain.StatusDone && w.Saved != nil)
}

// confirmDeleteKeyboard asks before deleting; yes and no are the ops of the
// entity (wish or recipe).
func confirmDeleteKeyboard(id int64, yes, no cbOp) *models.InlineKeyboardMarkup {
	del := cbButton("Да, удалить", callback{op: yes, id: id})
	del.Style = styleDanger
	return keyboard([]button{del, cbButton("Отмена", callback{op: no, id: id})})
}

func recipeListKeyboard(items []domain.Recipe, page, pages int) *models.InlineKeyboardMarkup {
	var rows [][]button
	for _, r := range items {
		title, _ := excerpt(r.Title, buttonTitleLen)
		if avg, ok := r.Cooking.AverageTenths(); ok {
			title += " · ⭐ " + domain.FormatTenths(avg)
		}
		rows = append(rows, []button{cbButton("🍳 "+title, callback{op: opRecipeOpen, id: int64(r.ID)})})
	}
	rows = append(rows, pager(page, pages, func(p int) callback { return callback{op: opRecipeList, page: p} }))
	return keyboard(rows...)
}

func recipeCardKeyboard(r domain.Recipe, openURL string, reroll bool) *models.InlineKeyboardMarkup {
	id := int64(r.ID)
	cook := cbButton("🍳 Приготовили", callback{op: opRecipeCookAsk, id: id})
	cook.Style = styleSuccess
	actions := []button{cook}
	if len(r.Ingredients) > 0 {
		actions = append(actions, cbButton("🛒 В покупки", callback{op: opRecipeShop, id: id}))
	}
	row := []button{}
	if openURL != "" {
		row = append(row, webAppButton("📱 Открыть", openURL))
	}
	row = append(row, cbButton("🗑 Удалить", callback{op: opRecipeAskDelete, id: id}))
	var again []button
	if reroll {
		again = []button{cbButton("🎲 Ещё вариант", callback{op: opCookAgain, id: id})}
	}
	return keyboard(actions, row, again)
}

// cookKeyboard replaces the card buttons after «Приготовили»: the author
// rates the cooking right away, skips the rating or goes back.
func cookKeyboard(id domain.RecipeID) *models.InlineKeyboardMarkup {
	stars := make([]button, 0, maxStars)
	for s := 1; s <= maxStars; s++ {
		stars = append(stars, cbButton(strconv.Itoa(s)+"⭐", callback{op: opRecipeCook, id: int64(id), stars: s}))
	}
	return keyboard(stars, []button{
		cbButton("Без оценки", callback{op: opRecipeCook, id: int64(id)}),
		cbButton("← Назад", callback{op: opRecipeBack, id: int64(id)}),
	})
}

// rateKeyboard lets a partner rate a cooking from its notification; the
// chosen stars (0 = none yet) are marked and can still be changed.
func rateKeyboard(recipe domain.RecipeID, cook domain.CookID, openURL string, chosen int) *models.InlineKeyboardMarkup {
	stars := make([]button, 0, maxStars)
	for s := 1; s <= maxStars; s++ {
		stars = append(stars, cbButton(marked(s == chosen, strconv.Itoa(s)+"⭐"),
			callback{op: opRecipeRate, id: int64(recipe), cook: int64(cook), stars: s}))
	}
	var open []button
	if openURL != "" {
		open = []button{webAppButton("Открыть ✨", openURL)}
	}
	return keyboard(stars, open)
}

// Shopping list layout: unchecked items one per row, a compact tail of
// bought ones.
const (
	shopPageSize     = 30
	shopCheckedShown = 10
	shopNameLen      = 40
	shopCheckedLen   = 18
)

// shopView is one page of the shopping list.
type shopView struct {
	unchecked   []domain.ShoppingItem // this page only
	checked     []domain.ShoppingItem // all bought items
	total       int                   // unchecked items on every page
	page, pages int
}

func newShopView(items []domain.ShoppingItem, page int) shopView {
	var v shopView
	var open []domain.ShoppingItem
	for _, it := range items {
		if it.Checked {
			v.checked = append(v.checked, it)
		} else {
			open = append(open, it)
		}
	}
	var from, to int
	v.total = len(open)
	v.page, v.pages, from, to = pageBoundsOf(len(open), page, shopPageSize)
	v.unchecked = open[from:to]
	return v
}

func shoppingKeyboard(v shopView, openURL string) *models.InlineKeyboardMarkup {
	var rows [][]button
	for _, it := range v.unchecked {
		rows = append(rows, []button{cbButton("☐ "+shopItemText(it),
			callback{op: opShopCheck, id: int64(it.ID), page: v.page})})
	}
	var row []button
	for _, it := range v.checked[:min(len(v.checked), shopCheckedShown)] {
		name, _ := excerpt(it.Name, shopCheckedLen)
		row = append(row, cbButton("☑ "+name, callback{op: opShopUncheck, id: int64(it.ID), page: v.page}))
		if len(row) == 2 {
			rows, row = append(rows, row), nil
		}
	}
	rows = append(rows, row)
	if n := len(v.checked); n > 0 {
		rows = append(rows, []button{cbButton("🧹 Очистить купленное ("+strconv.Itoa(n)+")", callback{op: opShopClear})})
	}
	rows = append(rows, pager(v.page, v.pages, func(p int) callback { return callback{op: opShopList, page: p} }))
	tail := []button{}
	if openURL != "" {
		tail = append(tail, webAppButton("Открыть список ✨", openURL))
	}
	tail = append(tail, cbButton("🔄", callback{op: opShopList, page: v.page}))
	rows = append(rows, tail)
	return keyboard(rows...)
}

// shopItemText is "Молоко — 750 мл" (plain button text, never HTML).
func shopItemText(it domain.ShoppingItem) string {
	name, _ := excerpt(it.Name, shopNameLen)
	if it.Quantity != nil {
		if q := it.Quantity.Format(); q != "" {
			name += " — " + q
		}
	}
	return name
}
