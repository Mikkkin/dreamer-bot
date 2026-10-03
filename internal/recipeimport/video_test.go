package recipeimport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMP4 starts like a real MP4 (an ftyp box), so content sniffing sees
// a video.
var fakeMP4 = append([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), make([]byte, 64)...)

// embedPage wraps a video URL the way the embed page does: JSON inside a
// JavaScript string, so `/` is `\\\/` and `%` is `\\u0025`, plus whatever
// raw escaping the caller passes in rawValue.
func embedPage(rawValue string) string {
	return `<html><head><title>Instagram</title></head><body><script>requireLazy(["TimeSliceImpl"],function(){});</script>` +
		`<script type="application/json">{"require":[["PolarisEmbedSimple","init",[],[{"contextJSON":"{\"context\":{\"is_video\":true,` +
		`\"owner\":{\"username\":\"v_ogorod\"},\"video_url\":\"` + rawValue + `\"}}"}]]]}</script></body></html>`
}

// realEmbedPage lays a post out like the real embed page: the caption as
// HTML first, then the post's data as a JSON string under contextJSON in
// a script, with the caption (edge_media_to_caption) before the video
// URL. Like Instagram, both JSON levels escape «/». videoURL "" leaves the
// key out (a photo post).
func realEmbedPage(t *testing.T, caption, videoURL string, isVideo bool) string {
	t.Helper()
	media := map[string]any{
		"__typename":            "GraphVideo",
		"shortcode":             "DItfAhKCJ3h",
		"is_video":              isVideo,
		"edge_media_to_caption": map[string]any{"edges": []any{map[string]any{"node": map[string]any{"text": caption}}}},
	}
	if videoURL != "" {
		media["video_url"] = videoURL
	}
	inner, err := json.Marshal(map[string]any{
		"context":  map[string]any{"type": "GraphVideo", "shortcode": "DItfAhKCJ3h"},
		"gql_data": map[string]any{"shortcode_media": media},
	})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := json.Marshal(strings.ReplaceAll(string(inner), "/", `\/`))
	if err != nil {
		t.Fatal(err)
	}
	contextJSON := strings.ReplaceAll(string(outer), "/", `\/`)
	if i, j := strings.Index(contextJSON, "edge_media_to_caption"), strings.Index(contextJSON, "video_url"); videoURL != "" && (i < 0 || j < i) {
		t.Fatalf("the caption must come before the video URL, as on the real page")
	}
	return `<html><body><div class="Caption"><a class="CaptionUsername" href="https://www.instagram.com/cook/">cook</a><br />` + caption + `</div>` +
		`<script>requireLazy(["TimeSliceImpl"],function(){});</script>` +
		`<script>(function(){s.handle({"require":[["PolarisEmbedSimple","init",[],[{"isProfileEmbed":false,"contextJSON":` + contextJSON + `}]]]});})();</script></body></html>`
}

// escapeAsInEmbed applies the two JSON levels of the real embed page to a
// plain URL: `/` → `\\\/`, `&` → `\\u0026`.
func escapeAsInEmbed(plain string) string {
	return strings.NewReplacer("/", `\\\/`, "&", `\\u0026`).Replace(plain)
}

type videoServer struct {
	mu      sync.Mutex
	foreign []string // requests to hosts the fetcher must never contact
	paths   []string
}

func (vs *videoServer) record(r *http.Request) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.paths = append(vs.paths, r.Host+r.URL.Path)
	u := &url.URL{Scheme: "https", Host: r.Host}
	if r.Host != canonicalHost && !allowedVideoURL(u) {
		vs.foreign = append(vs.foreign, r.Host+r.URL.Path)
	}
}

