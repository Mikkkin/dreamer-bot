package httpapi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func TestAuthentication(t *testing.T) {
	valid := signInitData(testToken, alice, testNow.Add(-time.Minute))
	cases := []struct {
		name   string
		header string
		status int
		code   string
	}{
		{"missing header", "", http.StatusUnauthorized, "unauthorized"},
		{"other scheme", "Bearer " + valid, http.StatusUnauthorized, "unauthorized"},
		{"scheme only", "tma", http.StatusUnauthorized, "unauthorized"},
		{"empty data", "tma ", http.StatusUnauthorized, "unauthorized"},
		{"garbage", "tma not-init-data", http.StatusUnauthorized, "unauthorized"},
		{"tampered hash", "tma " + valid[:len(valid)-2] + "00", http.StatusUnauthorized, "unauthorized"},
		{"duplicate user appended", "tma " + valid + "&user=%7B%22id%22%3A111%7D", http.StatusUnauthorized, "unauthorized"},
		{"signed with another token", "tma " + signInitData("987654321:AAEXAMPLEexampleEXAMPLEexample_-99999", alice, testNow), http.StatusUnauthorized, "unauthorized"},
		{"expired", "tma " + signInitData(testToken, alice, testNow.Add(-25*time.Hour)), http.StatusUnauthorized, "unauthorized"},
		{"from the future", "tma " + signInitData(testToken, alice, testNow.Add(5*time.Minute)), http.StatusUnauthorized, "unauthorized"},
		{"valid but not whitelisted", tma(mallory), http.StatusForbidden, "forbidden"},
		{"whitelisted", "tma " + valid, http.StatusOK, ""},
		{"scheme is case-insensitive", "TMA " + valid, http.StatusOK, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			header := http.Header{}
			if c.header != "" {
				header.Set("Authorization", c.header)
			}
			rec := h.do(http.MethodGet, "/api/me", nil, header)
			if c.code == "" {
				expectStatus(t, rec, c.status)
				return
			}
			expectError(t, rec, c.status, c.code, "")
			if c.status == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "tma" {
				t.Error("401 must carry WWW-Authenticate: tma")
			}
			if len(h.db.touches) != 0 {
				t.Errorf("rejected requests must not touch users, got %+v", h.db.touches)
			}
		})
	}
}

func TestTouchRecordsProfileFromInitData(t *testing.T) {
	h := newHarness(t)
	expectStatus(t, h.call(http.MethodGet, "/api/me", alice, nil), http.StatusOK)
	want := domain.User{ID: alice, FirstName: "Алиса", Username: "u111"}
	if len(h.db.touches) != 1 || h.db.touches[0] != want {
		t.Fatalf("touches = %+v, want [%+v]", h.db.touches, want)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name  string
		path  string
		user  domain.UserID
		cache string
	}{
		{"index", "/", 0, "no-cache"},
		{"index by name", "/index.html", 0, "no-cache"},
		{"asset", "/assets/app-abc123.js", 0, "public, max-age=31536000, immutable"},
		{"missing asset", "/assets/gone-123.js", 0, "no-store"},
		{"root file", "/favicon.svg", 0, "no-cache"},
		{"unknown path", "/nope", 0, "no-store"},
		{"api", "/api/me", alice, "no-store"},
		{"api unauthorized", "/api/me", 0, "no-store"},
		{"api unknown", "/api/nope", alice, "no-store"},
		{"media bad signature", "/media/1/thumb?exp=1&sig=00", 0, "no-store"},
		{"health", "/healthz", 0, "no-store"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := h.call(http.MethodGet, c.path, c.user, nil)
			hdr := rec.Header()
			if got := hdr.Get("Content-Security-Policy"); got != contentSecurityPolicy {
				t.Errorf("CSP = %q", got)
			}
			if !strings.Contains(hdr.Get("Content-Security-Policy"), "frame-ancestors https://web.telegram.org") {
				t.Error("CSP must allow framing by Telegram Web only")
			}
			if hdr.Get("X-Content-Type-Options") != "nosniff" {
				t.Error("missing nosniff")
			}
			if hdr.Get("Referrer-Policy") != "no-referrer" {
				t.Error("missing Referrer-Policy")
			}
			if hdr.Get("Permissions-Policy") == "" {
				t.Error("missing Permissions-Policy")
			}
			if _, ok := hdr["X-Frame-Options"]; ok {
				t.Error("X-Frame-Options would break Telegram Web")
			}
			if got := hdr.Get("Cache-Control"); got != c.cache {
				t.Errorf("Cache-Control = %q, want %q", got, c.cache)
			}
		})
	}
}

