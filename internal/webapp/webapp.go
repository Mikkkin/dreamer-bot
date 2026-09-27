// Package webapp embeds the compiled Mini App (web/dist, copied into
// internal/webapp/dist at build time) so the service ships as one binary.
package webapp

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the compiled Mini App rooted at its index.html.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Unreachable: "dist" is embedded at compile time.
		panic(err)
	}
	return sub
}
