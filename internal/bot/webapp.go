package bot

import (
	"errors"
	"net/url"
	"strconv"
	"sync/atomic"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// webApp holds the public Mini App URL. It can change at runtime (a quick
// tunnel gets a new hostname on every start), so readers always load it.
type webApp struct {
	base atomic.Pointer[string]
}

// validateWebAppURL accepts "" (unknown) or an absolute https URL, which is
// what Telegram requires for web_app buttons.
func validateWebAppURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("bot: web app URL must be an absolute https URL")
	}
	return nil
}

func (w *webApp) set(raw string) { w.base.Store(&raw) }

// url returns the Mini App URL or "" when it is not known yet.
func (w *webApp) url() string {
	if p := w.base.Load(); p != nil {
		return *p
	}
	return ""
}

// wishLink deep-links to one wish (WEBAPP_URL?wish=<id>), "" if unknown.
func (w *webApp) wishLink(id domain.WishID) string {
	return w.deepLink("wish", int64(id))
}

// recipeLink deep-links to one recipe (WEBAPP_URL?recipe=<id>).
func (w *webApp) recipeLink(id domain.RecipeID) string {
	return w.deepLink("recipe", int64(id))
}

func (w *webApp) deepLink(key string, id int64) string {
	base := w.url()
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u.RawQuery = url.Values{key: {strconv.FormatInt(id, 10)}}.Encode()
	u.Fragment = ""
	return u.String()
}