func TestRateLimit(t *testing.T) {
	h := newHarness(t, func(_ *Options, tu *tuning) {
		tu.apiRate = rateLimit{every: 0, burst: 3}
	})
	for range 3 {
		expectStatus(t, h.call(http.MethodGet, "/api/categories", alice, nil), http.StatusOK)
	}
	rec := h.call(http.MethodGet, "/api/categories", alice, nil)
	expectError(t, rec, http.StatusTooManyRequests, "rate_limited", "")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 must carry Retry-After")
	}
	// Buckets are per user.
	expectStatus(t, h.call(http.MethodGet, "/api/categories", bob, nil), http.StatusOK)
}

func TestUploadRateLimitIsStricter(t *testing.T) {
	h := newHarness(t, func(_ *Options, tu *tuning) {
		tu.uploadRate = rateLimit{every: 0, burst: 1}
	})
	id := createWish(t, h, alice, map[string]any{"title": "Камера"})
	expectStatus(t, h.upload("/api/wishes/"+id+"/images", alice, jpeg(10)), http.StatusCreated)
	expectError(t, h.upload("/api/wishes/"+id+"/images", alice, jpeg(10)), http.StatusTooManyRequests, "rate_limited", "")
	// Other endpoints are unaffected.
	expectStatus(t, h.call(http.MethodGet, "/api/wishes/"+id, alice, nil), http.StatusOK)
}

func TestLimiterEvictsIdleBuckets(t *testing.T) {
	clk := &clock{t: testNow}
	l := newLimiter(rateLimit{every: 0, burst: 1}, 10*time.Minute, clk.now)
	if !l.allow(alice) || l.allow(alice) {
		t.Fatal("burst of one must allow exactly one request")
	}
	clk.t = clk.t.Add(11 * time.Minute)
	if !l.allow(bob) {
		t.Fatal("bob must get his own bucket")
	}
	l.mu.Lock()
	_, aliceKept := l.buckets[alice]
	size := len(l.buckets)
	l.mu.Unlock()
	if aliceKept || size != 1 {
		t.Fatalf("idle bucket not evicted: size %d", size)
	}
}

func TestPanicBecomesGeneric500(t *testing.T) {
	h := newHarness(t, func(o *Options, _ *tuning) { o.Services.Stats = panickingStats{} })
	rec := h.call(http.MethodGet, "/api/stats", alice, nil)
	expectError(t, rec, http.StatusInternalServerError, "internal", "")
	body := rec.Body.String()
	for _, leak := range []string{"exploded", "4711", "goroutine", ".go:"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response leaks %q: %s", leak, body)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("security headers must survive a panic")
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "internal-detail-4711") || !strings.Contains(logs, "goroutine") {
		t.Fatalf("panic and stack must be logged: %s", logs)
	}
	if !strings.Contains(logs, `"route":"GET /api/stats","status":500`) {
		t.Fatalf("access log must record the 500: %s", logs)
	}
}

func TestInternalErrorsAreGeneric(t *testing.T) {
	h := newHarness(t, func(o *Options, _ *tuning) { o.Services.Stats = failingStats{} })
	rec := h.call(http.MethodGet, "/api/stats", alice, nil)
	expectError(t, rec, http.StatusInternalServerError, "internal", "")
	if strings.Contains(rec.Body.String(), "sqlite") || strings.Contains(rec.Body.String(), "/data") {
		t.Fatalf("internal detail leaked: %s", rec.Body.String())
	}
	if !strings.Contains(h.logs.String(), "disk I/O error") {
		t.Fatal("the cause must be logged")
	}
}

