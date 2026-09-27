package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const healthcheckTimeout = 3 * time.Second

// healthcheck backs the Docker HEALTHCHECK: the distroless image has no shell
// or curl, so the binary probes itself.
func healthcheck(httpAddr string, stderr io.Writer) int {
	target, err := healthURL(httpAddr)
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck:", err)
		return 1
	}
	client := &http.Client{
		Timeout:       healthcheckTimeout,
		Transport:     &http.Transport{Proxy: nil}, // never route the local probe through a proxy
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

// healthURL points at the local listener described by HTTP_ADDR (default
// ":8080"). A wildcard or empty host means the loopback address.
func healthURL(httpAddr string) (string, error) {
	addr := strings.TrimSpace(httpAddr)
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", fmt.Errorf("HTTP_ADDR %q is not host:port", addr)
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}
