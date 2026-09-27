package bot

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// chain runs the global middlewares in production order around a handler
// that records whether it was reached.
type chain struct {
	api     *fakeAPI
	logs    *bytes.Buffer
	reached int
	run     tg.HandlerFunc
}

func newChain(wl auth.Whitelist) *chain {
	c := &chain{api: newFakeAPI(), logs: &bytes.Buffer{}}
	log := slog.New(slog.NewTextHandler(c.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	final := func(context.Context, *tg.Bot, *models.Update) { c.reached++ }
	c.run = recoverer(log)(accessFilter(wl, c.api, log)(setupGate(wl, c.api, botUsername, log)(final)))
	return c
}

func (c *chain) handle(u *models.Update) { c.run(context.Background(), nil, u) }

func groupMessage(user domain.UserID, text string) *models.Update {
	return &models.Update{ID: 5, Message: &models.Message{
		ID: 1, From: userOf(user), Text: text,
		Chat: models.Chat{ID: -100123, Type: models.ChatTypeSupergroup, Title: "Семья"},
	}}
}

func memberUpdate(chatType models.ChatType, status models.ChatMemberType) *models.Update {
	return &models.Update{ID: 6, MyChatMember: &models.ChatMemberUpdated{
		Chat:          models.Chat{ID: -100500, Type: chatType},
		From:          *userOf(stranger),
		NewChatMember: models.ChatMember{Type: status},
	}}
}

func TestAccessFilterDropsSilently(t *testing.T) {
	secret := "секретное сообщение"
	tests := []struct {
		name string
		upd  *models.Update
	}{
		{"stranger in private chat", textUpdate(stranger, secret)},
		{"stranger presses a button", callbackUpdate(stranger, "w:o:1", cardMessage(stranger, 1))},
		{"whitelisted user in a group", groupMessage(alice, secret)},
		{"stranger in a group", groupMessage(stranger, secret)},
		{"callback from an inline message", callbackUpdate(alice, "w:o:1", nil)},
		{"message without sender", &models.Update{Message: &models.Message{Chat: privateChat(alice), Text: secret}}},
		{"bot account", &models.Update{Message: &models.Message{
			From: &models.User{ID: int64(alice), IsBot: true}, Chat: privateChat(alice), Text: secret}}},
		{"sender differs from the private chat", &models.Update{Message: &models.Message{
			From: userOf(alice), Chat: privateChat(bob), Text: secret}}},
		{"channel post", &models.Update{ChannelPost: &models.Message{Text: secret}}},
		{"my_chat_member: removed from a group", memberUpdate(models.ChatTypeGroup, models.ChatMemberTypeLeft)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newChain(testWhitelist())
			c.handle(tt.upd)
			if c.reached != 0 {
				t.Error("update reached the handlers")
			}
			if calls := c.api.all(); len(calls) != 0 {
				t.Errorf("dropped update caused API calls: %+v", calls)
			}
			if strings.Contains(c.logs.String(), secret) {
				t.Error("message content was logged")
			}
		})
	}
}

func TestAccessFilterLeavesGroups(t *testing.T) {
	for _, chatType := range []models.ChatType{models.ChatTypeGroup, models.ChatTypeSupergroup, models.ChatTypeChannel} {
		for _, status := range []models.ChatMemberType{models.ChatMemberTypeMember, models.ChatMemberTypeAdministrator} {
			c := newChain(testWhitelist())
			c.handle(memberUpdate(chatType, status))
			leaves := c.api.of("LeaveChat")
			if len(leaves) != 1 || leaves[0].(*tg.LeaveChatParams).ChatID != int64(-100500) {
				t.Errorf("%s/%s: LeaveChat calls %+v", chatType, status, leaves)
			}
			if c.reached != 0 || len(c.api.all()) != 1 {
				t.Errorf("%s/%s: unexpected activity", chatType, status)
			}
		}
	}
	// Setup mode does not change that.
	c := newChain(auth.NewWhitelist(nil))
	c.handle(memberUpdate(models.ChatTypeGroup, models.ChatMemberTypeMember))
	if c.api.count("LeaveChat") != 1 {
		t.Error("bot stayed in a group in setup mode")
	}
}

func TestAccessFilterPassesWhitelisted(t *testing.T) {
	c := newChain(testWhitelist())
	c.handle(textUpdate(alice, "Лампа"))
	c.handle(callbackUpdate(bob, "x", cardMessage(bob, 1)))
	if c.reached != 2 {
		t.Errorf("reached %d, want 2", c.reached)
	}
	if len(c.api.all()) != 0 {
		t.Error("middlewares talked to Telegram for allowed updates")
	}
}

func TestSetupModeRepliesWithCallerIDOnly(t *testing.T) {
	c := newChain(auth.NewWhitelist(nil))
	c.handle(commandUpdate(stranger, "start"))
	c.handle(commandUpdate(alice, "start"))

	sent := c.api.of("SendMessage")
	if len(sent) != 2 {
		t.Fatalf("%d replies, want 2", len(sent))
	}
	for i, id := range []domain.UserID{stranger, alice} {
		p := sent[i].(*tg.SendMessageParams)
		if p.ChatID != int64(id) {
			t.Errorf("reply %d went to %v", i, p.ChatID)
		}
		want := "Ваш Telegram ID: <code>" + strconv.FormatInt(int64(id), 10) + "</code>"
		if !strings.Contains(p.Text, want) || !strings.Contains(p.Text, "ALLOWED_USER_IDS") {
			t.Errorf("reply %d = %q", i, p.Text)
		}
		for _, other := range []domain.UserID{stranger, alice, bob} {
			if other != id && strings.Contains(p.Text, strconv.FormatInt(int64(other), 10)) {
				t.Errorf("reply to %d leaks the ID %d", id, other)
			}
		}
	}
	if c.reached != 0 {
		t.Error("setup mode let an update through to the handlers")
	}
}

func TestSetupModeIgnoresEverythingElse(t *testing.T) {
	c := newChain(auth.NewWhitelist(nil))
	c.handle(textUpdate(alice, "привет"))
	c.handle(commandUpdate(alice, "list"))
	c.handle(callbackUpdate(alice, "x", cardMessage(alice, 1)))
	c.handle(groupMessage(alice, "/start"))
	start := commandUpdate(alice, "start")
	start.Message.Text = "/start@other_bot"
	c.handle(start)
	if calls := c.api.all(); len(calls) != 0 {
		t.Errorf("setup mode answered: %+v", calls)
	}
	if c.reached != 0 {
		t.Error("setup mode let an update through")
	}
}

func TestRecovererSurvivesPanics(t *testing.T) {
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, nil))
	h := recoverer(log)(func(context.Context, *tg.Bot, *models.Update) { panic("boom") })
	h(context.Background(), nil, textUpdate(alice, "x"))
	if !strings.Contains(logs.String(), "handler panic") {
		t.Error("panic not logged")
	}
}

