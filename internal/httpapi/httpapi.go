// Package httpapi serves the Mini App over HTTP: the JSON API under /api
// (contract: docs/API.md), signed media URLs, the embedded static app and a
// health probe. It is a thin adapter: every business rule lives in the
// service layer, and domain errors are mapped to HTTP in one place.
package httpapi

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/nutrition"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// Options configures the handler. Services, Validator and Signer are required.
type Options struct {
	Services  *service.Services
	Validator *auth.InitDataValidator
	Whitelist auth.Whitelist
	Signer    *auth.MediaSigner
	// Static is the compiled Mini App (webapp.FS()). Without an index.html a
	// small "not built" page is served at /.
	Static          fs.FS
	DefaultCurrency domain.Currency
	MaxImageBytes   int64
	// Nutrition is the food table behind nutrition_auto in recipes; nil
	// means nutrition.Default().
	Nutrition *nutrition.Table
	Log       *slog.Logger
	// Health reports readiness for /healthz, e.g. a database ping. Nil means
	// that a running process is healthy.
	Health func(ctx context.Context) error
}

const (
	defaultMaxImageBytes = 10 << 20
	healthTimeout        = 2 * time.Second
)

// tuning holds knobs that production never changes but tests do.
type tuning struct {
	apiRate     rateLimit
	uploadRate  rateLimit
	limiterIdle time.Duration
	now         func() time.Time

	// externalRate paces requests that make the server call an external
	// service on the user's behalf (routes marked external).
	externalRate rateLimit
	// importDeadline bounds one recipe import.
	importDeadline time.Duration
}

func defaultTuning() tuning {
	return tuning{
		// A list screen issues a handful of requests at once; 40 absorbs a
		// burst of navigation while 10/s stops a runaway client.
		apiRate: rateLimit{every: 10, burst: 40},
		// Uploads decode and re-encode images, which is CPU heavy.
		uploadRate:  rateLimit{every: 1, burst: 10},
		limiterIdle: 10 * time.Minute,
		now:         time.Now,

		// The burst absorbs a few retries, then a call every two seconds
		// keeps the external service from seeing a flood from one user.
		externalRate: rateLimit{every: 0.5, burst: 6},
		// Reading a post takes seconds; the video path (download, upload
		// to the model, waiting for it) may need up to ~90 s.
		importDeadline: 120 * time.Second,
	}
}

// New builds the HTTP handler. It panics when a required option is missing,
// which is a wiring error that must fail at startup.
func New(o Options) http.Handler { return newHandler(o, defaultTuning()) }

func newHandler(o Options, t tuning) http.Handler {
	return newServer(o, t).routes()
}

type server struct {
	svc         *service.Services
	validator   *auth.InitDataValidator
	whitelist   auth.Whitelist
	signer      *auth.MediaSigner
	currency    domain.Currency
	maxImage    int64
	food        *nutrition.Table
	health      func(context.Context) error
	log         *slog.Logger
	static      *staticSite
	apiLimit    *limiter
	uploadLimit *limiter
	now         func() time.Time

	externalLimit  *limiter
	importDeadline time.Duration
}

func newServer(o Options, t tuning) *server {
	switch {
	case o.Services == nil:
		panic("httpapi: Options.Services is required")
	case o.Validator == nil:
		panic("httpapi: Options.Validator is required")
	case o.Signer == nil:
		panic("httpapi: Options.Signer is required")
	}
	s := &server{
		svc:         o.Services,
		validator:   o.Validator,
		whitelist:   o.Whitelist,
		signer:      o.Signer,
		currency:    o.DefaultCurrency,
		maxImage:    o.MaxImageBytes,
		food:        o.Nutrition,
		health:      o.Health,
		log:         o.Log,
		static:      newStaticSite(o.Static),
		apiLimit:    newLimiter(t.apiRate, t.limiterIdle, t.now),
		uploadLimit: newLimiter(t.uploadRate, t.limiterIdle, t.now),
		now:         t.now,

		externalLimit:  newLimiter(t.externalRate, t.limiterIdle, t.now),
		importDeadline: t.importDeadline,
	}
	if s.currency == "" {
		s.currency = domain.Currencies[0]
	}
	if s.maxImage <= 0 {
		s.maxImage = defaultMaxImageBytes
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.food == nil {
		s.food = nutrition.Default()
	}
	return s
}

// routes registers every endpoint and wraps the mux in the outer middleware:
// recover, then security headers, then the access log.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range s.apiRoutes() {
		limits := []*limiter{s.apiLimit}
		if rt.upload {
			limits = append(limits, s.uploadLimit)
		}
		if rt.external {
			limits = append(limits, s.externalLimit)
		}
		mux.Handle(rt.pattern, s.api(rt.handler, limits...))
	}

	// Unknown API and media paths get a 404 of their own so that they never
	// fall through to the static app.
	mux.HandleFunc("/api/", notFound)
	mux.HandleFunc("GET /media/{id}/{variant}", s.media)
	mux.HandleFunc("/media/", notFound)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.Handle("/", s.static)

	return s.recoverPanics(securityHeaders(s.accessLog(mux)))
}

