// Package logging builds the service logger. Every record passes through a
// redactor so that secrets (above all the bot token, which also appears in
// Telegram file-download URLs and therefore in *url.Error values) can never
// reach the log output, whichever package produced the message.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"slices"
	"strings"
)

// Redacted replaces every occurrence of a secret.
const Redacted = "[REDACTED]"

// New returns a JSON logger at the given level that redacts every non-empty
// secret from the message, attribute keys and all attribute values:
// strings, errors, fmt.Stringers, members of groups, and arbitrary values
// that the handler would render as JSON.
func New(w io.Writer, level slog.Level, secrets ...string) *slog.Logger {
	r := newRedactor(secrets)
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: r.replaceAttr,
	}))
}

type redactor struct {
	secrets  []string
	replacer *strings.Replacer
}

func newRedactor(secrets []string) *redactor {
	var all []string
	for _, s := range secrets {
		if s == "" {
			continue
		}
		// URLs may carry the secret percent-encoded (":" becomes "%3A").
		all = append(all, s, url.QueryEscape(s), url.PathEscape(s))
	}
	if len(all) == 0 {
		return &redactor{}
	}
	slices.Sort(all)
	all = slices.Compact(all)
	// Longest first: strings.Replacer prefers earlier arguments at the same
	// position, so a secret that contains another one is replaced whole.
	slices.SortStableFunc(all, func(a, b string) int { return len(b) - len(a) })
	pairs := make([]string, 0, 2*len(all))
	for _, s := range all {
		pairs = append(pairs, s, Redacted)
	}
	return &redactor{secrets: all, replacer: strings.NewReplacer(pairs...)}
}

func (r *redactor) contains(s string) bool {
	for _, secret := range r.secrets {
		if strings.Contains(s, secret) {
			return true
		}
	}
	return false
}

// replaceAttr is called by slog for the message, the built-in attributes and
// every attribute inside groups (slog recurses into groups itself), with
// LogValuer values already resolved.
func (r *redactor) replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if r.replacer == nil {
		return a
	}
	a.Key = r.replacer.Replace(a.Key)
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(r.replacer.Replace(a.Value.String()))
	case slog.KindAny:
		a.Value = r.anyValue(a.Value)
	}
	return a
}

func (r *redactor) anyValue(v slog.Value) slog.Value {
	switch x := v.Any().(type) {
	case error:
		// The JSON handler prints err.Error() as well.
		return slog.StringValue(r.replacer.Replace(x.Error()))
	case fmt.Stringer:
		if s := x.String(); r.contains(s) {
			return slog.StringValue(r.replacer.Replace(s))
		}
	}
	// Anything else is rendered by the handler with encoding/json. Check
	// that rendering so a secret nested in a struct or a map cannot slip
	// through.
	rendered := fmt.Sprint(v.Any())
	if b, err := json.Marshal(v.Any()); err == nil {
		rendered = string(b)
	}
	if r.contains(rendered) {
		return slog.StringValue(r.replacer.Replace(rendered))
	}
	return v
}
