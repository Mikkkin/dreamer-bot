package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
)

const token = "123456789:AAEXAMPLEexampleEXAMPLEexample_-12345"

type stringer struct{ s string }

func (s stringer) String() string { return s.s }

type valuer struct{ secret string }

func (v valuer) LogValue() slog.Value { return slog.StringValue("token=" + v.secret) }

type payload struct {
	URL  string            `json:"url"`
	Tags map[string]string `json:"tags"`
}

func TestRedactsSecrets(t *testing.T) {
	downloadErr := &url.Error{
		Op:  "Get",
		URL: "https://api.telegram.org/file/bot" + token + "/photos/file_1.jpg",
		Err: errors.New("dial tcp: i/o timeout"),
	}
	cases := []struct {
		name string
		log  func(l *slog.Logger)
	}{
		{"url.Error attr", func(l *slog.Logger) { l.Error("download failed", "err", downloadErr) }},
		{"wrapped url.Error", func(l *slog.Logger) { l.Error("download failed", "err", fmt.Errorf("fetch photo: %w", downloadErr)) }},
		{"message", func(l *slog.Logger) { l.Info("token is " + token) }},
		{"string attr", func(l *slog.Logger) { l.Info("x", "link", "https://api.telegram.org/bot"+token+"/getMe") }},
		{"percent-encoded", func(l *slog.Logger) { l.Info("x", "q", "t="+url.QueryEscape(token)) }},
		{"stringer", func(l *slog.Logger) { l.Info("x", "v", stringer{"bot" + token}) }},
		{"log valuer", func(l *slog.Logger) { l.Info("x", "v", valuer{token}) }},
		{"group", func(l *slog.Logger) {
			l.Info("x", slog.Group("req", "url", token, slog.Group("inner", "err", downloadErr)))
		}},
		{"with attrs", func(l *slog.Logger) { l.With("secret", token).Info("x") }},
		{"with group", func(l *slog.Logger) { l.WithGroup("g").Info("x", "secret", token) }},
		{"struct value", func(l *slog.Logger) { l.Info("x", "p", payload{URL: "https://x/" + token}) }},
		{"map key", func(l *slog.Logger) { l.Info("x", "p", payload{Tags: map[string]string{token: "v"}}) }},
		{"attr key", func(l *slog.Logger) { l.Info("x", token, 1) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			c.log(New(&buf, slog.LevelDebug, token))
			out := buf.String()
			if strings.Contains(out, token) || strings.Contains(out, url.QueryEscape(token)) {
				t.Fatalf("secret leaked: %s", out)
			}
			if !strings.Contains(out, Redacted) {
				t.Fatalf("expected %s marker: %s", Redacted, out)
			}
			if !json.Valid(bytes.TrimSpace(buf.Bytes())) {
				t.Fatalf("output is not JSON: %s", out)
			}
		})
	}
}

func TestKeepsContextAroundSecret(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, slog.LevelInfo, token)
	l.Error("download failed", "err", &url.Error{Op: "Get", URL: "https://api.telegram.org/file/bot" + token + "/a.jpg", Err: errors.New("timeout")})

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	want := `Get "https://api.telegram.org/file/bot[REDACTED]/a.jpg": timeout`
	if rec["err"] != want {
		t.Fatalf("err = %q, want %q", rec["err"], want)
	}
}

func TestLongestSecretWins(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, slog.LevelInfo, "abc", "abcdef").Info("value abcdefg")
	if !strings.Contains(buf.String(), `"value [REDACTED]g"`) {
		t.Fatalf("unexpected output %s", buf.String())
	}
}

func TestNoSecretsLeavesOutputAlone(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, slog.LevelInfo, "", "").Info("hello", "n", 1, "p", payload{URL: "https://example.com"})
	out := buf.String()
	if strings.Contains(out, Redacted) || !strings.Contains(out, `"url":"https://example.com"`) {
		t.Fatalf("unexpected output %s", out)
	}
}

func TestUnrelatedValuesKeepTheirShape(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, slog.LevelInfo, token).Info("x", "p", payload{URL: "https://example.com"}, "err", errors.New("boom"))
	var rec struct {
		P   payload `json:"p"`
		Err string  `json:"err"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("unmarshal %s: %v", buf.String(), err)
	}
	if rec.P.URL != "https://example.com" || rec.Err != "boom" {
		t.Fatalf("unexpected record %+v", rec)
	}
}

func TestLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, slog.LevelWarn, token)
	l.Info("hidden")
	l.Warn("shown")
	if strings.Contains(buf.String(), "hidden") || !strings.Contains(buf.String(), "shown") {
		t.Fatalf("level not applied: %s", buf.String())
	}
}
