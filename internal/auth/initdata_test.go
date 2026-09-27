package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Public fixture from init-data-golang's validate_test.go (not a real token).
const (
	vectorToken    = "5768337691:AAH5YkoiEuPk8-FZa32hStHTqXiLPtAEhx8"
	vectorInitData = "query_id=AAHdF6IQAAAAAN0XohDhrOrc&user=%7B%22id%22%3A279058397%2C%22first_name%22%3A%22Vladislav%22%2C%22last_name%22%3A%22Kibenko%22%2C%22username%22%3A%22vdkfrost%22%2C%22language_code%22%3A%22ru%22%2C%22is_premium%22%3Atrue%7D&auth_date=1662771648&hash=c501b71e775f74ce10e377dea85a7ea24ecd640b223ea86dfe453e0eaed2e2b2"
	vectorAuthDate = 1662771648
)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

// sign builds initData the way Telegram does: every field except hash goes
// into the sorted data-check-string.
func sign(token string, fields map[string]string) string {
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
	m := hmac.New(sha256.New, secret.Sum(nil))
	m.Write([]byte(strings.Join(pairs, "\n")))
	q.Set("hash", hex.EncodeToString(m.Sum(nil)))
	return q.Encode()
}

func TestValidatePublicVector(t *testing.T) {
	v := NewInitDataValidator(vectorToken, 24*time.Hour, fixedNow(time.Unix(vectorAuthDate, 0)))
	u, err := v.Validate(vectorInitData)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := WebAppUser{ID: 279058397, FirstName: "Vladislav", LastName: "Kibenko", Username: "vdkfrost", LanguageCode: "ru"}
	if u != want {
		t.Fatalf("user = %+v, want %+v", u, want)
	}
}

func TestValidateRejects(t *testing.T) {
	now := time.Unix(vectorAuthDate, 0)
	tamperedHash := vectorInitData[:len(vectorInitData)-1] + "3"
	cases := []struct {
		name string
		now  time.Time
		raw  string
		want error
	}{
		{"empty", now, "", ErrInitDataMalformed},
		{"tampered hash", now, tamperedHash, ErrInitDataHash},
		{"tampered field", now, strings.Replace(vectorInitData, "Vladislav", "Mallory", 1), ErrInitDataHash},
		{"wrong token", now, sign("1:other", map[string]string{"auth_date": "1662771648", "user": `{"id":1}`}), ErrInitDataHash},
		{"duplicate key appended", now, vectorInitData + "&user=%7B%22id%22%3A1%7D", ErrInitDataMalformed},
		{"duplicate key percent-encoded", now, vectorInitData + "&%75ser=%7B%22id%22%3A1%7D", ErrInitDataMalformed},
		{"duplicate hash", now, vectorInitData + "&hash=00", ErrInitDataMalformed},
		{"missing hash", now, strings.Split(vectorInitData, "&hash=")[0], ErrInitDataMalformed},
		{"hash not hex", now, strings.Split(vectorInitData, "&hash=")[0] + "&hash=zz", ErrInitDataMalformed},
		{"hash wrong length", now, strings.Split(vectorInitData, "&hash=")[0] + "&hash=abcd", ErrInitDataMalformed},
		{"semicolon separator", now, strings.Replace(vectorInitData, "&", ";", 1), ErrInitDataMalformed},
		{"expired", now.Add(24*time.Hour + time.Second), vectorInitData, ErrInitDataExpired},
		{"future auth_date", now.Add(-2 * time.Minute), vectorInitData, ErrInitDataExpired},
		{"oversized", now, vectorInitData + "&pad=" + strings.Repeat("a", maxInitDataLen), ErrInitDataMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewInitDataValidator(vectorToken, 24*time.Hour, fixedNow(c.now))
			u, err := v.Validate(c.raw)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if u != (WebAppUser{}) {
				t.Fatalf("rejected data must not yield a user, got %+v", u)
			}
		})
	}
}

func TestValidateAllowsSmallClockSkew(t *testing.T) {
	v := NewInitDataValidator(vectorToken, 24*time.Hour, fixedNow(time.Unix(vectorAuthDate, 0).Add(-30*time.Second)))
	if _, err := v.Validate(vectorInitData); err != nil {
		t.Fatalf("30s of skew must be tolerated: %v", err)
	}
}

func TestValidateSignedFields(t *testing.T) {
	const token = "123456789:AAEXAMPLEexampleEXAMPLEexample_-12345"
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	authDate := strconv.FormatInt(now.Add(-time.Hour).Unix(), 10)
	v := NewInitDataValidator(token, 24*time.Hour, fixedNow(now))

	t.Run("signature field is part of the check string", func(t *testing.T) {
		fields := map[string]string{
			"auth_date": authDate,
			"query_id":  "AAHdF6IQAAAAAN0XohDhrOrc",
			"user":      `{"id":111,"first_name":"Дима","username":"dima"}`,
			"signature": "6fbdaab833d39f54518bd5c3eb3f511d035e68cb",
		}
		u, err := v.Validate(sign(token, fields))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if u.ID != 111 || u.FirstName != "Дима" || u.Username != "dima" {
			t.Fatalf("unexpected user %+v", u)
		}

		// Signing without "signature" and appending it afterwards must fail:
		// the field is covered by the HMAC.
		withoutSig := map[string]string{"auth_date": authDate, "user": fields["user"]}
		if _, err := v.Validate(sign(token, withoutSig) + "&signature=abc"); !errors.Is(err, ErrInitDataHash) {
			t.Fatalf("unsigned signature field accepted: %v", err)
		}
	})

	bad := []struct {
		name   string
		fields map[string]string
	}{
		{"no user", map[string]string{"auth_date": authDate}},
		{"user not json", map[string]string{"auth_date": authDate, "user": "nope"}},
		{"zero user id", map[string]string{"auth_date": authDate, "user": `{"id":0}`}},
		{"negative user id", map[string]string{"auth_date": authDate, "user": `{"id":-7}`}},
		{"no auth_date", map[string]string{"user": `{"id":1}`}},
		{"auth_date not a number", map[string]string{"auth_date": "yesterday", "user": `{"id":1}`}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if _, err := v.Validate(sign(token, c.fields)); !errors.Is(err, ErrInitDataMalformed) {
				t.Fatalf("err = %v, want %v", err, ErrInitDataMalformed)
			}
		})
	}
}

func TestNonPositiveMaxAgeKeepsExpiry(t *testing.T) {
	v := NewInitDataValidator(vectorToken, 0, fixedNow(time.Unix(vectorAuthDate, 0).Add(48*time.Hour)))
	if _, err := v.Validate(vectorInitData); !errors.Is(err, ErrInitDataExpired) {
		t.Fatalf("maxAge 0 must not disable expiry, got %v", err)
	}
}
