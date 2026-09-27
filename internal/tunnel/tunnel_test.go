package tunnel

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// scripted serves the given responses in order and then repeats the last one.
// A response starting with "status:" is sent as that bare HTTP status.
type scripted struct {
	mu        sync.Mutex
	responses []string
	calls     int
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/quicktunnel" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	i := min(s.calls, len(s.responses)-1)
	s.calls++
	resp := s.responses[i]
	s.mu.Unlock()
	switch resp {
	case "status:503":
		w.WriteHeader(http.StatusServiceUnavailable)
	case "garbage":
		_, _ = w.Write([]byte("not json"))
	default:
		_, _ = w.Write([]byte(`{"hostname":"` + resp + `"}`))
	}
}

func (s *scripted) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type recorder struct {
	mu   sync.Mutex
	urls []string
}

func (r *recorder) record(u string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urls = append(r.urls, u)
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.urls...)
}

// runScripted runs a fast watcher against the script until it has been
// polled at least `polls` times, then returns every reported URL.
func runScripted(t *testing.T, responses []string, polls int) []string {
	t.Helper()
	script := &scripted{responses: responses}
	srv := httptest.NewServer(script)
	defer srv.Close()

	rec := &recorder{}
	w := &watcher{
		client:     srv.Client(),
		endpoint:   srv.URL + "/quicktunnel",
		minBackoff: time.Millisecond,
		maxBackoff: 4 * time.Millisecond,
		poll:       2 * time.Millisecond,
		log:        slog.New(slog.DiscardHandler),
		onChange:   rec.record,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.run(ctx)
	}()
	deadline := time.After(5 * time.Second)
	for script.callCount() < polls {
		select {
		case <-deadline:
			t.Fatalf("watcher polled only %d times", script.callCount())
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
	return rec.got()
}

func equal(a, b []string) bool {
	return strings.Join(a, "|") == strings.Join(b, "|")
}

func TestWatchDiscoversHostnameAfterRetries(t *testing.T) {
	got := runScripted(t, []string{"", "status:503", "garbage", "", "calm-river-42.trycloudflare.com"}, 12)
	want := []string{"https://calm-river-42.trycloudflare.com/"}
	if !equal(got, want) {
		t.Fatalf("onChange calls = %v, want %v (only once for a stable hostname)", got, want)
	}
}

func TestWatchReportsChangedHostname(t *testing.T) {
	got := runScripted(t, []string{"a-1.trycloudflare.com", "a-1.trycloudflare.com", "status:503", "b-2.trycloudflare.com"}, 10)
	want := []string{"https://a-1.trycloudflare.com/", "https://b-2.trycloudflare.com/"}
	if !equal(got, want) {
		t.Fatalf("onChange calls = %v, want %v", got, want)
	}
}

func TestWatchRejectsInvalidHostnames(t *testing.T) {
	invalid := []string{
		"localhost",
		"Evil.Example.com",
		"evil.com/path",
		"evil.com:8443",
		"user@evil.com",
		"https://evil.com",
		"evil.com?x=1",
		"evil..com",
		"evil.com.",
		"evil com.org",
		"evil.example.com",
		"trycloudflare.com",
		"a.b.trycloudflare.com",
		"x.trycloudflare.com.evil.com",
		"-x.trycloudflare.com",
		strings.Repeat("a", 250) + ".com",
	}
	responses := append(invalid, "ok-host.trycloudflare.com")
	got := runScripted(t, responses, len(responses)+3)
	want := []string{"https://ok-host.trycloudflare.com/"}
	if !equal(got, want) {
		t.Fatalf("onChange calls = %v, want %v", got, want)
	}
}

func TestWatchDoesNotFollowRedirects(t *testing.T) {
	target := httptest.NewServer(&scripted{responses: []string{"evil.trycloudflare.com"}})
	defer target.Close()
	redirector := httptest.NewServer(http.RedirectHandler(target.URL+"/quicktunnel", http.StatusFound))
	defer redirector.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	rec := &recorder{}
	Watch(ctx, redirector.URL, nil, rec.record)
	if got := rec.got(); len(got) != 0 {
		t.Fatalf("redirect must not be followed, got %v", got)
	}
}

func TestWatchStopsWhenContextEnds(t *testing.T) {
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-hang:
		}
	}))
	defer srv.Close()
	defer close(hang)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, srv.URL+"/", nil, func(string) { t.Error("unexpected onChange") })
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return after cancellation")
	}
}

func TestWatchInvalidMetricsURLReturns(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(context.Background(), "http://[::1", nil, func(string) { t.Error("unexpected onChange") })
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch must give up on an invalid metrics URL")
	}
}

func TestIsInternal(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "172.18.0.3": true, "10.1.2.3": true, "192.168.1.5": true,
		"fd00::1": true, "::ffff:172.18.0.3": true,
		"8.8.8.8": false, "1.1.1.1": false, "2606:4700::1111": false, "100.64.0.1": false,
	} {
		if got := isInternal(netip.MustParseAddr(addr)); got != want {
			t.Errorf("isInternal(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestTransportRefusesPublicAddresses(t *testing.T) {
	client := &http.Client{Transport: privateOnlyTransport(), Timeout: 2 * time.Second}
	// The dialer's Control hook rejects the address before any packet is sent,
	// so this is deterministic even without network access.
	_, err := client.Get("http://8.8.8.8:20241/quicktunnel")
	if err == nil || !strings.Contains(err.Error(), "non-private") {
		t.Fatalf("dialing a public address must be refused, got %v", err)
	}
}

// levelCounter counts log records per level.
type levelCounter struct {
	mu     sync.Mutex
	counts map[slog.Level]int
}

func (c *levelCounter) Enabled(context.Context, slog.Level) bool { return true }
func (c *levelCounter) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[r.Level]++
	return nil
}
func (c *levelCounter) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *levelCounter) WithGroup(string) slog.Handler      { return c }

func TestWaitingWarnsOnlyOnce(t *testing.T) {
	counter := &levelCounter{counts: map[slog.Level]int{}}
	w := &watcher{maxBackoff: time.Second, log: slog.New(counter)}
	for range 5 {
		w.logWaiting(context.Background(), nil, time.Second)
	}
	if counter.counts[slog.LevelWarn] != 1 || counter.counts[slog.LevelDebug] != 4 {
		t.Fatalf("want 1 warn + 4 debug, got %v", counter.counts)
	}
}
