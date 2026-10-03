package recipeimport

import (
	"net/url"
	"regexp"
	"strings"
)

// Ref identifies an Instagram post: Kind is "p" (a photo or carousel post),
// "reel" or "tv", Shortcode is the post's ID from its URL.
type Ref struct {
	Kind      string
	Shortcode string
}

// canonicalHost is the only host the importer ever contacts for a post.
const canonicalHost = "www.instagram.com"

// instagramHosts are the hosts a shared link may use.
var instagramHosts = map[string]bool{
	"instagram.com":     true,
	"www.instagram.com": true,
	"m.instagram.com":   true,
}

var shortcodeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{5,40}$`)

// postKinds maps a URL path segment to the Kind of the post; "reels" is the
// plural form the app shares for reels.
var postKinds = map[string]string{"p": "p", "reel": "reel", "reels": "reel", "tv": "tv"}

// reservedPaths are Instagram's own first path segments, never a username:
// «/share/reel/…» carries a share token in place of the shortcode.
var reservedPaths = map[string]bool{
	"share": true, "explore": true, "stories": true, "accounts": true, "direct": true,
	"about": true, "developer": true, "legal": true, "web": true, "api": true,
	"graphql": true, "challenge": true, "emails": true, "session": true, "privacy": true,
	"terms": true, "static": true, "invites": true, "lite": true, "ar": true,
}

// ParseURL finds an Instagram post link in raw: a share link copied from the
// app (with ?igsh=… and other query parameters or a fragment), a /reels/
// link, an m. or bare instagram.com host, a /username/p/… link, or a link
// without a scheme. raw may hold other text around the link (a message with
// a comment); the first post link wins. Links to any other host, to
// profiles, to stories or to Instagram's own pages (/share/…, /explore/…)
// are rejected.
func ParseURL(raw string) (Ref, bool) {
	for _, token := range strings.Fields(raw) {
		token = strings.Trim(token, `.,;:!?()[]{}<>«»"'`+"`")
		if ref, ok := parseToken(token); ok {
			return ref, true
		}
	}
	return Ref{}, false
}

func parseToken(token string) (Ref, bool) {
	if token == "" || len(token) > 2048 {
		return Ref{}, false
	}
	if !strings.Contains(token, "://") {
		token = "https://" + token
	}
	u, err := url.Parse(token)
	if err != nil || u.User != nil || u.Port() != "" {
		return Ref{}, false
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "https" && scheme != "http" {
		return Ref{}, false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if !instagramHosts[host] {
		return Ref{}, false
	}
	segments := strings.FieldsFunc(u.EscapedPath(), func(r rune) bool { return r == '/' })
	// /p/CODE/… or /username/p/CODE/…
	for i := 0; i+1 < len(segments) && i <= 1; i++ {
		kind, ok := postKinds[strings.ToLower(segments[i])]
		if !ok {
			continue
		}
		if i == 1 && reservedPaths[strings.ToLower(segments[0])] {
			// «/share/reel/TOKEN/» holds a share token, not a shortcode.
			return Ref{}, false
		}
		if code := segments[i+1]; shortcodeRe.MatchString(code) {
			return Ref{Kind: kind, Shortcode: code}, true
		}
		return Ref{}, false
	}
	return Ref{}, false
}

// URL is the canonical post link, rebuilt from the parsed parts. It is the
// only URL the Fetcher ever requests and the link stored with the recipe.
func (r Ref) URL() string {
	return "https://" + canonicalHost + "/" + r.Kind + "/" + r.Shortcode + "/"
}
