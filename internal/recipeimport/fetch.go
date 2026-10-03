package recipeimport

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Post is what a link preview of an Instagram post shows: the caption, the
// cover picture and the author's username.
type Post struct {
	Caption  string
	ImageURL string // "" when the page has no usable og:image
	Author   string // "" when unknown
	// Reel reports a reel or IGTV video, known from the link or the
	// page's og:url. Other posts may hold a video too (Fetcher.Video
	// tells).
	Reel bool
}

// userAgent is the link-preview robot Instagram serves full captions to; a
// browser user agent gets a JavaScript shell without them.
const userAgent = "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"

// imageHostSuffixes are the CDN hosts a cover picture may come from.
var imageHostSuffixes = []string{".cdninstagram.com", ".fbcdn.net"}

type fetchLimits struct {
	timeout      time.Duration
	postBytes    int64
	imageBytes   int64
	redirects    int
	videoTimeout time.Duration
	videoBytes   int64
}

var defaultLimits = fetchLimits{
	timeout:      10 * time.Second,
	postBytes:    3 << 20,  // a post page is about 0.8 MB
	imageBytes:   10 << 20, // the media pipeline re-encodes and limits it further
	redirects:    2,
	videoTimeout: 60 * time.Second, // a 2-minute reel (26 MB) took 3 s
	videoBytes:   MaxVideoBytes,
}

// Fetcher reads public Instagram posts the way a link preview does. It
// only ever requests the canonical post URL (Ref.URL), its embed page
// (for the video) and pictures and videos on Instagram's CDN, follows at
// most two redirects and only within those hosts, and caps every
// response. It is safe for concurrent use.
type Fetcher struct {
	posts  *http.Client
	images *http.Client
	videos *http.Client
	limits fetchLimits
}

// NewFetcher returns a Fetcher with a 10 s timeout per request (60 s for
// a video).
func NewFetcher() *Fetcher {
	return newFetcher(http.DefaultTransport.(*http.Transport).Clone(), defaultLimits)
}

func newFetcher(rt http.RoundTripper, limits fetchLimits) *Fetcher {
	errRedirect := errors.New("redirect off the allowed hosts")
	check := func(allowed func(*url.URL) bool) func(*http.Request, []*http.Request) error {
		return func(req *http.Request, via []*http.Request) error {
			if len(via) > limits.redirects || !allowed(req.URL) {
				return errRedirect
			}
			return nil
		}
	}
	return &Fetcher{
		posts: &http.Client{
			Transport:     rt,
			Timeout:       limits.timeout,
			CheckRedirect: check(isCanonicalPostURL),
		},
		images: &http.Client{
			Transport:     rt,
			Timeout:       limits.timeout,
			CheckRedirect: check(func(u *url.URL) bool { return allowedImageURL(u) }),
		},
		videos: &http.Client{
			Transport:     rt,
			Timeout:       limits.videoTimeout,
			CheckRedirect: check(allowedVideoURL),
		},
		limits: limits,
	}
}

func isCanonicalPostURL(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && u.Port() == "" &&
		strings.TrimSuffix(strings.ToLower(u.Hostname()), ".") == canonicalHost
}

// allowedImageURL reports an https URL on Instagram's CDN, without
// credentials or an unusual port.
func allowedImageURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	for _, suffix := range imageHostSuffixes {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}
	return false
}

// Post fetches the post page and reads its caption from og:description,
// where Instagram wraps it as `N likes, M comments - user on Date:
// "caption".`. ErrNoCaption means the page came without og:description
// (a login wall, a removed post); any other failure is ErrUnavailable.
func (f *Fetcher) Post(ctx context.Context, ref Ref) (Post, error) {
	body, err := f.get(ctx, f.posts, ref.URL(), f.limits.postBytes, "text/html,application/xhtml+xml")
	if err != nil {
		return Post{}, err
	}
	meta := ogMeta(body)
	desc, ok := meta["og:description"]
	if !ok {
		return Post{}, ErrNoCaption
	}
	caption, author := stripWrapper(desc)
	if author == "" {
		author = authorFromTitle(meta["og:title"])
	}
	post := Post{Caption: caption, Author: author, Reel: ref.Kind != "p" || reelPathRe.MatchString(meta["og:url"])}
	if img, err := url.Parse(meta["og:image"]); err == nil && allowedImageURL(img) {
		post.ImageURL = img.String()
	}
	return post, nil
}