func TestToucherRecordsProfilesSparingly(t *testing.T) {
	clk := newClock()
	users := &fakeUsers{items: map[domain.UserID]domain.User{}}
	tc := newToucher(users, clk.Now, slog.New(slog.DiscardHandler))
	reached := 0
	h := tc.middleware(func(context.Context, *tg.Bot, *models.Update) { reached++ })

	h(context.Background(), nil, textUpdate(alice, "a"))
	h(context.Background(), nil, textUpdate(alice, "b"))
	if users.touchCount() != 1 {
		t.Fatalf("touched %d times, want 1", users.touchCount())
	}
	u := users.touches[0]
	if u.ID != alice || !u.HasChat || u.FirstName != "Дима" {
		t.Errorf("profile %+v", u)
	}

	renamed := textUpdate(alice, "c")
	renamed.Message.From.FirstName = "Дмитрий"
	h(context.Background(), nil, renamed)
	clk.Advance(touchEvery + time.Second)
	h(context.Background(), nil, renamed)
	if users.touchCount() != 3 {
		t.Errorf("touched %d times, want 3 (rename, then hourly refresh)", users.touchCount())
	}
	if reached != 4 {
		t.Errorf("handler reached %d times", reached)
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		in     string
		cmd    string
		isCmd  bool
		reason string
	}{
		{"/start", "start", true, "plain"},
		{"/Start extra words", "start", true, "case and args"},
		{"/list@" + botUsername, "list", true, "addressed to us"},
		{"/list@" + strings.ToUpper(botUsername), "list", true, "username is case-insensitive"},
		{"/list@other_bot", "", true, "addressed to another bot"},
		{"/home/user/file", "", false, "path"},
		{"/", "", false, "slash only"},
		{"hello /start", "", false, "not at the start"},
		{"/привет", "", false, "non-ASCII"},
	}
	for _, tt := range tests {
		cmd, ok := parseCommand(tt.in, botUsername)
		if cmd != tt.cmd || ok != tt.isCmd {
			t.Errorf("%s: parseCommand(%q) = %q, %v", tt.reason, tt.in, cmd, ok)
		}
	}
}