func TestFetcherVideo(t *testing.T) {
	const cdn = "https://scontent-ams2-1.cdninstagram.com/o1/v/t2/f2/m367/AQP.mp4?_nc_cat=103&efg=eyJ9%3D&_nc_ht=scontent-ams2-1.cdninstagram.com&oe=6AC6AF08"
	pages := map[string]string{
		// The real page: `\"video_url\":\"https:\\\/\\\/…\\u00253D…\"`.
		"/reel/REALPAGE1/embed/captioned/": embedPage(strings.ReplaceAll(escapeAsInEmbed(cdn), "%", `\\u0025`)),
		// The real layout, built from JSON.
		"/reel/REALPAGE2/embed/captioned/": realEmbedPage(t, "Борщ", cdn, true),
		// contextJSON as an object rather than a string holding one.
		"/p/OBJECTCTX/embed/captioned/": `<script>{"contextJSON":{"is_video":true,"video_url":"` + strings.NewReplacer("/", `\/`, "&", `\u0026`).Replace(cdn) + `"}}</script>`,
	}
	vs := &videoServer{}
	var gotQuery, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vs.record(r)
		switch {
		case r.Host == canonicalHost:
			page, ok := pages[r.URL.Path]
			if !ok || r.UserAgent() != userAgent {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(page))
		case r.Host == "scontent-ams2-1.cdninstagram.com" && r.URL.Path == "/o1/v/t2/f2/m367/AQP.mp4":
			gotQuery, gotUA = r.URL.RawQuery, r.UserAgent()
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(fakeMP4)
		default:
			http.Error(w, "unexpected", http.StatusTeapot)
		}
	}))
	defer srv.Close()
	f := testFetcher(srv, nil)
	for _, ref := range []Ref{{"reel", "REALPAGE1"}, {"reel", "REALPAGE2"}, {"p", "OBJECTCTX"}} {
		gotQuery = ""
		video, mimeType, err := f.Video(context.Background(), ref)
		if err != nil {
			t.Errorf("%s: %v", ref.Shortcode, err)
			continue
		}
		if string(video) != string(fakeMP4) || mimeType != "video/mp4" {
			t.Errorf("%s: %d bytes of %q", ref.Shortcode, len(video), mimeType)
		}
		if want, _ := url.Parse(cdn); gotQuery != want.RawQuery || gotUA != userAgent {
			t.Errorf("%s: CDN query %q (want %q), UA %q", ref.Shortcode, gotQuery, want.RawQuery, gotUA)
		}
	}
	srv.Close()
	if len(vs.foreign) > 0 {
		t.Errorf("requests left the allowed hosts: %v", vs.foreign)
	}
}

func TestFetcherVideoNoVideo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p/PHOTO0001/embed/captioned/":
			_, _ = w.Write([]byte(`<script>{"contextJSON":"{\"context\":{\"is_video\":false,\"display_url\":\"https:\\\/\\\/scontent.cdninstagram.com\\\/a.jpg\"}}"}</script>`))
		case "/p/NULLVIDEO/embed/captioned/":
			_, _ = w.Write([]byte(`<script>{"contextJSON":"{\"video_url\":null,\"is_video\":false}"}</script>`))
		case "/reel/HIDDEN001/embed/captioned/":
			_, _ = w.Write([]byte(`<script>{"contextJSON":"{\"context\":{\"is_video\":true}}"}</script>`))
		case "/reel/GONE00001/embed/captioned/":
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := testFetcher(srv, nil)
	for _, code := range []string{"PHOTO0001", "NULLVIDEO"} {
		if _, _, err := f.Video(context.Background(), Ref{"p", code}); !errors.Is(err, ErrNoVideo) {
			t.Errorf("%s: err = %v, want ErrNoVideo", code, err)
		}
	}
	for _, code := range []string{"HIDDEN001", "GONE00001"} {
		_, _, err := f.Video(context.Background(), Ref{"reel", code})
		if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNoVideo) {
			t.Errorf("%s: err = %v, want ErrUnavailable", code, err)
		}
	}
}

