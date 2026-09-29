package bot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func (e *testEnv) addShopping(t *testing.T, drafts ...domain.ShoppingDraft) []domain.ShoppingItem {
	t.Helper()
	items, err := e.svc.shopping.Add(context.Background(), alice, drafts)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func (e *testEnv) item(t *testing.T, id domain.ShoppingItemID) domain.ShoppingItem {
	t.Helper()
	for _, it := range e.svc.shopping.snapshot() {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no shopping item %d", id)
	return domain.ShoppingItem{}
}

func TestShopCommandListsAndToggles(t *testing.T) {
	e := newTestEnv(t)
	milk := quantity(t, "500", "мл")
	e.addShopping(t,
		domain.ShoppingDraft{Name: "Молоко", Quantity: milk},
		domain.ShoppingDraft{Name: "Хлеб"},
		domain.ShoppingDraft{Name: "<b>Сыр</b> & " + scriptInjected},
	)

	e.handle(commandUpdate(alice, "shop"))
	sent := e.api.lastSent(t)
	if !strings.Contains(sent.Text, "Список покупок") || !strings.Contains(sent.Text, "Нужно купить: 3") {
		t.Errorf("list text %q", sent.Text)
	}
	assertSafeHTML(t, sent.Text)
	texts := buttonTexts(sent.ReplyMarkup)
	for _, want := range []string{"☐ Молоко — " + milk.Format(), "☐ Хлеб", "Открыть список ✨", "🔄"} {
		if !slices.Contains(texts, want) {
			t.Errorf("list lacks button %q: %v", want, texts)
		}
	}
	if slices.ContainsFunc(texts, func(s string) bool { return strings.HasPrefix(s, "🧹") }) {
		t.Error("«Очистить купленное» with nothing bought")
	}
	if urls := webAppURLs(sent.ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?shopping=1"}) {
		t.Errorf("open button %v", urls)
	}
	check := callback{op: opShopCheck, id: 1}.String()
	if !slices.Contains(callbackData(sent.ReplyMarkup), check) {
		t.Fatalf("no toggle %q: %v", check, callbackData(sent.ReplyMarkup))
	}

	list := cardMessage(alice, 40)
	e.api.reset()
	e.handle(callbackUpdate(alice, check, list))
	if !e.item(t, 1).Checked {
		t.Fatal("item not checked")
	}
	edit := e.api.lastEdit(t)
	if edit.MessageID != 40 || !strings.Contains(edit.Text, "Нужно купить: 2 · куплено: 1") {
		t.Errorf("list not refreshed in place: %+v", edit)
	}
	texts = buttonTexts(edit.ReplyMarkup)
	if !slices.Contains(texts, "☑ Молоко") || !slices.Contains(texts, "🧹 Очистить купленное (1)") || slices.Contains(texts, "☐ Молоко — "+milk.Format()) {
		t.Errorf("buttons after checking %v", texts)
	}
	if got := e.api.answers(); !slices.Equal(got, []string{"☑ Куплено"}) {
		t.Errorf("answers %v", got)
	}

	// A tap on an out-of-date list sets the state the button showed, so it
	// never undoes the partner's change.
	e.handle(callbackUpdate(alice, check, list))
	if !e.item(t, 1).Checked {
		t.Error("a second «check» unchecked the item")
	}

	e.api.reset()
	e.handle(callbackUpdate(alice, callback{op: opShopUncheck, id: 1}.String(), list))
	if e.item(t, 1).Checked {
		t.Error("item not unchecked")
	}
	if got := e.api.answers(); !slices.Equal(got, []string{"↩️ Снова в списке"}) {
		t.Errorf("answers %v", got)
	}

	// An item removed meanwhile.
	e.api.reset()
	e.handle(callbackUpdate(alice, callback{op: opShopCheck, id: 99}.String(), list))
	if got := e.api.answers(); !slices.Equal(got, []string{"Этой позиции уже нет в списке"}) {
		t.Errorf("answers %v", got)
	}
	if e.api.count("EditMessageText") != 1 {
		t.Error("list not refreshed after a stale tap")
	}

	// Clearing removes only the bought items.
	e.handle(callbackUpdate(alice, callback{op: opShopCheck, id: 1}.String(), list))
	e.handle(callbackUpdate(alice, callback{op: opShopCheck, id: 2}.String(), list))
	e.api.reset()
	e.handle(callbackUpdate(alice, callback{op: opShopClear}.String(), list))
	if left := e.svc.shopping.snapshot(); len(left) != 1 || left[0].ID != 3 {
		t.Errorf("left %+v", left)
	}
	if got := e.api.answers(); !slices.Equal(got, []string{"🧹 Убрали купленное: 2"}) {
		t.Errorf("answers %v", got)
	}
	e.api.reset()
	e.handle(callbackUpdate(alice, callback{op: opShopClear}.String(), list))
	if got := e.api.answers(); !slices.Equal(got, []string{"Купленного нет"}) {
		t.Errorf("answers %v", got)
	}
}

func TestShopEmptyAndAllBought(t *testing.T) {
	e := newTestEnv(t)
	e.handle(commandUpdate(alice, "shop"))
	sent := e.api.lastSent(t)
	if !strings.Contains(sent.Text, "Пока пусто") {
		t.Errorf("empty list %q", sent.Text)
	}
	if texts := buttonTexts(sent.ReplyMarkup); !slices.Equal(texts, []string{"Открыть список ✨", "🔄"}) {
		t.Errorf("empty list buttons %v", texts)
	}

	e.addShopping(t, domain.ShoppingDraft{Name: "Хлеб"})
	e.handle(callbackUpdate(alice, callback{op: opShopCheck, id: 1}.String(), cardMessage(alice, 40)))
	if text := e.api.lastEdit(t).Text; !strings.Contains(text, "Всё куплено") {
		t.Errorf("all bought %q", text)
	}

	// Without a Mini App URL there is no web_app button.
	e.app.web.set("")
	e.handle(commandUpdate(alice, "shop"))
	if urls := webAppURLs(e.api.lastSent(t).ReplyMarkup); len(urls) != 0 {
		t.Errorf("web_app button without a URL: %v", urls)
	}
}

func TestShopPagination(t *testing.T) {
	e := newTestEnv(t)
	var drafts []domain.ShoppingDraft
	for i := range shopPageSize + 5 {
		drafts = append(drafts, domain.ShoppingDraft{Name: fmt.Sprintf("Позиция %02d", i+1)})
	}
	for i := range shopCheckedShown + 3 {
		drafts = append(drafts, domain.ShoppingDraft{Name: fmt.Sprintf("Куплено %02d", i+1)})
	}
	items := e.addShopping(t, drafts...)
	for _, it := range items[shopPageSize+5:] {
		if _, err := e.svc.shopping.Update(context.Background(), alice, it.ID, domain.ShoppingPatch{Checked: domain.Some(true)}); err != nil {
			t.Fatal(err)
		}
	}

	e.handle(commandUpdate(alice, "shop"))
	sent := e.api.lastSent(t)
	texts := buttonTexts(sent.ReplyMarkup)
	unchecked := slices.DeleteFunc(slices.Clone(texts), func(s string) bool { return !strings.HasPrefix(s, "☐") })
	checked := slices.DeleteFunc(slices.Clone(texts), func(s string) bool { return !strings.HasPrefix(s, "☑") })
	if len(unchecked) != shopPageSize || len(checked) != shopCheckedShown {
		t.Errorf("first page: %d unchecked, %d checked buttons", len(unchecked), len(checked))
	}
	if !slices.Contains(texts, "1/2") || !slices.Contains(texts, "▶️") || !slices.Contains(texts, "🧹 Очистить купленное (13)") {
		t.Errorf("first page buttons %v", texts)
	}
	if !strings.Contains(sent.Text, "Ещё куплено: 3") {
		t.Errorf("list text %q", sent.Text)
	}
	if n := len(texts); n > 100 {
		t.Errorf("%d buttons, Telegram allows at most 100", n)
	}

	list := cardMessage(alice, 40)
	e.handle(callbackUpdate(alice, callback{op: opShopList, page: 1}.String(), list))
	edit := e.api.lastEdit(t)
	texts = buttonTexts(edit.ReplyMarkup)
	if !slices.Contains(texts, "☐ Позиция 31") || slices.Contains(texts, "☐ Позиция 01") || !slices.Contains(texts, "◀️") {
		t.Errorf("second page %v", texts)
	}
	// Toggling on the second page keeps the page.
	toggle := callback{op: opShopCheck, id: 33, page: 1}.String()
	if !slices.Contains(callbackData(edit.ReplyMarkup), toggle) {
		t.Fatalf("no toggle %q on the second page", toggle)
	}
	e.handle(callbackUpdate(alice, toggle, list))
	if texts := buttonTexts(e.api.lastEdit(t).ReplyMarkup); !slices.Contains(texts, "2/2") {
		t.Errorf("toggle left the page: %v", texts)
	}
	for _, data := range callbackData(edit.ReplyMarkup) {
		if len(data) > maxCallbackData {
			t.Errorf("payload %q too long", data)
		}
	}
}
