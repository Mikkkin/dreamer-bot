package config

import (
	"strings"
	"testing"
	"time"
)

const testToken = "123456789:AAEXAMPLEexampleEXAMPLEexample_-12345"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"BOT_TOKEN": testToken, "ALLOWED_USER_IDS": " 111, 222 ,111"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedUsers) != 2 || cfg.AllowedUsers[0] != 111 || cfg.AllowedUsers[1] != 222 {
		t.Errorf("unexpected users %v", cfg.AllowedUsers)
	}
	if cfg.SetupMode() || cfg.DefaultCurrency != "EUR" || cfg.InitDataMaxAge != 24*time.Hour || cfg.MaxImageBytes != 10<<20 || !cfg.VkusvillEnabled {
		t.Errorf("unexpected defaults %+v", cfg)
	}
}

func TestLoadSetupMode(t *testing.T) {
	cfg, err := Load(env(map[string]string{"BOT_TOKEN": testToken}))
	if err != nil || !cfg.SetupMode() {
		t.Fatalf("empty whitelist must mean setup mode: %v", err)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	cases := map[string]map[string]string{
		"missing token":   {},
		"malformed token": {"BOT_TOKEN": "not-a-token"},
		"bad user id":     {"BOT_TOKEN": testToken, "ALLOWED_USER_IDS": "111,abc"},
		"negative id":     {"BOT_TOKEN": testToken, "ALLOWED_USER_IDS": "-5"},
		"http webapp":     {"BOT_TOKEN": testToken, "WEBAPP_URL": "http://example.com"},
		"webapp query":    {"BOT_TOKEN": testToken, "WEBAPP_URL": "https://example.com/?x=1"},
		"currency":        {"BOT_TOKEN": testToken, "DEFAULT_CURRENCY": "XXX"},
		"max age":         {"BOT_TOKEN": testToken, "INITDATA_MAX_AGE": "10s"},
		"image size":      {"BOT_TOKEN": testToken, "MAX_IMAGE_MB": "500"},
		"log level":       {"BOT_TOKEN": testToken, "LOG_LEVEL": "loud"},
		"vkusvill flag":   {"BOT_TOKEN": testToken, "VKUSVILL_ENABLED": "maybe"},
	}
	for name, m := range cases {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestErrorsNeverContainToken(t *testing.T) {
	_, err := Load(env(map[string]string{"BOT_TOKEN": testToken, "ALLOWED_USER_IDS": "x", "WEBAPP_URL": "ftp://x"}))
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("error must exist and must not leak the token: %v", err)
	}
}

func TestWebAppURLNormalized(t *testing.T) {
	cfg, err := Load(env(map[string]string{"BOT_TOKEN": testToken, "WEBAPP_URL": "https://dreams.example.com"}))
	if err != nil || cfg.WebAppURL != "https://dreams.example.com/" {
		t.Fatalf("got %q, %v", cfg.WebAppURL, err)
	}
}

func TestVkusvillFlag(t *testing.T) {
	cases := map[string]bool{"": true, " ": true, "true": true, "1": true, "TRUE": true, "false": false, "0": false, " false ": false}
	for raw, want := range cases {
		cfg, err := Load(env(map[string]string{"BOT_TOKEN": testToken, "VKUSVILL_ENABLED": raw}))
		if err != nil || cfg.VkusvillEnabled != want {
			t.Errorf("VKUSVILL_ENABLED=%q: enabled = %v, %v; want %v", raw, cfg.VkusvillEnabled, err, want)
		}
	}
}
