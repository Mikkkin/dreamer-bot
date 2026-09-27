// Package auth decides who may use the service: the whitelist of Telegram
// user IDs, Mini App initData validation and signed media URLs. All secrets
// here are derived from the bot token.
package auth

import (
	"slices"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Whitelist is the immutable set of Telegram users allowed to use the bot.
type Whitelist struct {
	ids []domain.UserID
}

// NewWhitelist copies ids into a new whitelist.
func NewWhitelist(ids []domain.UserID) Whitelist {
	return Whitelist{ids: slices.Clone(ids)}
}

// Allows reports whether the user is whitelisted. Zero and negative IDs are
// never allowed (Telegram user IDs are positive).
func (w Whitelist) Allows(id domain.UserID) bool {
	return id > 0 && slices.Contains(w.ids, id)
}

// IDs returns a copy of the whitelisted IDs.
func (w Whitelist) IDs() []domain.UserID { return slices.Clone(w.ids) }

// Empty reports whether nobody is whitelisted (setup mode).
func (w Whitelist) Empty() bool { return len(w.ids) == 0 }
