// Command dreamer runs the shared wishlist: the Telegram bot, the Mini App
// API and the embedded Mini App, all in one process.
//
// Usage:
//
//	dreamer [serve]      run the service (default)
//	dreamer healthcheck  probe the local /healthz; exit 0 when healthy
//	dreamer version      print the build version
package main

import (
	"fmt"
	"io"
	"os"

	// The distroless runtime image has no zoneinfo database.
	_ "time/tzdata"
)

// version is stamped at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	if len(args) > 1 {
		cmd = "" // no subcommand takes arguments
	}
	switch cmd {
	case "serve":
		return serve(getenv, stderr)
	case "healthcheck":
		return healthcheck(getenv("HTTP_ADDR"), stderr)
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	default:
		fmt.Fprintln(stderr, "usage: dreamer [serve|healthcheck|version]")
		return 2
	}
}
