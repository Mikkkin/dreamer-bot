package bot

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

type notifyEnv struct {
	n    *notifier
	api  *fakeAPI
	svc  *fakeServices
	logs *bytes.Buffer
}

func newNotifyEnv(t *testing.T, delay time.Duration) *notifyEnv {
	t.Helper()
	api := newFakeAPI()
	svc := newFakeServices(newClock().Now)
	logs := &bytes.Buffer{}
	web := &webApp{}
	web.set(testWebApp)
	n := newNotifier(api, web, slog.New(slog.NewTextHandler(&lockedWriter{w: logs}, nil)))
	n.delay = delay
	n.timeout = 5 * time.Second
	n.attach(svc.services())
	return &notifyEnv{n: n, api: api, svc: svc, logs: logs}
}

func recipients() service.Recipients {
	return service.Recipients{
		Actor: domain.User{ID: alice, FirstName: "Дима"},
		To:    []domain.User{{ID: bob, FirstName: "Аня", HasChat: true}},
	}
}

func TestNotifierWishCreatedReloadsFinalState(t *testing.T) {
	e := newNotifyEnv(t, 0)
	w, err := e.svc.wishes.Create(context.Background(), alice, domain.WishDraft{Title: "Черновое"})
	if err != nil {
		t.Fatal(err)
	}
	// The author renames the wish and adds a cover before the delay ends.
	final := w
	final.Title = "Поездка в Токио"
	final.Price = &domain.Money{Minor: 120000, Currency: "EUR"}
	final.Images = []domain.Image{{ID: 9}}
	e.svc.wishes.put(final)
	e.svc.images.data[9] = []byte("jpeg")

	e.n.WishCreated(context.Background(), recipients(), w)
	e.n.drain()

	photos := e.api.of("SendPhoto")
	if len(photos) != 1 {
		t.Fatalf("%d photos sent", len(photos))
	}
	p := photos[0].(*tg.SendPhotoParams)
	want := "💫 <b>Дима</b> · новое желание\n«Поездка в Токио» · " + final.Price.Format()
	if p.ChatID != int64(bob) || p.Caption != want {
		t.Errorf("sent %v %q, want %q", p.ChatID, p.Caption, want)
	}
	if urls := webAppURLs(p.ReplyMarkup); !slices.Equal(urls, []string{testWebApp + "?wish=1"}) {
		t.Errorf("open button %v", urls)
	}
}

func TestNotifierSkipsDeletedEntities(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.WishCreated(context.Background(), recipients(), domain.Wish{ID: 404, Title: "Удалено"})
	e.n.RecipeCreated(context.Background(), recipients(), domain.Recipe{ID: 404, Title: "Удалено"})
	e.n.drain()
	if calls := e.api.all(); len(calls) != 0 {
		t.Errorf("notified about deleted items: %+v", calls)
	}
}

func TestNotifierNeverBlocksCaller(t *testing.T) {
	e := newNotifyEnv(t, time.Hour) // created notices would wait for an hour
	e.api.release = make(chan struct{})

	start := time.Now()
	e.n.WishFulfilled(context.Background(), recipients(), domain.Wish{ID: 1, Title: "Мечта"})
	e.n.WishCreated(context.Background(), recipients(), domain.Wish{ID: 1, Title: "Мечта"})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("notifier blocked the caller for %v", elapsed)
	}

	// Shutdown cuts the delay short and waits for delivery to finish.
	drained := make(chan struct{})
	go func() {
		e.n.drain()
		close(drained)
	}()
	select {
	case <-drained:
		t.Fatal("drain returned while a delivery was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(e.api.release)
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not finish")
	}
	if n := e.api.count("SendMessage"); n != 1 {
		// The wish with ID 1 does not exist in the fake store, so only the
		// fulfilled notice (which is not reloaded) is delivered.
		t.Errorf("%d messages sent, want 1", n)
	}
}

func TestNotifierSurvivesCallerCancellation(t *testing.T) {
	e := newNotifyEnv(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	e.n.WishFulfilled(ctx, recipients(), domain.Wish{ID: 1, Title: "Мечта"})
	cancel() // the HTTP request that triggered the notice is over
	e.n.drain()
	sent := e.api.of("SendMessage")
	if len(sent) != 1 || !strings.Contains(sent[0].(*tg.SendMessageParams).Text, "отметил(а) Дима") {
		t.Errorf("sent %+v", sent)
	}
}

func TestNotifierLogsDeliveryErrors(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.api.fail["SendMessage"] = fmt.Errorf("%w, Forbidden: bot was blocked by the user", tg.ErrorForbidden)
	e.n.WishFulfilled(context.Background(), recipients(), domain.Wish{ID: 1, Title: "Мечта"})
	e.n.drain()
	if !strings.Contains(e.logs.String(), "notification failed") {
		t.Errorf("error not logged: %s", e.logs.String())
	}
}

func TestNotifierDropsAfterDrain(t *testing.T) {
	e := newNotifyEnv(t, 0)
	e.n.drain()
	e.n.WishFulfilled(context.Background(), recipients(), domain.Wish{ID: 1, Title: "Мечта"})
	e.n.drain()
	if len(e.api.all()) != 0 {
		t.Error("notice delivered after shutdown")
	}
}

func TestNotifierEscapesUserText(t *testing.T) {
	e := newNotifyEnv(t, 0)
	r := recipients()
	r.Actor.FirstName = "<b>Злодей</b>"
	e.n.WishFulfilled(context.Background(), r, domain.Wish{ID: 1, Title: scriptInjected})
	e.n.drain()
	text := e.api.lastSent(t).Text
	if strings.Contains(text, "<script>") || strings.Contains(text, "<b>Злодей") {
		t.Errorf("unescaped notification %q", text)
	}
}

// lockedWriter makes a bytes.Buffer safe for concurrent log writes.
type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
