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
	if cfg.SetupMode() || cfg.DefaultCurrency != "EUR" || cfg.InitDataMaxAge != 24*time.Hour || cfg.MaxImageBytes != 10<<20 {
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

const testLLMKey = "AIzaSyExampleExampleExampleExample1234"

func TestLLMSettings(t *testing.T) {
	cfg, err := Load(env(map[string]string{"BOT_TOKEN": testToken}))
	if err != nil || cfg.LLM.APIKey != "" || cfg.LLM.Provider != "gemini" {
		t.Fatalf("default LLM = %+v, %v; want off, gemini", cfg.LLM, err)
	}
	// Settings without a key keep it off and are not checked further.
	if _, err := Load(env(map[string]string{"BOT_TOKEN": testToken, "LLM_PROVIDER": "openai"})); err != nil {
		t.Errorf("openai without a key: %v", err)
	}
	good := map[string]map[string]string{
		"gemini":         {"LLM_PROVIDER": " Gemini ", "LLM_API_KEY": " " + testLLMKey + " "},
		"gemini model":   {"LLM_PROVIDER": "gemini", "LLM_API_KEY": testLLMKey, "LLM_MODEL": "gemini-2.5-flash-lite"},
		"anthropic":      {"LLM_API_KEY": testLLMKey},
		"openai":         {"LLM_PROVIDER": "openai", "LLM_API_KEY": testLLMKey, "LLM_MODEL": "gpt-5-mini"},
		"openai compat":  {"LLM_PROVIDER": "openai", "LLM_API_KEY": testLLMKey, "LLM_MODEL": "llama", "LLM_BASE_URL": "https://llm.example.com/v1"},
		"openai locally": {"LLM_PROVIDER": "openai", "LLM_API_KEY": testLLMKey, "LLM_MODEL": "llama", "LLM_BASE_URL": "http://127.0.0.1:11434/v1"},
	}
	for name, m := range good {
		m["BOT_TOKEN"] = testToken
		cfg, err := Load(env(m))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if cfg.LLM.APIKey != testLLMKey {
			t.Errorf("%s: key = %q", name, cfg.LLM.APIKey)
		}
	}
	if cfg, _ := Load(env(map[string]string{"BOT_TOKEN": testToken, "LLM_PROVIDER": "GEMINI", "LLM_API_KEY": testLLMKey})); cfg.LLM.Provider != "gemini" {
		t.Errorf("provider = %q, want it lower-cased", cfg.LLM.Provider)
	}

	bad := map[string]map[string]string{
		"provider":         {"LLM_PROVIDER": "grok", "LLM_API_KEY": testLLMKey},
		"provider no key":  {"LLM_PROVIDER": "grok"},
		"short key":        {"LLM_API_KEY": "sk-123"},
		"key with space":   {"LLM_API_KEY": "AIzaSyExample Example1234567890"},
		"openai model":     {"LLM_PROVIDER": "openai", "LLM_API_KEY": testLLMKey},
		"anthropic base":   {"LLM_API_KEY": testLLMKey, "LLM_BASE_URL": "https://example.com/v1"},
		"gemini base":      {"LLM_PROVIDER": "gemini", "LLM_API_KEY": testLLMKey, "LLM_BASE_URL": "https://example.com/v1"},
		"plain http":       {"LLM_PROVIDER": "openai", "LLM_API_KEY": testLLMKey, "LLM_MODEL": "m", "LLM_BASE_URL": "http://llm.example.com/v1"},
		"gemini bad model": {"LLM_PROVIDER": "gemini", "LLM_API_KEY": testLLMKey, "LLM_MODEL": "../files?key=x"},
	}
	for name, m := range bad {
		m["BOT_TOKEN"] = testToken
		_, err := Load(env(m))
		if err == nil {
			t.Errorf("%s: expected error", name)
			continue
		}
		if strings.Contains(err.Error(), testLLMKey) || strings.Contains(err.Error(), "sk-123") {
			t.Errorf("%s: the error leaks the key: %v", name, err)
		}
	}
}

// The ВкусВилл cart is gone; a .env written for an older version may still
// set VKUSVILL_ENABLED, and that must not stop the bot from starting.
func TestRemovedSettingsAreIgnored(t *testing.T) {
	for _, raw := range []string{"true", "false", "maybe"} {
		if _, err := Load(env(map[string]string{"BOT_TOKEN": testToken, "VKUSVILL_ENABLED": raw})); err != nil {
			t.Errorf("VKUSVILL_ENABLED=%q: %v", raw, err)
		}
	}
}
