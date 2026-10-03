// Package config loads and validates the service configuration from the
// environment. Invalid configuration fails fast at startup with a message
// that never includes secret values.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/recipeimport"
)

// Config is the fully validated runtime configuration.
type Config struct {
	// BotToken authenticates the bot and is the root secret of the service:
	// Mini App initData and media URL signatures are derived from it.
	BotToken string
	// AllowedUsers is the whitelist. Empty means setup mode.
	AllowedUsers []domain.UserID
	// WebAppURL is the public HTTPS URL of the Mini App; empty when unknown.
	WebAppURL string
	// QuickTunnelMetricsURL points at cloudflared's metrics server; used to
	// discover a trycloudflare.com hostname when WebAppURL is empty.
	QuickTunnelMetricsURL string

	HTTPAddr        string
	DataDir         string
	DefaultCurrency domain.Currency
	InitDataMaxAge  time.Duration
	MaxImageBytes   int64
	LogLevel        slog.Level

	// LLM is the optional language model that reads the recipes the import
	// rules are unsure about (with Gemini also the video of a reel). It is
	// off when LLM.APIKey is empty. The key is a secret like BotToken.
	LLM recipeimport.LLMConfig
}

// SetupMode reports whether no user is whitelisted yet.
func (c Config) SetupMode() bool { return len(c.AllowedUsers) == 0 }

// LLMProviders are the accepted values of LLM_PROVIDER; the first is the
// default.
var LLMProviders = []string{recipeimport.ProviderGemini, recipeimport.ProviderAnthropic, recipeimport.ProviderOpenAI}

// minLLMKeyLen rejects values that cannot be an API key. The key is
// redacted from every log line, so a stray short value would garble them.
const minLLMKeyLen = 16

// botTokenPattern matches the "<bot id>:<secret>" shape issued by BotFather.
var botTokenPattern = regexp.MustCompile(`^[0-9]{5,20}:[A-Za-z0-9_-]{30,64}$`)

// Load reads the configuration using getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	cfg := Config{
		BotToken:              strings.TrimSpace(getenv("BOT_TOKEN")),
		HTTPAddr:              orDefault(getenv("HTTP_ADDR"), ":8080"),
		DataDir:               orDefault(getenv("DATA_DIR"), "./data"),
		QuickTunnelMetricsURL: strings.TrimSpace(getenv("QUICK_TUNNEL_METRICS_URL")),
	}

	switch {
	case cfg.BotToken == "":
		fail("BOT_TOKEN is required (get one from @BotFather)")
	case !botTokenPattern.MatchString(cfg.BotToken):
		fail("BOT_TOKEN has an invalid format")
	}

	users, err := parseUserIDs(getenv("ALLOWED_USER_IDS"))
	if err != nil {
		fail("ALLOWED_USER_IDS: %w", err)
	}
	cfg.AllowedUsers = users

	if raw := strings.TrimSpace(getenv("WEBAPP_URL")); raw != "" {
		u, err := parseHTTPSURL(raw)
		if err != nil {
			fail("WEBAPP_URL: %w", err)
		}
		cfg.WebAppURL = u
	}
	if cfg.QuickTunnelMetricsURL != "" {
		if u, err := url.Parse(cfg.QuickTunnelMetricsURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			fail("QUICK_TUNNEL_METRICS_URL must be an http(s) URL")
		}
	}

	cur, err := domain.ParseCurrency(orDefault(getenv("DEFAULT_CURRENCY"), "EUR"))
	if err != nil {
		fail("DEFAULT_CURRENCY must be one of %v", domain.Currencies)
	}
	cfg.DefaultCurrency = cur

	maxAge, err := time.ParseDuration(orDefault(getenv("INITDATA_MAX_AGE"), "24h"))
	switch {
	case err != nil:
		fail("INITDATA_MAX_AGE: %w", err)
	case maxAge < time.Minute || maxAge > 7*24*time.Hour:
		fail("INITDATA_MAX_AGE must be between 1m and 168h")
	}
	cfg.InitDataMaxAge = maxAge

	mb, err := strconv.Atoi(orDefault(getenv("MAX_IMAGE_MB"), "10"))
	switch {
	case err != nil:
		fail("MAX_IMAGE_MB must be an integer")
	case mb < 1 || mb > 50:
		fail("MAX_IMAGE_MB must be between 1 and 50")
	}
	cfg.MaxImageBytes = int64(mb) << 20

	if err := cfg.LogLevel.UnmarshalText([]byte(orDefault(getenv("LOG_LEVEL"), "info"))); err != nil {
		fail("LOG_LEVEL must be debug, info, warn or error")
	}

	if err := loadLLM(getenv, &cfg.LLM); err != nil {
		fail("%w", err)
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// loadLLM reads LLM_API_KEY, LLM_PROVIDER, LLM_MODEL and LLM_BASE_URL and
// checks them by building the client once, so a wrong combination fails at
// startup rather than at the first import. Messages never include the key.
func loadLLM(getenv func(string) string, llm *recipeimport.LLMConfig) error {
	*llm = recipeimport.LLMConfig{
		Provider: strings.ToLower(orDefault(getenv("LLM_PROVIDER"), LLMProviders[0])),
		APIKey:   strings.TrimSpace(getenv("LLM_API_KEY")),
		Model:    strings.TrimSpace(getenv("LLM_MODEL")),
		BaseURL:  strings.TrimSpace(getenv("LLM_BASE_URL")),
	}
	switch {
	case !slices.Contains(LLMProviders, llm.Provider):
		return fmt.Errorf("LLM_PROVIDER must be one of %v", LLMProviders)
	case llm.APIKey == "":
		return nil
	case len(llm.APIKey) < minLLMKeyLen || strings.ContainsAny(llm.APIKey, " \t\r\n"):
		return errors.New("LLM_API_KEY does not look like an API key")
	}
	if _, err := recipeimport.NewLLM(*llm); err != nil {
		return fmt.Errorf("LLM settings: %w", err)
	}
	return nil
}

func parseUserIDs(raw string) ([]domain.UserID, error) {
	var ids []domain.UserID
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("%q is not a positive numeric Telegram user ID", part)
		}
		if !slices.Contains(ids, domain.UserID(id)) {
			ids = append(ids, domain.UserID(id))
		}
	}
	return ids, nil
}

func parseHTTPSURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("must be an absolute URL")
	}
	if u.Scheme != "https" {
		return "", errors.New("must use https (Telegram requires it for Mini Apps)")
	}
	if u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return "", errors.New("must not contain credentials, a query or a #fragment")
	}
	return strings.TrimRight(u.String(), "/") + "/", nil
}

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}
