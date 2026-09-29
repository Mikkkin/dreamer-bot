package bot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
)

func newTestBot(t *testing.T, o Options) (*Bot, *fakeAPI, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	o.Token = testToken
	o.Log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if err := o.normalize(); err != nil {
		t.Fatal(err)
	}
	api := newFakeAPI()
	b := newBot(o)
	b.bind(api, botUsername)
	return b, api, logs
}

func menuCalls(api *fakeAPI) []*tg.SetChatMenuButtonParams {
	var out []*tg.SetChatMenuButtonParams
	for _, p := range api.of("SetChatMenuButton") {
		out = append(out, p.(*tg.SetChatMenuButtonParams))
	}
	return out
}

func TestConfigureSetsCommandsAndMenus(t *testing.T) {
	b, api, _ := newTestBot(t, Options{Whitelist: testWhitelist(), WebAppURL: testWebApp})
	b.configure(context.Background())

	if hooks := api.of("DeleteWebhook"); len(hooks) != 1 || hooks[0].(*tg.DeleteWebhookParams).DropPendingUpdates {
		t.Errorf("deleteWebhook calls %+v", hooks)
	}
	cmds := api.of("SetMyCommands")
	if len(cmds) != 1 || len(cmds[0].(*tg.SetMyCommandsParams).Commands) != 8 {
		t.Fatalf("setMyCommands %+v", cmds)
	}
	if !slices.ContainsFunc(cmds[0].(*tg.SetMyCommandsParams).Commands, func(c models.BotCommand) bool { return c.Command == cmdShop }) {
		t.Error("/shop is not in the command menu")
	}
	menus := menuCalls(api)
	if len(menus) != 3 {
		t.Fatalf("%d menu calls, want default + 2 users", len(menus))
	}
	wantChats := []any{nil, int64(alice), int64(bob)}
	for i, m := range menus {
		if m.ChatID != wantChats[i] {
			t.Errorf("menu %d chat %v, want %v", i, m.ChatID, wantChats[i])
		}
		wa, ok := m.MenuButton.(models.MenuButtonWebApp)
		if !ok || wa.Type != models.MenuButtonTypeWebApp || wa.WebApp.URL != testWebApp || wa.Text != menuButtonText {
			t.Errorf("menu %d = %#v", i, m.MenuButton)
		}
	}
}

func TestConfigureWithoutURLRestoresCommandsMenu(t *testing.T) {
	b, api, _ := newTestBot(t, Options{Whitelist: testWhitelist()})
	b.configure(context.Background())
	for _, m := range menuCalls(api) {
		if c, ok := m.MenuButton.(models.MenuButtonCommands); !ok || c.Type != models.MenuButtonTypeCommands {
			t.Errorf("menu %#v, want the commands menu", m.MenuButton)
		}
	}
}

func TestConfigureInSetupMode(t *testing.T) {
	b, api, _ := newTestBot(t, Options{Whitelist: auth.NewWhitelist(nil), WebAppURL: testWebApp})
	b.configure(context.Background())
	cmds := api.of("SetMyCommands")[0].(*tg.SetMyCommandsParams).Commands
	if len(cmds) != 1 || cmds[0].Command != cmdStart {
		t.Errorf("setup commands %+v", cmds)
	}
	if n := api.count("SetChatMenuButton"); n != 0 {
		t.Errorf("setup mode set %d menu buttons", n)
	}
}