func TestFetcherVideoRefusesForeignHosts(t *testing.T) {
	vs := &videoServer{}
	var videoURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vs.record(r)
		switch {
		case r.Host == canonicalHost:
			_, _ = w.Write([]byte(embedPage(videoURL)))
		case r.Host == "scontent.cdninstagram.com" && r.URL.Path == "/away.mp4":
			http.Redirect(w, r, "https://evil.example/v.mp4", http.StatusFound)
		case r.Host == "scontent.cdninstagram.com" && r.URL.Path == "/downgrade.mp4":
			http.Redirect(w, r, "http://scontent.cdninstagram.com/v.mp4", http.StatusFound)
		case r.Host == "scontent.cdninstagram.com" && r.URL.Path == "/port.mp4":
			http.Redirect(w, r, "https://scontent.cdninstagram.com:8443/v.mp4", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(fakeMP4)
		}
	}))
	defer srv.Close()
	f := testFetcher(srv, nil)
	for _, raw := range []string{
		`https:\\\/\\\/evil.example\\\/v.mp4`,
		`http:\\\/\\\/scontent.cdninstagram.com\\\/v.mp4`,
		`https:\\\/\\\/cdninstagram.com.evil.example\\\/v.mp4`,
		`https:\\\/\\\/scontent.cdninstagram.com.evil.example\\\/v.mp4`,
		`https:\\\/\\\/evilcdninstagram.com\\\/v.mp4`,
		`https:\\\/\\\/scontent.cdninstagram.com@evil.example\\\/v.mp4`,
		`https:\\\/\\\/user:pass@scontent.cdninstagram.com\\\/v.mp4`,
		`https:\\\/\\\/scontent.cdninstagram.com:443\\\/v.mp4`,
		`https:\\\/\\\/scontent.cdninstagram.com:8443\\\/v.mp4`,
		`https:\\\/\\\/169.254.169.254\\\/latest\\\/meta-data`,
		`https:\\\/\\\/evil.example\\u002f.cdninstagram.com\\\/v.mp4`,
		`https:\\\/\\\/evil.example%2f.cdninstagram.com\\\/v.mp4`,
		`https:\\\/\\\/evil.example\\\\.cdninstagram.com\\\/v.mp4`,
		`file:\\\/\\\/\\\/etc\\\/passwd`,
		`\\u0000`,
		`https:\\\/\\\/scontent.cdninstagram.com\\\/away.mp4`,
		`https:\\\/\\\/scontent.cdninstagram.com\\\/downgrade.mp4`,
		`https:\\\/\\\/scontent.cdninstagram.com\\\/port.mp4`,
	} {
		videoURL = raw
		_, _, err := f.Video(context.Background(), Ref{"reel", "FOREIGN01"})
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("video_url %s: err = %v, want ErrUnavailable", raw, err)
		}
	}
	srv.Close()
	if len(vs.foreign) > 0 {
		t.Errorf("requests left the allowed hosts: %v", vs.foreign)
	}
}

func TestAllowedVideoURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://scontent-ams2-1.cdninstagram.com/o1/v/t2/x.mp4": true,
		"https://video.xx.fbcdn.net/v/x.mp4":                     true,
		"https://scontent.cdninstagram.com:443/x.mp4":            false,
		"https://scontent.cdninstagram.com:8443/x.mp4":           false,
		"http://scontent.cdninstagram.com/x.mp4":                 false,
		"https://u@scontent.cdninstagram.com/x.mp4":              false,
		"https://cdninstagram.com/x.mp4":                         false,
		"https://fbcdn.net.evil.example/x.mp4":                   false,
	} {
		u, err := url.Parse(raw)
		if got := err == nil && allowedVideoURL(u); got != want {
			t.Errorf("allowedVideoURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestFetcherVideoDownloadLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == canonicalHost {
			code := strings.Split(strings.Trim(r.URL.Path, "/"), "/")[1]
			_, _ = w.Write([]byte(embedPage(`https:\\\/\\\/scontent.cdninstagram.com\\\/` + code + `.mp4`)))
			return
		}
		switch r.URL.Path {
		case "/DECLARED1.mp4": // a Content-Length over the cap: refused unread
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", "5000")
			_, _ = w.Write(make([]byte, 5000))
		case "/STREAMED1.mp4": // no Content-Length, too long
			w.Header().Set("Content-Type", "video/mp4")
			for range 5 {
				_, _ = w.Write(make([]byte, 1000))
				w.(http.Flusher).Flush()
			}
		case "/HTMLPAGE1.mp4":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>login</html>"))
		case "/OCTETMP41.mp4":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(fakeMP4)
		case "/QUICKTIME.mp4":
			w.Header().Set("Content-Type", "video/quicktime")
			_, _ = w.Write(fakeMP4)
		case "/OCTETTEXT.mp4":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("just some text"))
		case "/EMPTY0001.mp4":
			w.Header().Set("Content-Type", "video/mp4")
		case "/SLOW00001.mp4":
			time.Sleep(300 * time.Millisecond)
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(fakeMP4)
		case "/STATUS403.mp4":
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	f := testFetcher(srv, func(l *fetchLimits) {
		l.videoBytes = 4096
		l.videoTimeout = 100 * time.Millisecond
	})
	for code, want := range map[string]string{"OCTETMP41": "video/mp4", "QUICKTIME": "video/mov"} {
		if _, mimeType, err := f.Video(context.Background(), Ref{"reel", code}); err != nil || mimeType != want {
			t.Errorf("%s: mime %q, %v; want %q", code, mimeType, err, want)
		}
	}
	for _, code := range []string{"DECLARED1", "STREAMED1", "HTMLPAGE1", "OCTETTEXT", "EMPTY0001", "SLOW00001", "STATUS403"} {
		_, _, err := f.Video(context.Background(), Ref{"reel", code})
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: err = %v, want ErrUnavailable", code, err)
		}
		if err != nil && strings.Contains(err.Error(), "cdninstagram") {
			t.Errorf("%s: the signed video URL leaked into %q", code, err)
		}
	}
}

