package main

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func TestHealthURL(t *testing.T) {
	cases := map[string]string{
		"":               "http://127.0.0.1:8080/healthz",
		":8080":          "http://127.0.0.1:8080/healthz",
		" :9000 ":        "http://127.0.0.1:9000/healthz",
		"0.0.0.0:8081":   "http://127.0.0.1:8081/healthz",
		"[::]:8082":      "http://127.0.0.1:8082/healthz",
		"127.0.0.1:8083": "http://127.0.0.1:8083/healthz",
		"10.0.0.5:8084":  "http://10.0.0.5:8084/healthz",
		"[::1]:8085":     "http://[::1]:8085/healthz",
	}
	for in, want := range cases {
		got, err := healthURL(in)
		if err != nil || got != want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"8080", "localhost", "host:"} {
		if _, err := healthURL(bad); err == nil {
			t.Errorf("healthURL(%q): expected error", bad)
		}
	}
}

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))

	var stderr bytes.Buffer
	if code := healthcheck(":"+port, &stderr); code != 0 {
		t.Fatalf("healthy server: exit %d (%s)", code, stderr.String())
	}
	status = http.StatusServiceUnavailable
	if code := healthcheck(":"+port, &stderr); code != 1 {
		t.Fatalf("unhealthy server: exit %d", code)
	}
	srv.Close()
	if code := healthcheck(":"+port, &stderr); code != 1 {
		t.Fatalf("no server: exit %d", code)
	}
}

func TestRunSubcommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, noEnv, &stdout, &stderr); code != 0 || strings.TrimSpace(stdout.String()) != version {
		t.Fatalf("version: exit %d, output %q", code, stdout.String())
	}
	for _, args := range [][]string{{"bogus"}, {"version", "extra"}} {
		if code := run(args, noEnv, &stdout, &stderr); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestServeRejectsInvalidConfigWithoutLeakingSecrets(t *testing.T) {
	const token = "123456789:AAEXAMPLEexampleEXAMPLEexample_-12345"
	env := map[string]string{"BOT_TOKEN": token, "ALLOWED_USER_IDS": "abc"}
	var stdout, stderr bytes.Buffer
	if code := run(nil, func(k string) string { return env[k] }, &stdout, &stderr); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if strings.Contains(stderr.String(), token) || !strings.Contains(stderr.String(), "ALLOWED_USER_IDS") {
		t.Fatalf("unexpected output %s", stderr.String())
	}
}
