package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"
)

// fakeBotAPI is a minimal Bot API server: it hands out queued updates via
// getUpdates and records every other method call.
type fakeBotAPI struct {
	mu      sync.Mutex
	queue   []string // raw JSON updates
	calls   []apiCall
	nextMsg int
}

type apiCall struct {
	method string
	form   url.Values
}

func (f *fakeBotAPI) push(updates ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, updates...)
}

func (f *fakeBotAPI) called(method string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c.form)
		}
	}
	return out
}

func (f *fakeBotAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method, ok := strings.CutPrefix(r.URL.Path, "/bot"+testToken+"/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseMultipartForm(1 << 20)
	f.mu.Lock()
	f.calls = append(f.calls, apiCall{method: method, form: r.Form})
	var result string
	switch method {
	case "getMe":
		result = `{"id":42,"is_bot":true,"first_name":"Dreamer","username":"` + botUsername + `"}`
	case "getUpdates":
		result = "[" + strings.Join(f.queue, ",") + "]"
		f.queue = nil
	case "sendMessage":
		f.nextMsg++
		result = fmt.Sprintf(`{"message_id":%d,"date":1,"chat":{"id":%s,"type":"private"}}`, f.nextMsg, r.Form.Get("chat_id"))
	default:
		result = "true"
	}
	f.mu.Unlock()

	if method == "getUpdates" && result == "[]" {
		// A short long-poll so the test does not spin.
		select {
		case <-time.After(20 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"ok":true,"result":%s}`, result)
}

func privateTextUpdate(id int, user int64, text string) string {
	entities := ""
	if strings.HasPrefix(text, "/") {
		entities = fmt.Sprintf(`,"entities":[{"type":"bot_command","offset":0,"length":%d}]`, len(text))
	}
	msg, _ := json.Marshal(text)
	return fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"date":1,"chat":{"id":%d,"type":"private"},`+
		`"from":{"id":%d,"is_bot":false,"first_name":"Дима"},"text":%s%s}}`, id, id, user, user, msg, entities)
}

func TestEndToEndWithLibraryClient(t *testing.T) {
	api := &fakeBotAPI{}
	srv := httptest.NewServer(api)
	defer srv.Close()

	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(&lockedWriter{w: logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	b, err := newWithClientOptions(context.Background(), Options{
		Token:     testToken,
		Whitelist: testWhitelist(),
		WebAppURL: testWebApp,
		Log:       log,
	}, tg.WithServerURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	svc := newFakeServices(time.Now)
	b.Attach(svc.services())

	api.push(
		privateTextUpdate(1, int64(alice), "/help"),
		privateTextUpdate(2, int64(stranger), "секретный текст"),
		`{"update_id":3,"my_chat_member":{"chat":{"id":-100500,"type":"supergroup","title":"x"},`+
			`"from":{"id":999,"is_bot":false,"first_name":"S"},"date":1,`+
			`"old_chat_member":{"status":"left","user":{"id":42,"is_bot":true,"first_name":"Dreamer"}},`+
			`"new_chat_member":{"status":"member","user":{"id":42,"is_bot":true,"first_name":"Dreamer"}}}}`,
		privateTextUpdate(4, int64(alice), "Лампа 30€"),
		`{"update_id":5,"message":{"message_id":5,"date":1,"chat":{"id":-100500,"type":"supergroup"},`+
			`"from":{"id":111,"is_bot":false,"first_name":"Дима"},"text":"групповой секрет"}}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for len(api.called("sendMessage")) < 2 || len(api.called("leaveChat")) < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("updates not processed; calls: %d sendMessage, %d leaveChat",
				len(api.called("sendMessage")), len(api.called("leaveChat")))
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}

	if len(api.called("deleteWebhook")) != 1 || len(api.called("setMyCommands")) != 1 {
		t.Error("bot account not configured")
	}
	menus := api.called("setChatMenuButton")
	if len(menus) != 3 {
		t.Errorf("%d menu buttons set", len(menus))
	}
	for _, m := range menus {
		if mb := m.Get("menu_button"); !strings.Contains(mb, `"type":"web_app"`) || !strings.Contains(mb, testWebApp) {
			t.Errorf("menu_button %s", mb)
		}
	}
	for _, m := range api.called("sendMessage") {
		if m.Get("chat_id") != strconv.FormatInt(int64(alice), 10) {
			t.Errorf("message sent to %s", m.Get("chat_id"))
		}
	}
	if leave := api.called("leaveChat"); len(leave) != 1 || leave[0].Get("chat_id") != "-100500" {
		t.Errorf("leaveChat %v", leave)
	}
	if allowed := api.called("getUpdates")[0].Get("allowed_updates"); !strings.Contains(allowed, "my_chat_member") {
		t.Errorf("allowed_updates %s", allowed)
	}
	if svc.users.touchCount() == 0 {
		t.Error("whitelisted user not touched")
	}
	out := logs.String()
	assertNoToken(t, out)
	for _, secret := range []string{"секретный", "групповой"} {
		if strings.Contains(out, secret) {
			t.Errorf("message content %q in logs", secret)
		}
	}
}

func TestNewRejectsBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	}))
	defer srv.Close()
	_, err := newWithClientOptions(context.Background(), Options{Token: testToken, Whitelist: testWhitelist()},
		tg.WithServerURL(srv.URL))
	if !errors.Is(err, ErrTokenRejected) {
		t.Fatalf("a 401 must map to ErrTokenRejected, got %v", err)
	}
	assertNoToken(t, err.Error())

	// Unreachable server: the transport error carries the request URL.
	srv.Close()
	_, err = newWithClientOptions(context.Background(), Options{Token: testToken, Whitelist: testWhitelist()},
		tg.WithServerURL(srv.URL))
	if err == nil || errors.Is(err, ErrTokenRejected) {
		t.Fatalf("expected a retryable connection error, got %v", err)
	}
	assertNoToken(t, err.Error())
}
