package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/time/rate"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const (
	testToken               = "123456789:AAEXAMPLEexampleEXAMPLEexample_-12345"
	alice     domain.UserID = 111
	bob       domain.UserID = 222
	mallory   domain.UserID = 999 // valid Telegram user, not whitelisted
	maxImg                  = 1 << 10
)

var (
	testNow   = time.Date(2026, 9, 27, 10, 5, 0, 0, time.UTC)
	testNames = map[domain.UserID]string{alice: "Алиса", bob: "Боб", mallory: "Мэллори"}
)

// clock is a settable time source shared by the signer and the server.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

type harness struct {
	t       *testing.T
	handler http.Handler
	db      *fakeDB
	logs    *bytes.Buffer
	clock   *clock
}

type option func(*Options, *tuning)

func newHarness(t *testing.T, opts ...option) *harness {
	t.Helper()
	clk := &clock{t: testNow}
	db := newFakeDB(testNow, maxImg, alice, bob)
	logs := &bytes.Buffer{}
	o := Options{
		Services:  db.services(),
		Validator: auth.NewInitDataValidator(testToken, 24*time.Hour, clk.now),
		Whitelist: auth.NewWhitelist([]domain.UserID{alice, bob}),
		Signer:    auth.NewMediaSigner(testToken, clk.now),
		Static: fstest.MapFS{
			"index.html":           {Data: []byte("<!doctype html><title>app</title>")},
			"assets/app-abc123.js": {Data: []byte("console.log(1)")},
			"favicon.svg":          {Data: []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")},
			".gitkeep":             {},
		},
		DefaultCurrency: "EUR",
		MaxImageBytes:   maxImg,
		Log:             slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	tu := defaultTuning()
	tu.apiRate = rateLimit{every: rate.Inf}
	tu.uploadRate = rateLimit{every: rate.Inf}
	tu.now = clk.now
	for _, opt := range opts {
		opt(&o, &tu)
	}
	return &harness{t: t, handler: newHandler(o, tu), db: db, logs: logs, clock: clk}
}

// signInitData produces launch data exactly like Telegram: every field but
// hash, sorted, joined by newlines, HMAC'ed with the WebAppData secret.
func signInitData(token string, user domain.UserID, authDate time.Time) string {
	fields := map[string]string{
		"auth_date": strconv.FormatInt(authDate.Unix(), 10),
		"query_id":  "AAHdF6IQAAAAAN0XohDhrOrc",
		"signature": "c2lnbmF0dXJlLXBsYWNlaG9sZGVy",
		"user": fmt.Sprintf(`{"id":%d,"first_name":%q,"username":"u%d","language_code":"ru"}`,
			user, testNames[user], user),
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, len(keys))
	q := url.Values{}
	for i, k := range keys {
		pairs[i] = k + "=" + fields[k]
		q.Set(k, fields[k])
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(pairs, "\n")))
	q.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return q.Encode()
}

func tma(user domain.UserID) string {
	return "tma " + signInitData(testToken, user, testNow.Add(-time.Minute))
}

func (h *harness) do(method, target string, body io.Reader, header http.Header) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, target, body)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// call sends an API request as user (0 = anonymous). A string body is sent
// verbatim, any other non-nil body is JSON-encoded.
func (h *harness) call(method, target string, user domain.UserID, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	header := http.Header{}
	if user != 0 {
		header.Set("Authorization", tma(user))
	}
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
		header.Set("Content-Type", "application/json")
	default:
		data, err := json.Marshal(b)
		if err != nil {
			h.t.Fatal(err)
		}
		r = bytes.NewReader(data)
		header.Set("Content-Type", "application/json")
	}
	return h.do(method, target, r, header)
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return v
}

type errorResponse struct {
	Error struct {
		Code    string  `json:"code"`
		Message string  `json:"message"`
		Field   *string `json:"field"`
	} `json:"error"`
}

// expectError checks status, code and (when field != "-") the field.
func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code, field string) errorResponse {
	t.Helper()
	expectStatus(t, rec, status)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("error Content-Type = %q", ct)
	}
	e := decode[errorResponse](t, rec)
	if e.Error.Code != code {
		t.Fatalf("code = %q, want %q; body: %s", e.Error.Code, code, rec.Body.String())
	}
	if e.Error.Message == "" {
		t.Fatalf("error without a message: %s", rec.Body.String())
	}
	switch {
	case field == "-":
	case field == "" && e.Error.Field != nil:
		t.Fatalf("unexpected field %q", *e.Error.Field)
	case field != "" && (e.Error.Field == nil || *e.Error.Field != field):
		t.Fatalf("field = %v, want %q; body: %s", e.Error.Field, field, rec.Body.String())
	}
	return e
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
