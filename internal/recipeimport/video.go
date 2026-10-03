package recipeimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrNoVideo means the post holds no video (a photo or a carousel of
// photos). It is not a failure: the importer just reads the caption.
var ErrNoVideo = errors.New("recipeimport: the post has no video")

// MaxVideoBytes caps a downloaded video (a 2-minute reel is about 26 MB).
const MaxVideoBytes = 60 << 20

// maxVideoURLBytes bounds the escaped video URL (a real one is about
// 1.1 KB).
const maxVideoURLBytes = 8 << 10

// videoTypes are the video media types the video parser accepts, with
// the name it knows each by.
var videoTypes = map[string]string{
	"video/mp4":       "video/mp4",
	"video/mpeg":      "video/mpeg",
	"video/mpg":       "video/mpg",
	"video/mov":       "video/mov",
	"video/quicktime": "video/mov",
	"video/avi":       "video/avi",
	"video/x-flv":     "video/x-flv",
	"video/webm":      "video/webm",
	"video/wmv":       "video/wmv",
	"video/3gpp":      "video/3gpp",
}

// reelPathRe matches the og:url of a reel or IGTV video.
var reelPathRe = regexp.MustCompile(`^https://www\.instagram\.com/(?:[^/?#]+/)?(?:reels?|tv)/`)

// embedURL is the post's embed page, rebuilt from the parsed parts like
// Ref.URL. Unlike the post page, it carries the video URL.
func (r Ref) embedURL() string {
	return r.URL() + "embed/captioned/"
}

// allowedVideoURL reports an https URL on Instagram's CDN without
// credentials or any explicit port.
func allowedVideoURL(u *url.URL) bool {
	return u.Port() == "" && allowedImageURL(u)
}

// searchAgent is the search crawler Instagram serves a post's media data
// to. The post page carries the video URL in it when the embed page leaves
// the video out (some reels, e.g. with licensed music).
const searchAgent = "Googlebot/2.1 (+http://www.google.com/bot.html)"

// Video downloads the post's video and returns it with its media type.
// It reads the video URL from the post's embed page or, when that page
// says the post is a video but gives no URL, from the post page's media
// data, and downloads it from Instagram's CDN only, within 60 s and
// 60 MiB. ErrNoVideo means the post has no video; any other failure is
// ErrUnavailable.
func (f *Fetcher) Video(ctx context.Context, ref Ref) ([]byte, string, error) {
	page, err := f.get(ctx, f.posts, ref.embedURL(), f.limits.postBytes, "text/html,application/xhtml+xml")
	if err != nil {
		return nil, "", err
	}
	raw, isVideo, ok := embedVideo(page)
	if raw == "" && (isVideo || !ok) {
		if raw, err = f.pageVideo(ctx, ref); err != nil {
			return nil, "", err
		}
	}
	switch {
	case raw == "":
		return nil, "", ErrNoVideo
	case len(raw) > maxVideoURLBytes:
		return nil, "", fmt.Errorf("%w: unreadable video URL", ErrUnavailable)
	}
	u, err := url.Parse(raw)
	if err != nil || !allowedVideoURL(u) {
		return nil, "", fmt.Errorf("%w: video host not allowed", ErrUnavailable)
	}
	return f.download(ctx, u.String())
}

// pageVideo reads the video URL from the media data on the post page: the
// first of the "video_versions" of the media object whose "code" is the
// post's shortcode, in a JSON script of the page. Only that object counts,
// and only through JSON keys: the caption is a string value in it and is
// never parsed, so it cannot pose as the media data.
func (f *Fetcher) pageVideo(ctx context.Context, ref Ref) (string, error) {
	page, err := f.getAs(ctx, f.posts, ref.URL(), f.limits.postBytes, "text/html,application/xhtml+xml", searchAgent)
	if err != nil {
		return "", err
	}
	for _, script := range scripts(page) {
		if !bytes.Contains(script, []byte(`"video_versions"`)) {
			continue
		}
		var doc any
		if json.Unmarshal(script, &doc) != nil {
			continue
		}
		if u, found := mediaVideoURL(doc, ref.Shortcode, 0); found {
			return u, nil
		}
	}
	return "", fmt.Errorf("%w: the post gives no video URL", ErrUnavailable)
}

// mediaVideoURL finds the media object of the post (its "code" is the
// shortcode) and returns the URL of its first video version. depth bounds
// the walk.
func mediaVideoURL(v any, code string, depth int) (string, bool) {
	if depth > 64 {
		return "", false
	}
	switch t := v.(type) {
	case map[string]any:
		if c, _ := t["code"].(string); c == code {
			if versions, _ := t["video_versions"].([]any); len(versions) > 0 {
				if first, _ := versions[0].(map[string]any); first != nil {
					if u, _ := first["url"].(string); u != "" {
						return u, true
					}
				}
			}
		}
		for _, child := range t {
			if u, found := mediaVideoURL(child, code, depth+1); found {
				return u, true
			}
		}
	case []any:
		for _, child := range t {
			if u, found := mediaVideoURL(child, code, depth+1); found {
				return u, true
			}
		}
	}
	return "", false
}

// contextKey opens the post's data on the embed page: a JSON document
// (usually a JSON string holding it) under "contextJSON" in a script.
var contextKey = []byte(`"contextJSON":`)

// embedVideo reads the video URL and the is_video flag from the post's
// data on the embed page. Only keys of that document count. The caption
// is a string value inside it (and plain text in the page's HTML), so a
// caption that quotes «"video_url":"…"», escaped or not, never poses as
// the key: inside a script every quote of the caption is escaped, and the
// HTML outside scripts is never read. ok is false when the data is there
// but unreadable; a page without it has no video ("", false, true).
func embedVideo(page []byte) (videoURL string, isVideo, ok bool) {
	for _, script := range scripts(page) {
		i := bytes.Index(script, contextKey)
		if i < 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(script[i+len(contextKey):]))
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return "", false, false
		}
		doc := []byte(value)
		var text string
		if json.Unmarshal(value, &text) == nil {
			doc = []byte(text)
		}
		return videoKeys(doc)
	}
	return "", false, true
}

