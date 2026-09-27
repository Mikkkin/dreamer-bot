package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Errors returned by InitDataValidator.Validate. They never include the
// rejected input, so they are safe to log.
var (
	ErrInitDataMalformed = errors.New("initdata: malformed")
	ErrInitDataHash      = errors.New("initdata: bad hash")
	ErrInitDataExpired   = errors.New("initdata: expired")
)

const (
	// maxInitDataLen bounds the work done for unauthenticated input. Real
	// launch data is well below 2 KiB.
	maxInitDataLen = 8 << 10
	// maxFutureSkew tolerates small clock differences with Telegram.
	maxFutureSkew = time.Minute
	// defaultInitDataMaxAge applies when the caller passes a non-positive
	// max age, so expiry can never be switched off by accident.
	defaultInitDataMaxAge = 24 * time.Hour
)

// WebAppUser is the Telegram profile carried by validated Mini App launch data.
type WebAppUser struct {
	ID           domain.UserID
	FirstName    string
	LastName     string
	Username     string
	LanguageCode string
}

// InitDataValidator checks Telegram Mini App launch data (initData) as
// described in https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app.
// It is safe for concurrent use.
type InitDataValidator struct {
	secret []byte
	maxAge time.Duration
	now    func() time.Time
}

// NewInitDataValidator derives the HMAC secret from the bot token once, so the
// token itself is not kept on the request path. A nil now means time.Now.
func NewInitDataValidator(botToken string, maxAge time.Duration, now func() time.Time) *InitDataValidator {
	if maxAge <= 0 {
		maxAge = defaultInitDataMaxAge
	}
	if now == nil {
		now = time.Now
	}
	return &InitDataValidator{secret: initDataSecret(botToken), maxAge: maxAge, now: now}
}

// initDataSecret is HMAC_SHA256(key="WebAppData", msg=token) per the Telegram spec.
func initDataSecret(botToken string) []byte {
	m := hmac.New(sha256.New, []byte("WebAppData"))
	m.Write([]byte(botToken))
	return m.Sum(nil)
}

// Validate authenticates raw initData and returns the user it describes.
// The signature is checked before any field is trusted; only then are the
// age and the user profile inspected. A valid result proves identity, not
// authorization: callers must still consult the whitelist.
func (v *InitDataValidator) Validate(raw string) (WebAppUser, error) {
	fields, err := v.verify(raw)
	if err != nil {
		return WebAppUser{}, err
	}
	if err := v.checkAge(fields["auth_date"]); err != nil {
		return WebAppUser{}, err
	}
	return parseUser(fields["user"])
}

// verify checks the hash and returns every signed field.
func (v *InitDataValidator) verify(raw string) (map[string]string, error) {
	if raw == "" || len(raw) > maxInitDataLen {
		return nil, ErrInitDataMalformed
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, ErrInitDataMalformed
	}
	fields := make(map[string]string, len(values))
	pairs := make([]string, 0, len(values))
	var gotHash []byte
	for key, vals := range values {
		// A repeated key (also after percent-decoding, e.g. "%75ser") would
		// let an attacker append an unsigned value that a first-value or
		// last-value parser picks up.
		if len(vals) != 1 {
			return nil, ErrInitDataMalformed
		}
		if key == "hash" {
			gotHash, err = hex.DecodeString(vals[0])
			if err != nil || len(gotHash) != sha256.Size {
				return nil, ErrInitDataMalformed
			}
			continue
		}
		// "signature" (the Ed25519 third-party signature) stays in the
		// data-check-string: only "hash" is excluded from the HMAC.
		fields[key] = vals[0]
		pairs = append(pairs, key+"="+vals[0])
	}
	if gotHash == nil {
		return nil, ErrInitDataMalformed
	}
	sort.Strings(pairs)
	m := hmac.New(sha256.New, v.secret)
	m.Write([]byte(strings.Join(pairs, "\n")))
	if !hmac.Equal(m.Sum(nil), gotHash) {
		return nil, ErrInitDataHash
	}
	return fields, nil
}

func (v *InitDataValidator) checkAge(authDate string) error {
	ts, err := strconv.ParseInt(authDate, 10, 64)
	if err != nil || ts <= 0 {
		return ErrInitDataMalformed
	}
	age := v.now().Sub(time.Unix(ts, 0))
	if age > v.maxAge || age < -maxFutureSkew {
		return ErrInitDataExpired
	}
	return nil
}

// initDataUser mirrors the WebAppUser JSON object sent by Telegram.
type initDataUser struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

func parseUser(raw string) (WebAppUser, error) {
	if raw == "" {
		return WebAppUser{}, ErrInitDataMalformed
	}
	var u initDataUser
	if err := json.Unmarshal([]byte(raw), &u); err != nil || u.ID <= 0 {
		return WebAppUser{}, ErrInitDataMalformed
	}
	return WebAppUser{
		ID:           domain.UserID(u.ID),
		FirstName:    u.FirstName,
		LastName:     u.LastName,
		Username:     u.Username,
		LanguageCode: u.LanguageCode,
	}, nil
}
