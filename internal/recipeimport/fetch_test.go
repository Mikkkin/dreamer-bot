package recipeimport

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// routeTo sends every request to srv while the client still sees the real
// URLs, so redirect and host checks run exactly as in production. The test
// server reads the requested host from r.Host.
type routeTo struct{ srv *httptest.Server }

func (rt routeTo) RoundTrip(r *http.Request) (*http.Response, error) {
	target, _ := url.Parse(rt.srv.URL)
	r2 := r.Clone(r.Context())
	r2.URL.Scheme, r2.URL.Host = target.Scheme, target.Host
	r2.Host = r.URL.Host
	return http.DefaultTransport.RoundTrip(r2)
}

func testFetcher(srv *httptest.Server, mutate func(*fetchLimits)) *Fetcher {
	limits := defaultLimits
	limits.timeout = 2 * time.Second
	if mutate != nil {
		mutate(&limits)
	}
	return newFetcher(routeTo{srv}, limits)
}

func postPage(description, image string) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	b.WriteString(`<meta property="og:title" content="Огород on Instagram: &quot;Маринад&quot;">`)
	if description != "" {
		b.WriteString(`<meta property="og:description" content="` + html.EscapeString(description) + `" />`)
	}
	if image != "" {
		b.WriteString(`<meta content='` + html.EscapeString(image) + `' property='og:image'>`)
	}
	b.WriteString(`</head><body>` + strings.Repeat("x", 1000) + `</body></html>`)
	return b.String()
}

const sampleCaption = "Маринад для шашлыка\n\nОсновные ингредиенты:\n- Соль — 23 г\n- Перец «чёрный» — 2 ч. л.\n\nПриятного аппетита ✌🏻 & \"кавычки\": тоже"

func TestFetcherPost(t *testing.T) {
	var gotUA, gotLang, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != canonicalHost {
			http.Error(w, "wrong host", http.StatusTeapot)
			return
		}
		gotUA, gotLang, gotPath = r.UserAgent(), r.Header.Get("Accept-Language"), r.URL.Path
		desc := `323 likes, 4 comments - v_ogorod on April 21, 2025: "` + sampleCaption + `".`
		_, _ = w.Write([]byte(postPage(desc, "https://scontent-ams2-1.cdninstagram.com/v/t51.2885-15/1.jpg?stp=dst-jpg&_nc_ht=x")))
	}))
	defer srv.Close()

	post, err := testFetcher(srv, nil).Post(context.Background(), Ref{"reel", "DItfAhKCJ3h"})
	if err != nil {
		t.Fatal(err)
	}
	if post.Caption != sampleCaption {
		t.Errorf("caption = %q, want %q", post.Caption, sampleCaption)
	}
	if post.Author != "v_ogorod" {
		t.Errorf("author = %q", post.Author)
	}
	if post.ImageURL != "https://scontent-ams2-1.cdninstagram.com/v/t51.2885-15/1.jpg?stp=dst-jpg&_nc_ht=x" {
		t.Errorf("image = %q", post.ImageURL)
	}
	if gotUA != userAgent || !strings.HasPrefix(gotLang, "en-US") || gotPath != "/reel/DItfAhKCJ3h/" {
		t.Errorf("request: UA %q, Accept-Language %q, path %q", gotUA, gotLang, gotPath)
	}
}

func TestStripWrapper(t *testing.T) {
	tests := []struct{ desc, caption, author string }{
		{`323 likes, 4 comments - v_ogorod on April 21, 2025: "Суп"`, "Суп", "v_ogorod"},
		{`323 likes, 4 comments - v_ogorod on April 21, 2025: "Суп".`, "Суп", "v_ogorod"},
		{`1,234 likes, 56 comments - chef.anna on March 3, 2024: "Суп "с" фрикадельками": да."`, `Суп "с" фрикадельками": да.`, "chef.anna"},
		{`12K likes, 1.2K comments - food_ru on December 31, 2023: "Line 1` + "\n\n" + `Line 2".`, "Line 1\n\nLine 2", "food_ru"},
		{`3.4M likes, 10K comments - big on May 1, 2022: "Х"`, "Х", "big"},
		{`1 like, 0 comments - solo on June 9, 2021: "Один"`, "Один", "solo"},
		{`548 comments - nolikes on July 7, 2025: "Без лайков".`, "Без лайков", "nolikes"},
		{`hidden_counts on July 7, 2025: "Скрытые счётчики".`, "Скрытые счётчики", "hidden_counts"},
		{`323 likes, 4 comments - v_ogorod on April 21, 2025`, "", "v_ogorod"},
		{`Просто описание без обёртки`, "Просто описание без обёртки", ""},
	}
	for _, tt := range tests {
		caption, author := stripWrapper(tt.desc)
		if caption != tt.caption || author != tt.author {
			t.Errorf("stripWrapper(%q) = %q, %q; want %q, %q", tt.desc, caption, author, tt.caption, tt.author)
		}
	}
}