// scripts returns the contents of the page's <script> elements; an
// unclosed one is ignored.
func scripts(page []byte) [][]byte {
	var out [][]byte
	for {
		i := bytes.Index(page, []byte("<script"))
		if i < 0 {
			return out
		}
		page = page[i:]
		j := bytes.IndexByte(page, '>')
		if j < 0 {
			return out
		}
		page = page[j+1:]
		k := bytes.Index(page, []byte("</script"))
		if k < 0 {
			return out
		}
		out = append(out, page[:k])
		page = page[k:]
	}
}

// videoKeys walks a JSON document in order and returns the first string
// value of a "video_url" key and whether any "is_video" key is true.
// Keys are told apart from string values, so no value can pose as a key.
func videoKeys(doc []byte) (videoURL string, isVideo, ok bool) {
	type frame struct {
		object bool
		key    string // the key whose value comes next
		hasKey bool
	}
	var stack []frame
	dec := json.NewDecoder(bytes.NewReader(doc))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) && len(stack) == 0 {
			return videoURL, isVideo, true
		}
		if err != nil {
			return "", false, false // malformed or truncated
		}
		top := len(stack) - 1
		if top >= 0 && stack[top].object && !stack[top].hasKey {
			if key, isKey := tok.(string); isKey {
				stack[top].key, stack[top].hasKey = key, true
			} else {
				stack = stack[:top] // the object's closing brace
			}
			continue
		}
		key := ""
		if top >= 0 && stack[top].object {
			key, stack[top].hasKey = stack[top].key, false
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				stack = append(stack, frame{object: true})
			case '[':
				stack = append(stack, frame{})
			default:
				stack = stack[:top] // the array's closing bracket
			}
		case string:
			if key == "video_url" && videoURL == "" {
				videoURL = v
			}
		case bool:
			if key == "is_video" && v {
				isVideo = true
			}
		}
	}
}

