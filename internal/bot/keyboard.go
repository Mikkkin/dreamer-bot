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

func draftKeyboard(d draft, categories []domain.Category) *models.InlineKeyboardMarkup {
	op := func(o cbOp) callback { return callback{op: o, draft: d.id} }
	field := func(f draftField) callback { return callback{op: opDraftField, draft: d.id, field: f} }

	if d.view == viewCategories {
		return categoryKeyboard(d, categories)
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
		rows = append(rows, []button{cbButton(link, field(fieldLink)), cbButton(text, field(fieldText))})
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
	pages := max(1, (total+listPageSize-1)/listPageSize)
	page = min(max(page, 0), pages-1)
	from := page * listPageSize
	return page, pages, from, min(from+listPageSize, total)
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
	return title
}

func wishCardKeyboard(w domain.Wish, openURL string) *models.InlineKeyboardMarkup {
	statuses := make([]button, 0, len(domain.Statuses))
	for _, s := range domain.Statuses {
		statuses = append(statuses, cbButton(marked(s == w.Status, s.Emoji()+" "+s.Label()),
			callback{op: opWishStatus, id: int64(w.ID), status: s}))
	}
	var open []button
	if openURL != "" {
		open = []button{webAppButton("📱 Открыть", openURL)}
	}
	return keyboard(statuses, open, []button{cbButton("🗑 Удалить", callback{op: opWishAskDelete, id: int64(w.ID)})})
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
		rows = append(rows, []button{cbButton("🍳 "+title, callback{op: opRecipeOpen, id: int64(r.ID)})})
	}
	rows = append(rows, pager(page, pages, func(p int) callback { return callback{op: opRecipeList, page: p} }))
	return keyboard(rows...)
}

func recipeCardKeyboard(r domain.Recipe, openURL string, reroll bool) *models.InlineKeyboardMarkup {
	row := []button{}
	if openURL != "" {
		row = append(row, webAppButton("📱 Открыть", openURL))
	}
	row = append(row, cbButton("🗑 Удалить", callback{op: opRecipeAskDelete, id: int64(r.ID)}))
	var again []button
	if reroll {
		again = []button{cbButton("🎲 Ещё вариант", callback{op: opCookAgain, id: int64(r.ID)})}
	}
	return keyboard(row, again)
}
