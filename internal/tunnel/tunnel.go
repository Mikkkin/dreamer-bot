// Package tunnel discovers the public hostname of a cloudflared quick tunnel.
// A quick tunnel gets a new random *.trycloudflare.com hostname on every
// start, and cloudflared reports it on its metrics server at /quicktunnel.
package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"syscall"
	"time"
)

const (
	requestTimeout = 5 * time.Second
	minBackoff     = time.Second
	maxBackoff     = 30 * time.Second
	pollInterval   = 60 * time.Second
	// maxBodyBytes bounds the metrics response; the real one is ~60 bytes.
	maxBodyBytes = 4 << 10
	// maxHostnameLen is the DNS limit for a full hostname.
	maxHostnameLen = 253
)

// hostnamePattern accepts only a single-label *.trycloudflare.com name, which
// is what cloudflared assigns to quick tunnels. Anything else (another domain,
// a scheme, port, path or credentials) is rejected: the discovered hostname
// becomes the Mini App button, so it must never point at a foreign site.
var hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.trycloudflare\.com$`)

// Watch polls {metricsURL}/quicktunnel until a hostname appears, retrying
// with exponential backoff (1s up to 30s), and afterwards every 60s to notice
// a restarted tunnel. It calls onChange("https://<host>/") whenever the
// hostname changes. onChange runs on the Watch goroutine. Watch blocks until
// ctx is done.
func Watch(ctx context.Context, metricsURL string, log *slog.Logger, onChange func(publicURL string)) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	endpoint, err := url.JoinPath(metricsURL, "quicktunnel")
	if err != nil {
		log.Error("invalid quick tunnel metrics URL", "err", err)
		return
	}
	w := &watcher{
		client: &http.Client{
			Timeout:   requestTimeout,
			Transport: privateOnlyTransport(),
			// The metrics server never redirects; refuse to follow anywhere.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		endpoint:   endpoint,
		minBackoff: minBackoff,
		maxBackoff: maxBackoff,
		poll:       pollInterval,
		log:        log,
		onChange:   onChange,
	}
	w.run(ctx)
}

type watcher struct {
	client     *http.Client
	endpoint   string
	minBackoff time.Duration
	maxBackoff time.Duration
	poll       time.Duration
	log        *slog.Logger
	onChange   func(publicURL string)
	// warned is set once the "still waiting" warning has been logged, so a bot
	// running without the quick profile does not warn every 30 seconds.
	warned bool
}

func (w *watcher) run(ctx context.Context) {
	var current string
	backoff := w.minBackoff
	for {
		host, err := w.fetch(ctx)
		if ctx.Err() != nil {
			return
		}
		var wait time.Duration
		switch {
		case err != nil || host == "":
			// cloudflared is starting (or restarting): retry quickly at first.
			w.logWaiting(ctx, err, backoff)
			wait = backoff
			backoff = min(2*backoff, w.maxBackoff)
		default:
			backoff = w.minBackoff
			wait = w.poll
			w.warned = false
			if host != current {
				current = host
				publicURL := "https://" + host + "/"
				// Warn: this repoints every Mini App button, so it must stand out.
				w.log.Warn("quick tunnel hostname discovered; Mini App URL updated", "public_url", publicURL)
				w.onChange(publicURL)
			}
		}
		if !sleep(ctx, wait) {
			return
		}
	}
}

func (w *watcher) logWaiting(ctx context.Context, err error, backoff time.Duration) {
	level := slog.LevelDebug
	if backoff >= w.maxBackoff && !w.warned {
		level = slog.LevelWarn
		w.warned = true
	}
	if err == nil {
		err = errors.New("hostname not assigned yet")
	}
	w.log.Log(ctx, level, "waiting for quick tunnel hostname", "err", err, "retry_in", backoff.String())
}

// fetch returns the current hostname, or "" when cloudflared has none yet.
func (w *watcher) fetch(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("quicktunnel: unexpected status %d", resp.StatusCode)
	}
	var body struct {
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&body); err != nil {
		return "", fmt.Errorf("quicktunnel: decode: %w", err)
	}
	if body.Hostname == "" {
		return "", nil
	}
	if len(body.Hostname) > maxHostnameLen || !hostnamePattern.MatchString(body.Hostname) {
		return "", errors.New("quicktunnel: invalid hostname")
	}
	return body.Hostname, nil
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

// privateOnlyTransport dials only loopback and private addresses. cloudflared
// runs next to the bot on the compose network, so a public address means the
// "quicktunnel" name was answered by an outside resolver — possibly a spoofed
// one — and must not be trusted to choose the Mini App URL.
func privateOnlyTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout: requestTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if !isInternal(ip) {
				return fmt.Errorf("quicktunnel: refusing non-private address %s", ip)
			}
			return nil
		},
	}
	return &http.Transport{
		Proxy:                 nil, // never route the metrics request through a proxy
		DialContext:           dialer.DialContext,
		ResponseHeaderTimeout: requestTimeout,
		MaxIdleConns:          1,
	}
}

func isInternal(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate()
}