func (f *Fetcher) download(ctx context.Context, target string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "video/*")
	resp, err := f.videos.Do(req)
	if err != nil {
		// The URL carries signed tokens; keep it out of the error.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, "", fmt.Errorf("%w: downloading the video: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: video status %d", ErrUnavailable, resp.StatusCode)
	}
	limit := f.limits.videoBytes
	if resp.ContentLength > limit {
		return nil, "", fmt.Errorf("%w: video over %d bytes", ErrUnavailable, limit)
	}
	// Size the buffer once from Content-Length: growing it by doubling
	// would leave tens of MB of garbage per video on a small server.
	var buf bytes.Buffer
	if resp.ContentLength > 0 {
		buf.Grow(int(resp.ContentLength) + bytes.MinRead)
	}
	if _, err := buf.ReadFrom(io.LimitReader(resp.Body, limit+1)); err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, "", fmt.Errorf("%w: reading the video: %v", ErrUnavailable, err)
	}
	body := buf.Bytes()
	if int64(len(body)) > limit {
		return nil, "", fmt.Errorf("%w: video over %d bytes", ErrUnavailable, limit)
	}
	if len(body) == 0 {
		return nil, "", fmt.Errorf("%w: empty video", ErrUnavailable)
	}
	mimeType, ok := videoType(resp.Header.Get("Content-Type"), body)
	if !ok {
		return nil, "", fmt.Errorf("%w: not a video", ErrUnavailable)
	}
	return body, mimeType, nil
}

// videoType names the video's media type from the response header, or
// from its first bytes when the header is generic or missing.
func videoType(header string, body []byte) (string, bool) {
	mt, _, err := mime.ParseMediaType(header)
	if err == nil {
		if t, ok := videoTypes[strings.ToLower(mt)]; ok {
			return t, true
		}
	}
	if header != "" && err == nil && mt != "application/octet-stream" && mt != "binary/octet-stream" {
		return "", false
	}
	sniffed, _, _ := mime.ParseMediaType(http.DetectContentType(body))
	t, ok := videoTypes[sniffed]
	return t, ok
}

// ------------------------------------------------------------- quota --

// DefaultVideoDailyLimit bounds the videos sent to the model a day.
const DefaultVideoDailyLimit = 20

// VideoQuota counts the videos sent to the model per calendar day (the
// server's local date) and refuses more than its limit. It lives in
// memory: a restart forgets the count. It is safe for concurrent use.
type VideoQuota struct {
	limit int
	now   func() time.Time

	mu   sync.Mutex
	day  string
	used int
}

// NewVideoQuota returns a quota of perDay videos a day; perDay < 1 means
// DefaultVideoDailyLimit.
func NewVideoQuota(perDay int) *VideoQuota {
	if perDay < 1 {
		perDay = DefaultVideoDailyLimit
	}
	return &VideoQuota{limit: perDay, now: time.Now}
}

// take reserves one video for today; false when today's limit is used up.
func (q *VideoQuota) take() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if day := q.now().Format(time.DateOnly); day != q.day {
		q.day, q.used = day, 0
	}
	if q.used >= q.limit {
		return false
	}
	q.used++
	return true
}

// refund gives back a reservation that sent nothing to the model (the
// post had no video).
func (q *VideoQuota) refund() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.used > 0 && q.now().Format(time.DateOnly) == q.day {
		q.used--
	}
}

var sharedVideoQuota = sync.OnceValue(func() *VideoQuota { return NewVideoQuota(DefaultVideoDailyLimit) })

// videoSlots bounds the videos held in memory at once (each up to
// MaxVideoBytes) on a small server.
var videoSlots = make(chan struct{}, 2)