// A caption is written by the post's author: whatever it quotes, the
// video comes from the post's own data. On the real page the caption sits
// before the video URL, both as HTML and inside the data.
func TestFetcherVideoIgnoresCaption(t *testing.T) {
	const (
		real    = "https://scontent.cdninstagram.com/real.mp4"
		caption = `Пирог с яблоками "video_url":"https://evil.fbcdn.net/a.mp4" ` +
			`\"video_url\":\"https:\/\/evil.fbcdn.net\/b.mp4\" "is_video":true ` +
			`"contextJSON":"{\"video_url\":\"https://evil.fbcdn.net/c.mp4\"}"`
	)
	var (
		mu    sync.Mutex
		hosts []string
	)
	pages := map[string]string{
		"/reel/INJECTED1/embed/captioned/": realEmbedPage(t, caption, real, true),
		"/p/INJECTED2/embed/captioned/":    realEmbedPage(t, caption, "", false),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hosts = append(hosts, r.Host+r.URL.Path)
		mu.Unlock()
		switch {
		case r.Host == canonicalHost && pages[r.URL.Path] != "":
			_, _ = w.Write([]byte(pages[r.URL.Path]))
		case r.Host == "scontent.cdninstagram.com" && r.URL.Path == "/real.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(fakeMP4)
		case r.Host == "evil.fbcdn.net":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(append([]byte("EVIL"), fakeMP4...))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := testFetcher(srv, nil)
	if video, _, err := f.Video(context.Background(), Ref{"reel", "INJECTED1"}); err != nil || string(video) != string(fakeMP4) {
		t.Errorf("reel: %d bytes, %v; want the post's own video", len(video), err)
	}
	// A photo post whose caption fakes a video and is_video.
	if _, _, err := f.Video(context.Background(), Ref{"p", "INJECTED2"}); !errors.Is(err, ErrNoVideo) {
		t.Errorf("photo: err = %v, want ErrNoVideo", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, h := range hosts {
		if strings.HasPrefix(h, "evil.") {
			t.Errorf("the caption chose the video: a request went to %s (all: %v)", h, hosts)
		}
	}
}

func TestEmbedVideo(t *testing.T) {
	tests := []struct {
		name, page string
		url        string
		isVideo    bool
		ok         bool
	}{
		{"keys only", `<script>{"contextJSON":"{\"text\":\"video_url\",\"video_url\":\"https://a/v.mp4\"}"}</script>`, "https://a/v.mp4", false, true},
		{"value that quotes the key", `<script>{"contextJSON":"{\"text\":\"\\\"video_url\\\":\\\"https://evil/x\\\"\",\"video_url\":\"https://a/v.mp4\"}"}</script>`, "https://a/v.mp4", false, true},
		{"first in document order", `<script>{"contextJSON":"{\"items\":[{\"video_url\":\"https://a/1.mp4\"},{\"video_url\":\"https://a/2.mp4\"}]}"}</script>`, "https://a/1.mp4", false, true},
		{"nested is_video", `<script>{"contextJSON":"{\"a\":{\"b\":[{\"is_video\":true}]}}"}</script>`, "", true, true},
		{"is_video as a string", `<script>{"contextJSON":"{\"is_video\":\"true\",\"text\":\"is_video\\\":true\"}"}</script>`, "", false, true},
		{"null video", `<script>{"contextJSON":"{\"video_url\":null,\"is_video\":false}"}</script>`, "", false, true},
		{"a list is not a URL", `<script>{"contextJSON":"{\"video_url\":[\"https://a/v.mp4\"]}"}</script>`, "", false, true},
		{"outside any script", `<div>"contextJSON":"{\"video_url\":\"https://evil/x\"}"</div>`, "", false, true},
		{"bare key, no post data", `<script>{"video_url":"https://evil/x","is_video":true}</script>`, "", false, true},
		{"unclosed script", `<script>{"contextJSON":"{\"video_url\":\"https://a/v.mp4\"}"}`, "", false, true},
		{"second script", `<script>var a = 1;</script><script type="application/json">{"contextJSON":"{\"video_url\":\"https://a/v.mp4\"}"}</script>`, "https://a/v.mp4", false, true},
		{"unreadable data", `<script>{"contextJSON":"{\"video_url\":</script>`, "", false, false},
		{"truncated document", `<script>{"contextJSON":"{\"video_url\":\"https://a/v.mp4\""}</script>`, "", false, false},
	}
	for _, tt := range tests {
		got, isVideo, ok := embedVideo([]byte(tt.page))
		if got != tt.url || isVideo != tt.isVideo || ok != tt.ok {
			t.Errorf("%s: embedVideo = %q, %v, %v; want %q, %v, %v", tt.name, got, isVideo, ok, tt.url, tt.isVideo, tt.ok)
		}
	}
}

func TestPostReel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := postPage(`5 likes - x on May 1, 2025: "Суп".`, "")
		if r.URL.Path == "/p/REELASP01/" {
			page = strings.Replace(page, "</head>", `<meta property="og:url" content="https://www.instagram.com/v_ogorod/reel/REELASP01/" /></head>`, 1)
		}
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()
	f := testFetcher(srv, nil)
	for ref, want := range map[Ref]bool{{"reel", "REEL00001"}: true, {"tv", "TV0000001"}: true, {"p", "PHOTO0001"}: false, {"p", "REELASP01"}: true} {
		if post, err := f.Post(context.Background(), ref); err != nil || post.Reel != want {
			t.Errorf("%v: reel = %v, %v; want %v", ref, post.Reel, err, want)
		}
	}
}

func TestVideoQuota(t *testing.T) {
	now := time.Date(2026, 10, 3, 23, 59, 0, 0, time.Local)
	q := NewVideoQuota(2)
	q.now = func() time.Time { return now }
	takes := func(n int) []bool {
		got := make([]bool, n)
		for i := range got {
			got[i] = q.take()
		}
		return got
	}
	if got := takes(3); !slices.Equal(got, []bool{true, true, false}) {
		t.Fatalf("a quota of 2 must give exactly 2, got %v", got)
	}
	q.refund()
	if got := takes(2); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("a refund must give back exactly one, got %v", got)
	}
	now = now.Add(2 * time.Minute) // the next day
	if got := takes(3); !slices.Equal(got, []bool{true, true, false}) {
		t.Fatalf("the quota must reset on a new day, got %v", got)
	}
	if NewVideoQuota(0).limit != DefaultVideoDailyLimit {
		t.Error("perDay 0 must mean the default")
	}
}

// postPageWithMedia lays out the post page as served to searchAgent: the
// media data in a JSON script, with a caption that quotes fake media data
// and an object of another post before the post's own one.
func postPageWithMedia(t *testing.T, code, videoURL string) string {
	t.Helper()
	fake := `{"code":"` + code + `","video_versions":[{"url":"https://evil.fbcdn.net/fake.mp4"}]}`
	doc := map[string]any{"require": []any{[]any{"ScheduledServerJS", "handle", nil, []any{map[string]any{
		"__bbox": map[string]any{"result": map[string]any{"data": map[string]any{
			"xdt_api__v1__media__shortcode__web_info": map[string]any{"items": []any{
				map[string]any{"code": "OTHERPOST", "video_versions": []any{map[string]any{"type": 101, "url": "https://scontent.cdninstagram.com/other.mp4"}}},
				map[string]any{
					"code":           code,
					"caption":        map[string]any{"text": "Рецепт в видео " + fake},
					"video_versions": []any{map[string]any{"type": 101, "url": videoURL}},
				},
			}},
		}}},
	}}}}}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return `<html><body><div>Рецепт в видео ` + fake + `</div><script type="application/json" data-sjs>` + string(b) + `</script></body></html>`
}

func TestFetcherVideoFromThePostPage(t *testing.T) {
	const cdn = "https://scontent-ams2-1.cdninstagram.com/o1/v/t2/f2/m86/AQPC.mp4?_nc_cat=106&efg=eyJ9"
	vs := &videoServer{}
	var pageUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vs.record(r)
		switch {
		case r.Host == canonicalHost && strings.HasSuffix(r.URL.Path, "/embed/captioned/"):
			// The embed page says «a video» but leaves the URL out.
			_, _ = w.Write([]byte(`<script>{"contextJSON":"{\"context\":{\"is_video\":true}}"}</script>`))
		case r.Host == canonicalHost && r.URL.Path == "/reel/DcjdkNGKlwS/":
			pageUA = r.UserAgent()
			_, _ = w.Write([]byte(postPageWithMedia(t, "DcjdkNGKlwS", cdn)))
		case r.Host == canonicalHost && r.URL.Path == "/reel/NOMEDIA01/":
			_, _ = w.Write([]byte(`<script type="application/json">{"require":[]}</script>`))
		case r.Host == "scontent-ams2-1.cdninstagram.com" && r.URL.Path == "/o1/v/t2/f2/m86/AQPC.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(fakeMP4)
		default:
			http.Error(w, "unexpected", http.StatusTeapot)
		}
	}))
	defer srv.Close()
	f := testFetcher(srv, nil)

	video, mimeType, err := f.Video(context.Background(), Ref{"reel", "DcjdkNGKlwS"})
	if err != nil || string(video) != string(fakeMP4) || mimeType != "video/mp4" {
		t.Fatalf("Video = %d bytes %q, %v", len(video), mimeType, err)
	}
	if pageUA != searchAgent {
		t.Errorf("post page fetched as %q, want %q", pageUA, searchAgent)
	}
	// No media data for the post: unavailable, not «no video».
	if _, _, err := f.Video(context.Background(), Ref{"reel", "NOMEDIA01"}); !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNoVideo) {
		t.Errorf("post page without media = %v, want ErrUnavailable", err)
	}
	srv.Close()
	if len(vs.foreign) > 0 {
		t.Errorf("requests left the allowed hosts (the caption's fake URL was used?): %v", vs.foreign)
	}
}

func TestMediaVideoURL(t *testing.T) {
	var doc any
	_ = json.Unmarshal([]byte(`{"a":[{"code":"X1","video_versions":[{"url":"u-x1"}]},{"b":{"code":"Y2","video_versions":[]}},{"code":"Y2","caption":{"text":"{\"code\":\"Y2\",\"video_versions\":[{\"url\":\"evil\"}]}"},"video_versions":[{"url":"u-y2"},{"url":"u-y2-low"}]}]}`), &doc)
	if u, ok := mediaVideoURL(doc, "Y2", 0); !ok || u != "u-y2" {
		t.Errorf("Y2 = %q, %v; want the post's own first version", u, ok)
	}
	if _, ok := mediaVideoURL(doc, "Z3", 0); ok {
		t.Error("an unknown post must have no video URL")
	}
}