func TestOgMetaDecodesEntities(t *testing.T) {
	page := `<head><meta property="og:description" content="Соль &amp; перец &#x2014; по вкусу&#10;&quot;Да&quot; &#064;chef &lt;3"><meta name="og:image" content="https://a.cdninstagram.com/1.jpg"></head>`
	m := ogMeta([]byte(page))
	if m["og:description"] != "Соль & перец — по вкусу\n\"Да\" @chef <3" {
		t.Errorf("description = %q", m["og:description"])
	}
	if m["og:image"] != "https://a.cdninstagram.com/1.jpg" {
		t.Errorf("image = %q", m["og:image"])
	}
}

func TestFetcherPostFailures(t *testing.T) {
	var (
		hits    int
		foreign []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Host != canonicalHost {
			foreign = append(foreign, r.Host+r.URL.Path)
		}
		switch r.URL.Path {
		case "/p/NOCAPTION/":
			_, _ = w.Write([]byte(postPage("", "")))
		case "/p/BIGPAGE01/":
			_, _ = w.Write([]byte(postPage(`1 like, 0 comments - x on May 1, 2025: "`+strings.Repeat("я", 4000)+`".`, "")))
		case "/p/GONE00001/":
			http.NotFound(w, r)
		case "/p/LIMITED01/":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/p/OFFHOST01/":
			http.Redirect(w, r, "https://evil.example/p/OFFHOST01/", http.StatusFound)
		case "/p/OFFHOST02/":
			http.Redirect(w, r, "https://instagram.com.evil.example/p/OFFHOST02/", http.StatusMovedPermanently)
		case "/p/PLAINHTTP/":
			http.Redirect(w, r, "http://www.instagram.com/p/PLAINHTTP/", http.StatusFound)
		case "/p/LOOP00001/":
			http.Redirect(w, r, "/p/LOOP00001/", http.StatusFound)
		case "/p/SLOW00001/":
			time.Sleep(300 * time.Millisecond)
			_, _ = w.Write([]byte(postPage(`1 like - x on May 1, 2025: "Суп".`, "")))
		}
	}))
	defer srv.Close()
	f := testFetcher(srv, func(l *fetchLimits) {
		l.postBytes = 4000
		l.timeout = 100 * time.Millisecond
	})
	for _, code := range []string{"NOCAPTION", "BIGPAGE01", "GONE00001", "LIMITED01", "OFFHOST01", "OFFHOST02", "PLAINHTTP", "LOOP00001", "SLOW00001"} {
		_, err := f.Post(context.Background(), Ref{"p", code})
		if !errors.Is(err, ErrUnavailable) || !errors.Is(err, domain.ErrExternalUnavailable) {
			t.Errorf("%s: err = %v, want ErrUnavailable", code, err)
		}
		if code == "NOCAPTION" && !errors.Is(err, ErrNoCaption) {
			t.Errorf("%s: err = %v, want ErrNoCaption", code, err)
		}
	}
	srv.Close() // waits for the handlers, so the counters below are final
	if hits > 20 {
		t.Errorf("%d requests: redirects are not capped", hits)
	}
	if len(foreign) > 0 {
		t.Errorf("requests left the canonical host: %v", foreign)
	}
}

func TestFetcherPostFollowsCanonicalRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host != canonicalHost:
			http.Error(w, "wrong host", http.StatusTeapot)
		case r.URL.Path == "/reel/MOVED0001/":
			http.Redirect(w, r, "https://www.instagram.com/p/MOVED0001/", http.StatusMovedPermanently)
		case r.URL.Path == "/p/MOVED0001/":
			_, _ = w.Write([]byte(postPage(`5 likes, 1 comment - x on May 1, 2025: "Суп".`, "")))
		}
	}))
	defer srv.Close()
	post, err := testFetcher(srv, nil).Post(context.Background(), Ref{"reel", "MOVED0001"})
	if err != nil || post.Caption != "Суп" {
		t.Fatalf("post = %+v, %v", post, err)
	}
}