func notFound(w http.ResponseWriter, _ *http.Request) { writeError(w, errNotFound) }

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if s.health != nil {
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		defer cancel()
		if err := s.health(ctx); err != nil {
			s.log.WarnContext(ctx, "health check failed", "err", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "unavailable")
			return
		}
	}
	_, _ = io.WriteString(w, "ok")
}

// apiRoute is one authenticated JSON endpoint. Every route in apiRoutes is
// wrapped by s.api (initData check, whitelist, rate limit); a test iterates
// this table to prove that no API route is reachable without them.
type apiRoute struct {
	pattern string
	handler apiFunc
	upload  bool // also subject to the stricter upload rate limit
	// external: also subject to the rate limit for calls to an external
	// service.
	external bool
}

func (s *server) apiRoutes() []apiRoute {
	return []apiRoute{
		{pattern: "GET /api/me", handler: s.me},
		{pattern: "GET /api/stats", handler: s.stats},

		{pattern: "GET /api/wishes", handler: s.listWishes},
		{pattern: "POST /api/wishes", handler: s.createWish},
		{pattern: "GET /api/wishes/{id}", handler: s.getWish},
		{pattern: "PATCH /api/wishes/{id}", handler: s.patchWish},
		{pattern: "PUT /api/wishes/{id}/status", handler: s.setWishStatus},
		{pattern: "DELETE /api/wishes/{id}", handler: s.deleteWish},
		{pattern: "POST /api/wishes/{id}/images", handler: s.addWishImage, upload: true},
		{pattern: "DELETE /api/wishes/{id}/images/{imageId}", handler: s.removeWishImage},
		{pattern: "GET /api/wishes/{id}/savings", handler: s.listSavings},
		{pattern: "POST /api/wishes/{id}/savings", handler: s.addSaving},
		{pattern: "DELETE /api/wishes/{id}/savings/{savingId}", handler: s.removeSaving},

		{pattern: "GET /api/categories", handler: s.listCategories},
		{pattern: "POST /api/categories", handler: s.createCategory},
		{pattern: "PATCH /api/categories/{id}", handler: s.patchCategory},
		{pattern: "DELETE /api/categories/{id}", handler: s.deleteCategory},

		{pattern: "GET /api/recipes", handler: s.listRecipes},
		{pattern: "GET /api/recipes/random", handler: s.randomRecipe},
		{pattern: "POST /api/recipes", handler: s.createRecipe},
		{pattern: "POST /api/recipes/import", handler: s.importRecipe, external: true},
		{pattern: "GET /api/recipes/{id}", handler: s.getRecipe},
		{pattern: "PATCH /api/recipes/{id}", handler: s.patchRecipe},
		{pattern: "DELETE /api/recipes/{id}", handler: s.deleteRecipe},
		{pattern: "POST /api/recipes/{id}/images", handler: s.addRecipeImage, upload: true},
		{pattern: "DELETE /api/recipes/{id}/images/{imageId}", handler: s.removeRecipeImage},
		{pattern: "POST /api/recipes/{id}/cooks", handler: s.cookRecipe},
		{pattern: "GET /api/recipes/{id}/cooks", handler: s.listCooks},
		{pattern: "PUT /api/recipes/{id}/cooks/{cookId}/rating", handler: s.rateCook},
		{pattern: "DELETE /api/recipes/{id}/cooks/{cookId}", handler: s.removeCook},
		{pattern: "POST /api/recipes/{id}/shopping", handler: s.addRecipeToShopping},

		{pattern: "GET /api/recipe-tags", handler: s.listRecipeTags},
		{pattern: "POST /api/recipe-tags", handler: s.createRecipeTag},
		{pattern: "PATCH /api/recipe-tags/{id}", handler: s.patchRecipeTag},
		{pattern: "DELETE /api/recipe-tags/{id}", handler: s.deleteRecipeTag},

		{pattern: "GET /api/shopping", handler: s.listShopping},
		{pattern: "POST /api/shopping", handler: s.addShopping},
		{pattern: "PATCH /api/shopping/{id}", handler: s.patchShopping},
		{pattern: "DELETE /api/shopping/{id}", handler: s.deleteShopping},
		{pattern: "POST /api/shopping/clear-checked", handler: s.clearChecked},
	}
}
