package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/bot"
	"github.com/Mikkkin/dreamer-bot/internal/config"
	"github.com/Mikkkin/dreamer-bot/internal/httpapi"
	"github.com/Mikkkin/dreamer-bot/internal/logging"
	"github.com/Mikkkin/dreamer-bot/internal/media"
	"github.com/Mikkkin/dreamer-bot/internal/nutrition"
	"github.com/Mikkkin/dreamer-bot/internal/recipeimport"
	"github.com/Mikkkin/dreamer-bot/internal/service"
	"github.com/Mikkkin/dreamer-bot/internal/storage/sqlite"
	"github.com/Mikkkin/dreamer-bot/internal/tunnel"
	"github.com/Mikkkin/dreamer-bot/internal/webapp"
)

const (
	// httpShutdownTimeout bounds draining HTTP requests; botShutdownTimeout
	// then lets in-flight bot saves (photo downloads) and partner
	// notifications finish. Together they stay below the 40s
	// stop_grace_period in compose.yaml, after which Docker sends SIGKILL.
	httpShutdownTimeout = 10 * time.Second
	botShutdownTimeout  = 28 * time.Second
	botRetryMin         = time.Second
	botRetryMax         = time.Minute
	// botHealthyRun is how long polling must have worked before the retry
	// delay starts over from botRetryMin.
	botHealthyRun = 5 * time.Minute
)

func serve(getenv func(string) string, stderr io.Writer) int {
	cfg, err := config.Load(getenv)
	if err != nil {
		// Configuration errors never include secret values.
		logging.New(stderr, slog.LevelInfo).Error("invalid configuration", "err", err)
		return 1
	}
	// Redact the whole token and, separately, its secret half (after the ":"),
	// which is sensitive on its own, and the optional LLM API key.
	log := logging.New(stderr, cfg.LogLevel, cfg.BotToken, tokenSecret(cfg.BotToken), cfg.LLM.APIKey)
	// Route the standard library logger and any library using slog's
	// default through the redacting handler as well.
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runService(ctx, cfg, log); err != nil && ctx.Err() == nil {
		log.Error("dreamer stopped", "err", err)
		return 1
	}
	log.Info("dreamer stopped")
	return 0
}

