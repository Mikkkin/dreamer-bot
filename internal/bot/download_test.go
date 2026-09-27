package bot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-telegram/bot/models"
)

// assertNoToken fails if s contains the token or its secret half in any
// encoding a URL error might use.
func assertNoToken(t *testing.T, s string) {
	t.Helper()
	_, secret, _ := strings.Cut(testToken, ":")
	for _, leak := range []string{testToken, secret, url.PathEscape(testToken), url.QueryEscape(testToken)} {
		if strings.Contains(s, leak) {
			t.Fatalf("token leaked: %q", s)
		}
	}
}

func newTestDownloader(api *fakeAPI, max int64) *downloader {
	return newDownloader(api, max, newRedactor(testToken))
}

func TestDownloadErrorsAreRedacted(t *testing.T) {
	// A closed port makes net/http fail with a *url.Error that embeds the
	// full request URL, token included.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	api := newFakeAPI()
	api.link = "http://" + addr
	d := newTestDownloader(api, 1<<20)

	raw, rawErr := d.fetchRaw(context.Background(), "file-1")
	if rawErr == nil || raw != nil {
		t.Fatal("expected a connection error")
	}
	if !strings.Contains(rawErr.Error(), testToken) {
		t.Skip("net/http no longer puts the URL into errors; nothing to redact")
	}

	_, err = d.fetch(context.Background(), "file-1")
	if err == nil {
		t.Fatal("expected an error")
	}
	assertNoToken(t, err.Error())
	assertNoToken(t, fmt.Sprintf("%+v", err))
	if errors.Unwrap(err) != nil {
		t.Error("redacted error must not expose the original through Unwrap")
	}
	if !strings.Contains(err.Error(), redacted) {
		t.Errorf("error %q does not mark the redaction", err)
	}
}

func TestDownloadGetFileErrorIsRedacted(t *testing.T) {
	api := newFakeAPI()
	api.fail["GetFile"] = errors.New("error do request for method getFile, Post https://api.telegram.org/bot" + testToken + "/getFile: timeout")
	_, err := newTestDownloader(api, 1<<20).fetch(context.Background(), "f")
	if err == nil {
		t.Fatal("expected an error")
	}
	assertNoToken(t, err.Error())
}

func TestDownloadLimits(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/big.jpg"):
			_, _ = w.Write(make([]byte, 101))
		case strings.HasSuffix(r.URL.Path, "/ok.jpg"):
			_, _ = w.Write(make([]byte, 100))
		case strings.HasSuffix(r.URL.Path, "/redirect.jpg"):
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		case strings.HasSuffix(r.URL.Path, "/elsewhere"):
			t.Error("download followed a redirect")
		default:
			http.Error(w, "nope", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	fetch := func(path string, size int64) ([]byte, error) {
		api := newFakeAPI()
		api.link = srv.URL
		api.file = &models.File{FilePath: path, FileSize: size}
		return newTestDownloader(api, 100).fetch(context.Background(), "f")
	}

	if data, err := fetch("ok.jpg", 100); err != nil || len(data) != 100 {
		t.Errorf("ok: %d bytes, %v", len(data), err)
	}
	before := hits.Load()
	if _, err := fetch("ok.jpg", 101); err == nil {
		t.Error("declared size over the limit was downloaded")
	}
	if hits.Load() != before {
		t.Error("oversized file was requested despite its declared size")
	}
	if _, err := fetch("big.jpg", 0); err == nil {
		t.Error("body over the limit accepted (size not declared)")
	}
	if _, err := fetch("redirect.jpg", 0); err == nil {
		t.Error("redirect response accepted")
	}
	_, err := fetch("error.jpg", 0)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("status error %v", err)
	}
	if err != nil {
		assertNoToken(t, err.Error())
	}
}

func TestDownloaderCapsAtBotAPILimit(t *testing.T) {
	if d := newTestDownloader(newFakeAPI(), 50<<20); d.max != maxBotDownload {
		t.Errorf("max = %d, want the 20 MB Bot API limit", d.max)
	}
	if d := newTestDownloader(newFakeAPI(), 5<<20); d.max != 5<<20 {
		t.Errorf("max = %d, want MaxImageBytes", d.max)
	}
}

func TestRedactor(t *testing.T) {
	r := newRedactor(testToken)
	_, secret, _ := strings.Cut(testToken, ":")
	in := "a " + testToken + " b " + url.PathEscape(testToken) + " c " + secret
	out := r.string(in)
	assertNoToken(t, out)
	if r.err(nil) != nil {
		t.Error("nil error must stay nil")
	}
}
