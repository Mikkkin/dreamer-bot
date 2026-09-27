package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const (
	// mediaKeyLabel domain-separates the media URL key from the initData
	// secret, although both are derived from the same bot token.
	mediaKeyLabel = "dreamer/media/v1"
	// mediaURLTTL is the minimum lifetime of a signed media URL.
	mediaURLTTL = 12 * time.Hour
)

// MediaSigner issues and checks signed, expiring media URLs. <img> tags
// cannot send the Authorization header, so the URL itself carries the proof.
// It is safe for concurrent use.
type MediaSigner struct {
	key []byte
	now func() time.Time
}

// NewMediaSigner derives the signing key from the bot token. A nil now means time.Now.
func NewMediaSigner(botToken string, now func() time.Time) *MediaSigner {
	if now == nil {
		now = time.Now
	}
	m := hmac.New(sha256.New, []byte(mediaKeyLabel))
	m.Write([]byte(botToken))
	return &MediaSigner{key: m.Sum(nil), now: now}
}

// URL returns "/media/{id}/{variant}?exp={unix}&sig={hex}". The expiry is
// rounded up to the next full hour, so every URL issued within the same hour
// is identical and the browser cache keeps working across list reloads.
func (s *MediaSigner) URL(id domain.ImageID, variant string) string {
	exp := s.expiry()
	return "/media/" + strconv.FormatInt(int64(id), 10) + "/" + variant +
		"?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + hex.EncodeToString(s.mac(int64(id), variant, exp))
}

// Verify reports whether sig is a valid, unexpired signature for the image
// variant. exp and sig are the raw query values; non-canonical encodings are
// rejected so every URL has exactly one valid form.
func (s *MediaSigner) Verify(id domain.ImageID, variant, exp, sig string) bool {
	if id <= 0 || variant == "" {
		return false
	}
	expUnix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || expUnix <= 0 || strconv.FormatInt(expUnix, 10) != exp {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil || len(got) != sha256.Size {
		return false
	}
	if !hmac.Equal(s.mac(int64(id), variant, expUnix), got) {
		return false
	}
	return s.now().Unix() <= expUnix
}

func (s *MediaSigner) expiry() int64 {
	t := s.now().Add(mediaURLTTL)
	exp := t.Truncate(time.Hour)
	if exp.Before(t) {
		exp = exp.Add(time.Hour)
	}
	return exp.Unix()
}

func (s *MediaSigner) mac(id int64, variant string, exp int64) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(strconv.FormatInt(id, 10) + "/" + variant + "/" + strconv.FormatInt(exp, 10)))
	return m.Sum(nil)
}