// Image downloads a cover picture from Instagram's CDN. Any other host,
// plain http, a redirect elsewhere, a non-image or a body over 10 MiB is
// ErrUnavailable.
func (f *Fetcher) Image(ctx context.Context, imageURL string) ([]byte, error) {
	u, err := url.Parse(imageURL)
	if err != nil || !allowedImageURL(u) {
		return nil, fmt.Errorf("%w: image host not allowed", ErrUnavailable)
	}
	return f.get(ctx, f.images, u.String(), f.limits.imageBytes, "image/*")
}

func (f *Fetcher) get(ctx context.Context, client *http.Client, target string, limit int64, accept string) ([]byte, error) {
	return f.getAs(ctx, client, target, limit, accept, userAgent)
}

// getAs is get with another User-Agent.
func (f *Fetcher) getAs(ctx context.Context, client *http.Client, target string, limit int64, accept, agent string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("User-Agent", agent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	if accept == "image/*" {
		if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "image/") {
			return nil, fmt.Errorf("%w: not an image (%s)", ErrUnavailable, ct)
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: response over %d bytes", ErrUnavailable, limit)
	}
	return body, nil
}

var (
	metaTagRe  = regexp.MustCompile(`(?is)<meta\b(?:[^>"']|"[^"]*"|'[^']*')*>`)
	metaAttrRe = regexp.MustCompile(`(?is)([a-z_:.-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
)

// ogMeta collects the <meta property="og:…" content="…"> values of a page,
// HTML entities decoded. The first value of each property wins.
func ogMeta(page []byte) map[string]string {
	out := make(map[string]string)
	for _, tag := range metaTagRe.FindAll(page, -1) {
		var prop, content string
		hasContent := false
		for _, m := range metaAttrRe.FindAllSubmatch(tag, -1) {
			value := string(m[2]) + string(m[3]) + string(m[4])
			switch strings.ToLower(string(m[1])) {
			case "property", "name":
				prop = strings.ToLower(value)
			case "content":
				content, hasContent = value, true
			}
		}
		if !strings.HasPrefix(prop, "og:") || !hasContent {
			continue
		}
		if _, seen := out[prop]; !seen {
			out[prop] = html.UnescapeString(content)
		}
	}
	return out
}

var (
	// wrapperRe matches `323 likes, 4 comments - demo_kitchen on April 21,
	// 2025: "caption".` with counts like 1,234 or 12K and either count
	// missing.
	wrapperRe = regexp.MustCompile(`(?is)^\s*(?:[\d.,]+\s*[kmb]?\s+(?:likes?|comments?)\s*[,-]?\s*)*-?\s*([^\s"]+)\s+on\s+([^:"\n]{4,40}?)\s*:\s*["“](.*)["”]\s*\.?\s*$`)
	// countsOnlyRe matches the wrapper of a post without a caption.
	countsOnlyRe  = regexp.MustCompile(`(?is)^\s*(?:[\d.,]+\s*[kmb]?\s+(?:likes?|comments?)\s*[,-]?\s*)+-?\s*([^\s"]+)\s+on\s+[^:"\n]{4,40}?\s*\.?\s*$`)
	titleAuthorRe = regexp.MustCompile(`^(.{1,80}?) on Instagram`)
)

// stripWrapper takes the caption out of og:description and returns it with
// the author's username. A description in another shape is returned as
// the caption.
func stripWrapper(desc string) (caption, author string) {
	if m := wrapperRe.FindStringSubmatch(desc); m != nil {
		return strings.TrimSpace(m[3]), m[1]
	}
	if m := countsOnlyRe.FindStringSubmatch(desc); m != nil {
		return "", m[1]
	}
	return strings.TrimSpace(desc), ""
}

func authorFromTitle(title string) string {
	if m := titleAuthorRe.FindStringSubmatch(title); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}
