package auth

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const mediaToken = "123456789:AAEXAMPLEexampleEXAMPLEexample_-12345"

// parseMediaURL splits a signed URL into the values the HTTP handler sees.
func parseMediaURL(t *testing.T, raw string) (id domain.ImageID, variant, exp, sig string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/media/"), "/")
	if !strings.HasPrefix(u.Path, "/media/") || len(parts) != 2 {
		t.Fatalf("unexpected path %q", u.Path)
	}
	n, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		t.Fatalf("id %q: %v", parts[0], err)
	}
	q := u.Query()
	return domain.ImageID(n), parts[1], q.Get("exp"), q.Get("sig")
}

func TestMediaURLFormat(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 5, 0, 0, time.UTC)
	s := NewMediaSigner(mediaToken, fixedNow(now))
	raw := s.URL(42, "thumb")
	wantExp := time.Date(2026, 9, 27, 23, 0, 0, 0, time.UTC).Unix()
	prefix := "/media/42/thumb?exp=" + strconv.FormatInt(wantExp, 10) + "&sig="
	if !strings.HasPrefix(raw, prefix) {
		t.Fatalf("URL = %q, want prefix %q", raw, prefix)
	}
	if sig := strings.TrimPrefix(raw, prefix); len(sig) != 64 {
		t.Fatalf("signature must be 64 hex chars, got %q", sig)
	}
}

func TestMediaRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 5, 0, 0, time.UTC)
	s := NewMediaSigner(mediaToken, fixedNow(now))
	id, variant, exp, sig := parseMediaURL(t, s.URL(42, "full"))
	if !s.Verify(id, variant, exp, sig) {
		t.Fatal("freshly signed URL must verify")
	}
}

func TestMediaVerifyRejects(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 5, 0, 0, time.UTC)
	s := NewMediaSigner(mediaToken, fixedNow(now))
	id, variant, exp, sig := parseMediaURL(t, s.URL(42, "thumb"))
	expUnix, _ := strconv.ParseInt(exp, 10, 64)
	flipped := "0" + sig[1:]
	if sig[0] == '0' {
		flipped = "1" + sig[1:]
	}
	cases := []struct {
		name              string
		id                domain.ImageID
		variant, exp, sig string
	}{
		{"tampered sig", id, variant, exp, flipped},
		{"sig with trailing garbage", id, variant, exp, sig + "00"},
		{"truncated sig", id, variant, exp, sig[:62]},
		{"empty sig", id, variant, exp, ""},
		{"other id", id + 1, variant, exp, sig},
		{"zero id", 0, variant, exp, sig},
		{"other variant", id, "full", exp, sig},
		{"empty variant", id, "", exp, sig},
		{"extended exp", id, variant, strconv.FormatInt(expUnix+3600, 10), sig},
		{"non-canonical exp", id, variant, "0" + exp, sig},
		{"signed exp", id, variant, "+" + exp, sig},
		{"exp not a number", id, variant, "soon", sig},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if s.Verify(c.id, c.variant, c.exp, c.sig) {
				t.Fatal("tampered URL must not verify")
			}
		})
	}
}

func TestMediaExpired(t *testing.T) {
	issued := time.Date(2026, 9, 27, 10, 5, 0, 0, time.UTC)
	id, variant, exp, sig := parseMediaURL(t, NewMediaSigner(mediaToken, fixedNow(issued)).URL(7, "thumb"))
	expiry := time.Date(2026, 9, 27, 23, 0, 0, 0, time.UTC)

	if !NewMediaSigner(mediaToken, fixedNow(expiry)).Verify(id, variant, exp, sig) {
		t.Fatal("URL must still be valid at its expiry second")
	}
	if NewMediaSigner(mediaToken, fixedNow(expiry.Add(time.Second))).Verify(id, variant, exp, sig) {
		t.Fatal("expired URL must not verify")
	}
}

func TestMediaURLStableWithinHour(t *testing.T) {
	cases := []struct {
		name   string
		a, b   time.Time
		stable bool
	}{
		{"same hour", time.Date(2026, 9, 27, 10, 0, 1, 0, time.UTC), time.Date(2026, 9, 27, 10, 59, 59, 0, time.UTC), true},
		{"next hour", time.Date(2026, 9, 27, 10, 30, 0, 0, time.UTC), time.Date(2026, 9, 27, 11, 30, 0, 0, time.UTC), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := NewMediaSigner(mediaToken, fixedNow(c.a)).URL(3, "thumb")
			b := NewMediaSigner(mediaToken, fixedNow(c.b)).URL(3, "thumb")
			if (a == b) != c.stable {
				t.Fatalf("stable = %v, want %v (%q vs %q)", a == b, c.stable, a, b)
			}
		})
	}
}

func TestMediaURLLivesAtLeastTTL(t *testing.T) {
	for _, now := range []time.Time{
		time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 27, 10, 0, 0, 1, time.UTC),
		time.Date(2026, 9, 27, 10, 59, 59, 999, time.UTC),
	} {
		_, _, exp, _ := parseMediaURL(t, NewMediaSigner(mediaToken, fixedNow(now)).URL(1, "full"))
		expUnix, _ := strconv.ParseInt(exp, 10, 64)
		lifetime := time.Unix(expUnix, 0).Sub(now)
		if lifetime < mediaURLTTL || lifetime > mediaURLTTL+time.Hour {
			t.Errorf("now %v: lifetime %v outside [12h, 13h]", now, lifetime)
		}
		if expUnix%3600 != 0 {
			t.Errorf("now %v: exp %d is not on a full hour", now, expUnix)
		}
	}
}

func TestMediaKeyIsDomainSeparated(t *testing.T) {
	s := NewMediaSigner(mediaToken, nil)
	if bytes.Equal(s.key, initDataSecret(mediaToken)) {
		t.Fatal("media key must differ from the initData secret")
	}
	other := NewMediaSigner("987654321:AAEXAMPLEexampleEXAMPLEexample_-99999", nil)
	if bytes.Equal(s.key, other.key) {
		t.Fatal("media key must depend on the bot token")
	}
}
