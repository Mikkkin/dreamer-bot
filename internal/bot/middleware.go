package bot

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// Update kinds used in logs. Logs of dropped updates carry only the kind and
// the user ID, never content.
const (
	kindMessage      = "message"
	kindCallback     = "callback_query"
	kindMyChatMember = "my_chat_member"
	kindOther        = "other"
)

// origin is who sent an update and in which chat.
type origin struct {
	kind string
	user *models.User
	chat *models.Chat
}

func originOf(u *models.Update) origin {
	switch {
	case u.Message != nil:
		return origin{kind: kindMessage, user: u.Message.From, chat: &u.Message.Chat}
	case u.CallbackQuery != nil:
		o := origin{kind: kindCallback, user: &u.CallbackQuery.From}
		switch m := u.CallbackQuery.Message; {
		case m.Message != nil:
			o.chat = &m.Message.Chat
		case m.InaccessibleMessage != nil:
			o.chat = &m.InaccessibleMessage.Chat
		}
		return o
	case u.MyChatMember != nil:
		return origin{kind: kindMyChatMember, user: &u.MyChatMember.From, chat: &u.MyChatMember.Chat}
	}
	return origin{kind: kindOther}
}

func (o origin) userID() int64 {
	if o.user == nil {
		return 0
	}
	return o.user.ID
}

// private reports whether the update comes from a person's private chat
// with the bot (and not, say, from an inline message without a chat).
func (o origin) private() bool {
	return o.chat != nil && o.chat.Type == models.ChatTypePrivate &&
		o.user != nil && !o.user.IsBot && o.chat.ID == o.user.ID
}

// recoverer turns a handler panic into a log line. The library runs
// handlers without a recover, so a panic would otherwise kill the process.
func recoverer(log *slog.Logger) tg.Middleware {
	return func(next tg.HandlerFunc) tg.HandlerFunc {
		return func(ctx context.Context, b *tg.Bot, u *models.Update) {
			defer func() {
				if r := recover(); r != nil {
					log.Error("bot: handler panic", "update_id", u.ID, "kind", originOf(u).kind,
						"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				}
			}()
			next(ctx, b, u)
		}
	}
}

// accessFilter silently drops everything that is not a private chat with a
// whitelisted user: no reply and no answerCallbackQuery, so strangers learn
// nothing. When the bot is added to a group or a channel it leaves at once.
// With an empty whitelist (setup mode) private chats pass on to setupGate.
func accessFilter(wl auth.Whitelist, api messenger, log *slog.Logger) tg.Middleware {
	return func(next tg.HandlerFunc) tg.HandlerFunc {
		return func(ctx context.Context, b *tg.Bot, u *models.Update) {
			o := originOf(u)
			switch {
			case o.kind == kindMyChatMember && o.chat.Type != models.ChatTypePrivate:
				leaveIfMember(ctx, api, log, u.MyChatMember)
				return
			case o.kind == kindOther || !o.private():
				log.Info("bot: dropped update", "reason", "not_private", "kind", o.kind, "user_id", o.userID())
				return
			case !wl.Empty() && !wl.Allows(domain.UserID(o.userID())):
				log.Info("bot: dropped update", "reason", "not_whitelisted", "kind", o.kind, "user_id", o.userID())
				return
			}
			next(ctx, b, u)
		}
	}
}

func leaveIfMember(ctx context.Context, api messenger, log *slog.Logger, m *models.ChatMemberUpdated) {
	switch m.NewChatMember.Type {
	case models.ChatMemberTypeMember, models.ChatMemberTypeAdministrator,
		models.ChatMemberTypeRestricted, models.ChatMemberTypeOwner:
	default:
		return // already out of the chat
	}
	log.Warn("bot: added to a non-private chat, leaving", "chat_type", string(m.Chat.Type), "user_id", m.From.ID)
	if _, err := api.LeaveChat(ctx, &tg.LeaveChatParams{ChatID: m.Chat.ID}); err != nil {
		log.Error("bot: leave chat failed", "err", err)
	}
}

// setupGate is active while the whitelist is empty. The only thing the bot
// does then is tell the caller their own Telegram ID in reply to /start, so
// the owner can put it into ALLOWED_USER_IDS.
func setupGate(wl auth.Whitelist, api messenger, username string, log *slog.Logger) tg.Middleware {
	return func(next tg.HandlerFunc) tg.HandlerFunc {
		return func(ctx context.Context, b *tg.Bot, u *models.Update) {
			if !wl.Empty() {
				next(ctx, b, u)
				return
			}
			if u.Message == nil || u.Message.From == nil {
				return
			}
			if cmd, ok := parseCommand(u.Message.Text, username); !ok || cmd != cmdStart {
				return
			}
			id := u.Message.From.ID
			log.Info("bot: setup mode, told a user their ID", "user_id", id)
			if _, err := sendHTML(ctx, api, u.Message.Chat.ID, renderSetupReply(id), nil); err != nil {
				log.Error("bot: setup reply failed", "err", err)
			}
		}
	}
}

func renderSetupReply(id int64) string {
	h := newHTML(maxMessageLen)
	h.Text("Ваш Telegram ID: ").Code(strconv.FormatInt(id, 10)).NL().NL()
	h.Text("Бот пока в режиме настройки. Добавьте этот ID в переменную ").Code("ALLOWED_USER_IDS")
	h.Text(" (два ID — через запятую) и перезапустите бота.")
	return h.String()
}

// toucher records whitelisted users (names and the fact that they have a
// private chat with the bot, i.e. can receive notifications). Profiles are
// written at most once per touchEvery unless they change.
type toucher struct {
	users service.Users
	now   func() time.Time
	log   *slog.Logger

	mu   sync.Mutex
	seen map[domain.UserID]touchMark
}

type touchMark struct {
	profile domain.User
	at      time.Time
}

const touchEvery = time.Hour

func newToucher(users service.Users, now func() time.Time, log *slog.Logger) *toucher {
	return &toucher{users: users, now: now, log: log, seen: make(map[domain.UserID]touchMark)}
}

func (t *toucher) middleware(next tg.HandlerFunc) tg.HandlerFunc {
	return func(ctx context.Context, b *tg.Bot, u *models.Update) {
		if o := originOf(u); o.user != nil && (o.kind == kindMessage || o.kind == kindCallback) {
			t.touch(ctx, o.user)
		}
		next(ctx, b, u)
	}
}

func (t *toucher) touch(ctx context.Context, from *models.User) {
	profile := domain.User{
		ID:        domain.UserID(from.ID),
		FirstName: from.FirstName,
		LastName:  from.LastName,
		Username:  from.Username,
		HasChat:   true,
	}
	now := t.now()
	t.mu.Lock()
	last, ok := t.seen[profile.ID]
	fresh := ok && last.profile == profile && now.Sub(last.at) < touchEvery
	t.mu.Unlock()
	if fresh {
		return
	}
	profile.UpdatedAt = now
	if err := t.users.Touch(ctx, profile); err != nil {
		t.log.Error("bot: touch user failed", "user_id", profile.ID, "err", err)
		return
	}
	profile.UpdatedAt = time.Time{}
	t.mu.Lock()
	t.seen[profile.ID] = touchMark{profile: profile, at: now}
	t.mu.Unlock()
}
