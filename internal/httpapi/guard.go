package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// apiFunc is an /api endpoint. It runs only for an authenticated,
// whitelisted user and returns errors instead of writing them, so that
// classify maps every error in one place.
type apiFunc func(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error

// api wraps an endpoint in the /api pipeline: authenticate the initData
// (401), enforce the whitelist (403), apply the rate limits (429), record
// the profile, then run the endpoint.
func (s *server) api(h apiFunc, limits ...*limiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", "tma")
			writeError(w, errUnauthorized)
			return
		}
		setRequestUser(r.Context(), user.ID)
		if !s.whitelist.Allows(user.ID) {
			writeError(w, errForbidden)
			return
		}
		for _, l := range limits {
			if !l.allow(user.ID) {
				w.Header().Set("Retry-After", "1")
				writeError(w, errRateLimited)
				return
			}
		}
		profile := domain.User{ID: user.ID, FirstName: user.FirstName, LastName: user.LastName, Username: user.Username}
		if err := s.svc.Users.Touch(r.Context(), profile); err != nil {
			s.fail(w, r, fmt.Errorf("touch user: %w", err))
			return
		}
		if err := h(w, r, user); err != nil {
			s.fail(w, r, err)
		}
	})
}

// authenticate validates "Authorization: tma <initData>". Identity comes
// only from the validated data, never from anything else in the request.
func (s *server) authenticate(r *http.Request) (auth.WebAppUser, bool) {
	scheme, raw, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "tma") {
		return auth.WebAppUser{}, false
	}
	user, err := s.validator.Validate(strings.TrimSpace(raw))
	if err != nil {
		// The sentinel errors carry no request data.
		s.log.DebugContext(r.Context(), "rejected Mini App init data", "reason", err)
		return auth.WebAppUser{}, false
	}
	return user, true
}

// rateLimit is a token bucket: every tokens per second, up to burst at once.
type rateLimit struct {
	every rate.Limit
	burst int
}

// limiter keeps one token bucket per user. Buckets idle for longer than
// idle are dropped lazily; by then they have refilled completely, so
// dropping them changes nothing but memory.
type limiter struct {
	limit rateLimit
	idle  time.Duration
	now   func() time.Time

	mu        sync.Mutex
	buckets   map[domain.UserID]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens   *rate.Limiter
	lastSeen time.Time
}

func newLimiter(limit rateLimit, idle time.Duration, now func() time.Time) *limiter {
	return &limiter{limit: limit, idle: idle, now: now, buckets: make(map[domain.UserID]*bucket), lastSweep: now()}
}

func (l *limiter) allow(id domain.UserID) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) >= l.idle {
		for key, b := range l.buckets {
			if now.Sub(b.lastSeen) >= l.idle {
				delete(l.buckets, key)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[id]
	if !ok {
		b = &bucket{tokens: rate.NewLimiter(l.limit.every, l.limit.burst)}
		l.buckets[id] = b
	}
	b.lastSeen = now
	return b.tokens.AllowN(now, 1)
}
