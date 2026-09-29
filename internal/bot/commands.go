package bot

import (
	"context"
	"strings"
	"unicode"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const (
	cmdStart   = "start"
	cmdHelp    = "help"
	cmdList    = "list"
	cmdRecipes = "recipes"
	cmdCook    = "cook"
	cmdShop    = "shop"
	cmdStats   = "stats"
	cmdCancel  = "cancel"
)

// botCommands is the command menu registered with setMyCommands.
func botCommands(setupMode bool) []models.BotCommand {
	if setupMode {
		return []models.BotCommand{{Command: cmdStart, Description: "Узнать свой Telegram ID"}}
	}
	return []models.BotCommand{
		{Command: cmdStart, Description: "Начало и кнопка приложения"},
		{Command: cmdList, Description: "Наши желания"},
		{Command: cmdRecipes, Description: "Что приготовить: все рецепты"},
		{Command: cmdCook, Description: "Случайный рецепт на сегодня"},
		{Command: cmdShop, Description: "Список покупок"},
		{Command: cmdStats, Description: "Статистика"},
		{Command: cmdHelp, Description: "Как пользоваться"},
		{Command: cmdCancel, Description: "Отменить ввод или черновик"},
	}
}

// parseCommand recognises "/name" and "/name@thisbot" at the start of text.
// It returns the lower-cased name; a command addressed to another bot is
// reported as a command with an empty name. Text such as "/home/user" is
// not a command.
func parseCommand(text, username string) (string, bool) {
	rest, ok := strings.CutPrefix(text, "/")
	if !ok {
		return "", false
	}
	if i := strings.IndexFunc(rest, unicode.IsSpace); i >= 0 {
		rest = rest[:i]
	}
	name, target, addressed := strings.Cut(rest, "@")
	if !isCommandName(name) {
		return "", false
	}
	if addressed && !strings.EqualFold(target, username) {
		return "", true
	}
	return strings.ToLower(name), true
}

func isCommandName(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func (a *app) onCommand(ctx context.Context, m *models.Message, cmd string) {
	chatID := m.Chat.ID
	user := domain.UserID(m.From.ID)
	if cmd != cmdCancel {
		a.dropPrompts(ctx, user)
	}
	switch cmd {
	case cmdStart:
		a.cmdStart(ctx, m)
	case cmdHelp:
		a.cmdHelp(ctx, chatID)
	case cmdList:
		a.sendWishList(ctx, chatID)
	case cmdRecipes:
		a.sendRecipeList(ctx, chatID)
	case cmdCook:
		a.sendRandomRecipe(ctx, chatID, 0)
	case cmdShop:
		a.sendShopping(ctx, chatID)
	case cmdStats:
		a.cmdStats(ctx, chatID)
	case cmdCancel:
		a.cmdCancel(ctx, chatID, user)
	default:
		a.say(ctx, chatID, "Такой команды я не знаю. Загляните в /help 🙂")
	}
}

func (a *app) cmdStart(ctx context.Context, m *models.Message) {
	h := newHTML(maxMessageLen)
	h.Text("Привет")
	if name := strings.TrimSpace(m.From.FirstName); name != "" {
		h.Text(", " + name)
	}
	h.Text("! ✨").NL().Text("Здесь живут наши желания и рецепты.").NL().NL()
	h.Text("Чтобы добавить желание, просто пришлите мне текст, ссылку или фото, например: ")
	h.Italic("Поездка в Токио 1200€").Text(". Я соберу карточку, а вы проверите её и нажмёте «✅ Сохранить».").NL().NL()
	h.Text("/list — желания · /recipes — рецепты").NL()
	h.Text("/cook — что приготовить · /shop — покупки").NL()
	h.Text("/stats — статистика").NL()
	h.Text("/help — подробнее")
	if _, err := sendHTML(ctx, a.api, m.Chat.ID, h.String(), openKeyboard("Открыть ✨", a.web.url())); err != nil {
		a.log.Warn("bot: send welcome failed", "err", err)
	}
}

func (a *app) cmdHelp(ctx context.Context, chatID int64) {
	h := newHTML(maxMessageLen)
	h.Bold("Как пользоваться").NL().NL()
	h.Text("➕ ").Bold("Добавить").Text(": пришлите текст, ссылку или фото (можно альбомом). ")
	h.Text("Первая строка станет названием, остальное — заметкой. Цену с валютой я узнаю сам: ")
	h.Code("1200€").Text(", ").Code("$50").Text(", ").Code("15 000 ₽").Text(".").NL()
	h.Text("🍳 Рецепт: в карточке черновика нажмите «🍳 Рецепт», там же — кухня и тип блюда.").NL()
	h.Text("✅ Проверьте карточку и нажмите «Сохранить».").NL().NL()
	h.Text("💰 На карточке желания в статусе «Копим» есть «Отложить» — так копится сумма.").NL()
	h.Text("🍳 «Приготовили» на карточке рецепта сохраняет историю и оценку, «🛒 В покупки» — ингредиенты.").NL().NL()
	h.Text("/list — наши желания по статусам").NL()
	h.Text("/recipes — все рецепты").NL()
	h.Text("/cook — случайный рецепт на сегодня").NL()
	h.Text("/shop — список покупок").NL()
	h.Text("/stats — статистика").NL()
	h.Text("/cancel — отменить ввод или черновик").NL().NL()
	h.Text("Всё остальное — в приложении ✨")
	if _, err := sendHTML(ctx, a.api, chatID, h.String(), openKeyboard("Открыть ✨", a.web.url())); err != nil {
		a.log.Warn("bot: send help failed", "err", err)
	}
}

func (a *app) cmdStats(ctx context.Context, chatID int64) {
	s, err := a.svc.Stats.Compute(ctx)
	if err != nil {
		a.say(ctx, chatID, a.userError(err, "compute stats"))
		return
	}
	if _, err := sendHTML(ctx, a.api, chatID, renderStats(s), nil); err != nil {
		a.log.Warn("bot: send stats failed", "err", err)
	}
}

// cmdCancel steps back one level: an awaited answer first, then the draft.
func (a *app) cmdCancel(ctx context.Context, chatID int64, user domain.UserID) {
	if a.dropPrompts(ctx, user) {
		a.say(ctx, chatID, "Хорошо, оставил как было 👌")
		return
	}
	d, ok := a.drafts.get(user)
	if !ok {
		a.say(ctx, chatID, "Отменять нечего 🙂")
		return
	}
	a.drafts.remove(user, d.id)
	a.retireCard(ctx, d, "✖️ Черновик удалён")
	a.say(ctx, chatID, "Готово, черновик удалён.")
}