func TestSetWebAppURL(t *testing.T) {
	b, api, _ := newTestBot(t, Options{Whitelist: testWhitelist()})

	// Before Run the URL is only remembered.
	b.SetWebAppURL(context.Background(), "https://first.trycloudflare.com/")
	if api.count("SetChatMenuButton") != 0 {
		t.Fatal("menu set before Run configured the bot")
	}
	if b.web.url() != "https://first.trycloudflare.com/" {
		t.Fatal("URL not stored")
	}

	b.configure(context.Background())
	api.reset()
	b.SetWebAppURL(context.Background(), "https://second.trycloudflare.com/")
	menus := menuCalls(api)
	if len(menus) != 3 {
		t.Fatalf("%d menu calls after a URL change", len(menus))
	}
	for _, m := range menus {
		if m.MenuButton.(models.MenuButtonWebApp).WebApp.URL != "https://second.trycloudflare.com/" {
			t.Errorf("menu %#v", m.MenuButton)
		}
	}
	if got := b.web.wishLink(5); got != "https://second.trycloudflare.com/?wish=5" {
		t.Errorf("deep link %q", got)
	}

	api.reset()
	for _, bad := range []string{"http://plain.example/", "javascript:alert(1)", "https://user:pw@x.example/", "/relative"} {
		b.SetWebAppURL(context.Background(), bad)
	}
	if api.count("SetChatMenuButton") != 0 || b.web.url() != "https://second.trycloudflare.com/" {
		t.Error("an invalid URL was applied")
	}
}

func TestWebAppDeepLinks(t *testing.T) {
	var w webApp
	if w.wishLink(1) != "" || w.url() != "" {
		t.Error("unknown URL must produce no links")
	}
	w.set("https://dreams.example/app/")
	if got := w.recipeLink(42); got != "https://dreams.example/app/?recipe=42" {
		t.Errorf("recipe link %q", got)
	}
	w.set("https://dreams.example")
	if got := w.wishLink(7); got != "https://dreams.example?wish=7" {
		t.Errorf("wish link %q", got)
	}
}

func TestLibraryErrorsAreSanitised(t *testing.T) {
	b, _, logs := newTestBot(t, Options{Whitelist: testWhitelist()})
	b.onLibraryError(errors.New(`error do request for method getUpdates, Post "https://api.telegram.org/bot` + testToken + `/getUpdates": EOF`))
	b.onLibraryError(errors.New(`error decode update 17, skipped, {"message":{"text":"секрет"}}, json: bad`))
	b.onLibraryError(errors.New(`error get updates, error decode response body for method getUpdates, {"result":[{"message":{"text":"секрет"}}]}, EOF`))
	out := logs.String()
	assertNoToken(t, out)
	if strings.Contains(out, "секрет") {
		t.Errorf("user content logged: %s", out)
	}
	if !strings.Contains(out, "content omitted") {
		t.Errorf("sanitised errors not marked: %s", out)
	}
	if got := sanitizeLibraryError(strings.Repeat("x", 2000)); utf16Len(got) > maxLibraryErrLen {
		t.Errorf("long error kept %d units", utf16Len(got))
	}
}

func TestUnhandledUpdatesAreNotLogged(t *testing.T) {
	b, _, logs := newTestBot(t, Options{Whitelist: testWhitelist()})
	b.unhandled(context.Background(), nil, textUpdate(alice, "личное сообщение"))
	if strings.Contains(logs.String(), "личное") {
		t.Error("default handler logged message content")
	}
}

func TestOptionsNormalize(t *testing.T) {
	o := Options{Token: testToken}
	if err := o.normalize(); err != nil {
		t.Fatal(err)
	}
	if o.Log == nil || o.DefaultCurrency != "EUR" || o.MaxImageBytes != defaultImageMax {
		t.Errorf("defaults %+v", o)
	}
	for name, bad := range map[string]Options{
		"no token":     {},
		"currency":     {Token: testToken, DefaultCurrency: "BTC"},
		"insecure url": {Token: testToken, WebAppURL: "http://x.example/"},
	} {
		if err := bad.normalize(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAttachAndDispatch(t *testing.T) {
	b, api, _ := newTestBot(t, Options{Whitelist: testWhitelist()})
	upd := commandUpdate(alice, "help")
	b.dispatch(context.Background(), nil, upd) // before Attach: ignored
	if len(api.all()) != 0 {
		t.Fatal("update handled before Attach")
	}
	svc := newFakeServices(newClock().Now)
	b.Attach(svc.services())
	b.touchMiddleware(b.dispatch)(context.Background(), nil, upd)
	if api.count("SendMessage") != 1 {
		t.Errorf("help not sent: %+v", api.all())
	}
	if svc.users.touchCount() != 1 {
		t.Error("user not touched")
	}
	if err := b.Run(context.Background()); err == nil {
		t.Error("Run without a Telegram client must fail")
	}
}
