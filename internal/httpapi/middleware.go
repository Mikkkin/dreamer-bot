package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const (
	// contentSecurityPolicy allows the Telegram SDK script and Telegram Web
	// as the only framing origin. The native clients use WebViews without
	// framing, so frame-ancestors does not affect them; X-Frame-Options is
	// deliberately absent because it would break Telegram Web.
	contentSecurityPolicy = "default-src 'none'; script-src 'self' https://telegram.org; " +
		"style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; connect-src 'self'; " +
		"font-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'none'; " +
		"object-src 'none'; frame-src 'none'; frame-ancestors https://web.telegram.org"
	permissionsPolicy = "camera=(), microphone=(), geolocation=(), payment=(), usb=()"
)

// statusWriter remembers the status code written by the wrapped handler.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// recoverPanics turns a handler panic into a generic 500. The stack goes to
// the log only, never into the response.
func (s *server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(p)
			}
			s.log.ErrorContext(r.Context(), "panic while serving request",
				"method", r.Method, "path", r.URL.Path, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			if sw.status != 0 {
				// The response has started; abort the connection rather than
				// let the client take a truncated body for a complete one.
				panic(http.ErrAbortHandler)
			}
			h := w.Header()
			for _, k := range []string{"Content-Length", "Content-Range", "Content-Disposition", "Etag", "Last-Modified"} {
				h.Del(k)
			}
			h.Set("Cache-Control", "no-store")
			writeError(sw, errInternal)
		}()
		next.ServeHTTP(sw, r)
	})
}

// securityHeaders sets the headers every response carries.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Permissions-Policy", permissionsPolicy)
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Cache-Control", cachePolicy(r.URL.Path))
		next.ServeHTTP(w, r)
	})
}

// cachePolicy is the default Cache-Control for a path. Handlers override it
// where they know better: media after a valid signature, static 404s.
func cachePolicy(path string) string {
	switch {
	case strings.HasPrefix(path, "/assets/"):
		// Vite puts a content hash into every asset name.
		return "public, max-age=31536000, immutable"
	case strings.HasPrefix(path, "/api/"), strings.HasPrefix(path, "/media/"), path == "/healthz":
		return "no-store"
	default:
		// index.html must be revalidated: Telegram WebViews cache
		// aggressively, and a stale index pins a stale bundle.
		return "no-cache"
	}
}

// requestInfo collects what the access log reports but only inner layers
// know, such as the authenticated user.
type requestInfo struct {
	userID domain.UserID
}

type requestInfoKey struct{}

func setRequestUser(ctx context.Context, id domain.UserID) {
	if info, ok := ctx.Value(requestInfoKey{}).(*requestInfo); ok {
		info.userID = id
	}
}

// accessLog logs one line per request: method, route pattern, status,
// duration and the user when known. Query strings, headers and bodies are
// never logged: they carry initData, signatures and personal data.
func (s *server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		info := &requestInfo{}
		r = r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info))
		sw := &statusWriter{ResponseWriter: w}
		completed := false
		defer func() {
			status := sw.status
			switch {
			case !completed:
				// A panic is unwinding; recoverPanics answers with 500.
				status = http.StatusInternalServerError
			case status == 0:
				status = http.StatusOK
			}
			route := r.Pattern // set by the mux on this request value
			if route == "" {
				route = "-"
			}
			level := slog.LevelInfo
			if route == "GET /healthz" {
				level = slog.LevelDebug
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", status),
				slog.Float64("duration_ms", float64(s.now().Sub(start).Microseconds())/1000),
			}
			if info.userID != 0 {
				attrs = append(attrs, slog.Int64("user_id", int64(info.userID)))
			}
			s.log.LogAttrs(r.Context(), level, "http request", attrs...)
		}()
		next.ServeHTTP(sw, r)
		completed = true
	})
}