// runService wires every component, runs the HTTP server and the bot until
// ctx is cancelled, then shuts down in order: bot, HTTP, database.
func runService(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	logStartup(cfg, log)
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	db, err := sqlite.Open(ctx, filepath.Join(cfg.DataDir, "dreamer.db"), log.With("component", "sqlite"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("close database", "err", err)
		}
	}()

	store, err := media.NewStore(filepath.Join(cfg.DataDir, "images"), cfg.MaxImageBytes)
	if err != nil {
		return fmt.Errorf("open image store: %w", err)
	}

	// Validated by config.Load; nil when no API key is set.
	llm, err := recipeimport.NewLLM(cfg.LLM)
	if err != nil {
		return fmt.Errorf("LLM settings: %w", err)
	}

	whitelist := auth.NewWhitelist(cfg.AllowedUsers)
	bots := newBotSupervisor(bot.Options{
		Token:           cfg.BotToken,
		Whitelist:       whitelist,
		WebAppURL:       cfg.WebAppURL,
		DefaultCurrency: cfg.DefaultCurrency,
		MaxImageBytes:   cfg.MaxImageBytes,
		Log:             log.With("component", "bot"),
	}, log.With("component", "bot"))

	services, err := service.New(service.Deps{
		Repos:     db,
		Media:     store,
		Notifier:  bots.Notifier(),
		Importer:  newRecipeImporter(llm, log.With("component", "import")),
		Whitelist: cfg.AllowedUsers,
		Log:       log.With("component", "service"),
	})
	if err != nil {
		return fmt.Errorf("build services: %w", err)
	}
	srv := &http.Server{
		Handler: httpapi.New(httpapi.Options{
			Services:        services,
			Validator:       auth.NewInitDataValidator(cfg.BotToken, cfg.InitDataMaxAge, time.Now),
			Whitelist:       whitelist,
			Signer:          auth.NewMediaSigner(cfg.BotToken, time.Now),
			Static:          webapp.FS(),
			DefaultCurrency: cfg.DefaultCurrency,
			MaxImageBytes:   cfg.MaxImageBytes,
			Nutrition:       nutrition.Default(),
			Log:             log.With("component", "http"),
			Health:          db.Ping,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		// The recipe import route extends both for its own requests.
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 16 << 10,
		ErrorLog:       slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	// Listen before starting anything else so that a busy port fails fast.
	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	httpErr := make(chan error, 1)
	go func() { httpErr <- srv.Serve(ln) }()
	log.Info("http server listening", "addr", ln.Addr().String())

	botCtx, stopBot := context.WithCancel(ctx)
	defer stopBot()
	if cfg.WebAppURL == "" && cfg.QuickTunnelMetricsURL != "" {
		go tunnel.Watch(botCtx, cfg.QuickTunnelMetricsURL, log.With("component", "tunnel"), func(publicURL string) {
			bots.SetWebAppURL(botCtx, publicURL)
		})
	}
	botDone := make(chan struct{})
	botErr := make(chan error, 1)
	go func() {
		defer close(botDone)
		if err := bots.Run(botCtx, services); err != nil {
			botErr <- err
		}
	}()

	var runErr error
	select {
	case <-ctx.Done():
		log.Info("shutdown requested")
	case err := <-httpErr:
		runErr = fmt.Errorf("http server: %w", err)
	case err := <-botErr:
		runErr = fmt.Errorf("telegram bot: %w", err)
	}

	stopBot()
	httpCtx, cancelHTTP := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancelHTTP()
	if err := srv.Shutdown(httpCtx); err != nil {
		log.Warn("http shutdown timed out; closing connections", "err", err)
		_ = srv.Close()
	}
	// The database closes (deferred) only after the bot has drained.
	botWait := time.NewTimer(botShutdownTimeout)
	defer botWait.Stop()
	select {
	case <-botDone:
	case <-botWait.C:
		log.Warn("telegram bot did not stop in time")
	}
	return runErr
}

// logStartup records the effective configuration without any secret.
func logStartup(cfg config.Config, log *slog.Logger) {
	log.Info("starting dreamer",
		"version", version,
		"http_addr", cfg.HTTPAddr,
		"data_dir", cfg.DataDir,
		"allowed_users", len(cfg.AllowedUsers),
		"webapp_url_known", cfg.WebAppURL != "",
		"quick_tunnel_discovery", cfg.WebAppURL == "" && cfg.QuickTunnelMetricsURL != "",
		"default_currency", string(cfg.DefaultCurrency),
		"initdata_max_age", cfg.InitDataMaxAge.String(),
		"max_image_bytes", cfg.MaxImageBytes,
		"log_level", cfg.LogLevel.String(),
		"llm", llmState(cfg),
	)
	if cfg.SetupMode() {
		log.Warn("setup mode: ALLOWED_USER_IDS is empty; the bot only tells /start senders their Telegram ID and the Mini App refuses everyone")
	}
	if cfg.WebAppURL == "" && cfg.QuickTunnelMetricsURL == "" {
		log.Warn("WEBAPP_URL is not set: the Mini App buttons stay hidden until a public HTTPS URL is configured")
	}
}

// llmState names the import model for the startup log, without the key.
func llmState(cfg config.Config) string {
	if cfg.LLM.APIKey == "" {
		return "off"
	}
	state := cfg.LLM.Provider
	if cfg.LLM.Model != "" {
		state += " " + cfg.LLM.Model
	}
	if cfg.LLM.Provider == recipeimport.ProviderGemini {
		state += " (captions and videos)"
	}
	return state
}

// sleep waits for d and reports false if ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// tokenSecret returns the part of a bot token after the colon, or "" when the
// token has no such part (then only the whole token is redacted).
func tokenSecret(token string) string {
	if _, secret, ok := strings.Cut(token, ":"); ok {
		return secret
	}
	return ""
}