func TestAccessLogOmitsSecrets(t *testing.T) {
	h := newHarness(t)
	header := tma(alice)
	expectStatus(t, h.call(http.MethodGet, "/api/wishes?q=secret-query", alice, nil), http.StatusOK)
	expectStatus(t, h.call(http.MethodGet, "/api/me", mallory, nil), http.StatusForbidden)
	h.do(http.MethodGet, "/media/5/thumb?exp=1790000000&sig=deadbeefcafe", nil, nil)

	logs := h.logs.String()
	signed, err := url.ParseQuery(strings.TrimPrefix(header, "tma "))
	if err != nil {
		t.Fatal(err)
	}
	hash := signed.Get("hash")
	for _, leak := range []string{"secret-query", hash, "tma ", "query_id", "deadbeefcafe", "Алиса"} {
		if strings.Contains(logs, leak) {
			t.Fatalf("log leaks %q: %s", leak, logs)
		}
	}

	var entries []map[string]any
	sc := bufio.NewScanner(strings.NewReader(logs))
	for sc.Scan() {
		var e map[string]any
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("log line is not JSON: %s", sc.Text())
		}
		if e["msg"] == "http request" {
			entries = append(entries, e)
		}
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 access log lines, got %d: %s", len(entries), logs)
	}
	first := entries[0]
	if first["route"] != "GET /api/wishes" || first["status"] != float64(200) || first["user_id"] != float64(alice) || first["method"] != "GET" {
		t.Fatalf("unexpected access log entry %v", first)
	}
	if _, ok := first["duration_ms"]; !ok {
		t.Fatalf("missing duration: %v", first)
	}
	if entries[1]["user_id"] != float64(mallory) || entries[1]["status"] != float64(403) {
		t.Fatalf("forbidden attempt must be attributed: %v", entries[1])
	}
	if entries[2]["route"] != "GET /media/{id}/{variant}" {
		t.Fatalf("media route pattern expected: %v", entries[2])
	}
}

func TestMalformedIDsAreNotFound(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/api/wishes/abc", "/api/wishes/0", "/api/wishes/-1", "/api/wishes/007", "/api/recipes/99999999999999999999", "/api/wishes/1/images/x"} {
		method := http.MethodGet
		if strings.Contains(path, "/images/") {
			method = http.MethodDelete
		}
		expectError(t, h.call(method, path, alice, nil), http.StatusNotFound, "not_found", "")
	}
}

func TestUnknownAPIRoutes(t *testing.T) {
	h := newHarness(t)
	expectError(t, h.call(http.MethodGet, "/api/nope", alice, nil), http.StatusNotFound, "not_found", "")
	expectError(t, h.call(http.MethodPost, "/api/me", alice, "{}"), http.StatusNotFound, "not_found", "")
}

func TestNewUsesProductionDefaults(t *testing.T) {
	db := newFakeDB(testNow, maxImg, alice)
	handler := New(Options{
		Services:  db.services(),
		Validator: auth.NewInitDataValidator(testToken, 24*time.Hour, func() time.Time { return testNow }),
		Whitelist: auth.NewWhitelist([]domain.UserID{alice}),
		Signer:    auth.NewMediaSigner(testToken, nil),
	})
	header := http.Header{"Authorization": {tma(alice)}}
	limited := 0
	// The default bucket holds 40 requests and refills at 10/s, so 80 quick
	// requests must run into the limit.
	for range 80 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/categories", nil)
		req.Header = header.Clone()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("default rate limit not applied")
	}
}

func TestNewPanicsWithoutRequiredOptions(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New must reject missing services")
		}
	}()
	New(Options{})
}

// Every registered API route must reject anonymous callers (401) and valid
// but non-whitelisted callers (403) before its handler runs. The route list
// comes from apiRoutes, so a new endpoint is covered automatically.
func TestEveryAPIRouteRequiresAuthAndWhitelist(t *testing.T) {
	concrete := strings.NewReplacer("{id}", "1", "{imageId}", "1")
	for _, rt := range (&server{}).apiRoutes() {
		method, path, ok := strings.Cut(rt.pattern, " ")
		if !ok {
			t.Fatalf("route %q has no method", rt.pattern)
		}
		path = concrete.Replace(path)
		t.Run(rt.pattern, func(t *testing.T) {
			h := newHarness(t)
			expectError(t, h.do(method, path, nil, nil), http.StatusUnauthorized, "unauthorized", "")
			header := http.Header{"Authorization": {tma(mallory)}}
			expectError(t, h.do(method, path, nil, header), http.StatusForbidden, "forbidden", "")
			if len(h.db.touches) != 0 {
				t.Errorf("rejected requests must not reach services, got %+v", h.db.touches)
			}
		})
	}
}