func TestFetcherPostDropsForeignImage(t *testing.T) {
	for _, image := range []string{
		"https://evil.example/a.jpg",
		"http://scontent.cdninstagram.com/a.jpg",
		"https://cdninstagram.com.evil.example/a.jpg",
		"javascript:alert(1)",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(postPage(`5 likes - x on May 1, 2025: "Суп".`, image)))
		}))
		post, err := testFetcher(srv, nil).Post(context.Background(), Ref{"p", "IMAGE0001"})
		srv.Close()
		if err != nil || post.ImageURL != "" {
			t.Errorf("og:image %q: post = %+v, %v; want no image", image, post, err)
		}
	}
}

func TestAllowedImageURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://scontent-ams2-1.cdninstagram.com/v/t51.2885-15/1.jpg": true,
		"https://scontent.cdninstagram.com/x.jpg":                      true,
		"https://scontent.xx.fbcdn.net/v/x.jpg":                        true,
		"https://SCONTENT.CDNINSTAGRAM.COM/x.jpg":                      true,
		"https://scontent.cdninstagram.com:443/x.jpg":                  true,
		"http://scontent.cdninstagram.com/x.jpg":                       false,
		"https://cdninstagram.com/x.jpg":                               false,
		"https://cdninstagram.com.evil.example/x.jpg":                  false,
		"https://scontent.cdninstagram.com.evil.example/x.jpg":         false,
		"https://evilcdninstagram.com/x.jpg":                           false,
		"https://fbcdn.net.evil.example/x.jpg":                         false,
		"https://user@scontent.cdninstagram.com/x.jpg":                 false,
		"https://scontent.cdninstagram.com:8443/x.jpg":                 false,
		"https://127.0.0.1/x.jpg":                                      false,
		"https://169.254.169.254/latest/meta-data":                     false,
		"file:///etc/passwd":                                           false,
	} {
		u, err := url.Parse(raw)
		if got := err == nil && allowedImageURL(u); got != want {
			t.Errorf("allowedImageURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestFetcherImage(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n fake image bytes")
	var foreign []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedImageURL(&url.URL{Scheme: "https", Host: r.Host}) {
			foreign = append(foreign, r.Host)
		}
		switch r.Host + r.URL.Path {
		case "scontent.cdninstagram.com/ok.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(png)
		case "scontent.cdninstagram.com/moved.jpg":
			http.Redirect(w, r, "https://scontent.xx.fbcdn.net/ok.jpg", http.StatusFound)
		case "scontent.xx.fbcdn.net/ok.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(png)
		case "scontent.cdninstagram.com/away.jpg":
			http.Redirect(w, r, "https://evil.example/ok.jpg", http.StatusFound)
		case "scontent.cdninstagram.com/page.jpg":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>login</html>"))
		case "scontent.cdninstagram.com/huge.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(make([]byte, 2048))
		default:
			http.Error(w, "unexpected "+r.Host+r.URL.Path, http.StatusTeapot)
		}
	}))
	defer srv.Close()
	f := testFetcher(srv, func(l *fetchLimits) { l.imageBytes = 1024 })
	ctx := context.Background()
	for _, ok := range []string{"https://scontent.cdninstagram.com/ok.jpg", "https://scontent.cdninstagram.com/moved.jpg"} {
		if got, err := f.Image(ctx, ok); err != nil || string(got) != string(png) {
			t.Errorf("Image(%q) = %d bytes, %v", ok, len(got), err)
		}
	}
	for _, bad := range []string{
		"https://scontent.cdninstagram.com/away.jpg",
		"https://scontent.cdninstagram.com/page.jpg",
		"https://scontent.cdninstagram.com/huge.jpg",
		"https://cdninstagram.com.evil.example/ok.jpg",
		"http://scontent.cdninstagram.com/ok.jpg",
		"::not a url",
	} {
		if _, err := f.Image(ctx, bad); !errors.Is(err, ErrUnavailable) {
			t.Errorf("Image(%q): err = %v, want ErrUnavailable", bad, err)
		}
	}
	srv.Close()
	if len(foreign) > 0 {
		t.Errorf("requests left the CDN hosts: %v", foreign)
	}
}
