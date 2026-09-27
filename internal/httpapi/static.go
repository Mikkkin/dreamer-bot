package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// notBuiltPage is served at / when the binary was built without the Mini App.
const notBuiltPage = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>dreamer-bot</title>
</head>
<body style="font-family: system-ui, sans-serif; margin: 2rem; line-height: 1.5">
<h1>Мини-приложение не собрано</h1>
<p>Сервер работает, но в этот бинарник не встроена сборка Mini App.
Соберите фронтенд (<code>cd web &amp;&amp; bun run build</code>) и пересоберите сервер.</p>
</body>
</html>
`

// staticSite serves the compiled Mini App. There is no SPA fallback: the app
// routes through query parameters, so unknown paths are plain 404s.
type staticSite struct {
	files     fs.FS
	index     []byte
	indexETag string
}

func newStaticSite(files fs.FS) *staticSite {
	site := &staticSite{files: files, index: []byte(notBuiltPage)}
	if files != nil {
		if index, err := fs.ReadFile(files, "index.html"); err == nil {
			site.index = index
		}
	}
	// Embedded files have no modification time, so the index is validated
	// by content instead.
	sum := sha256.Sum256(site.index)
	site.indexETag = `"` + hex.EncodeToString(sum[:12]) + `"`
	return site
}

func (s *staticSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", s.indexETag)
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(s.index))
		return
	}
	s.serveFile(w, r, strings.TrimPrefix(r.URL.Path, "/"))
}

func (s *staticSite) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	if s.files == nil || !publicPath(name) {
		staticNotFound(w)
		return
	}
	f, err := s.files.Open(name)
	if err != nil {
		staticNotFound(w)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		staticNotFound(w)
		return
	}
	content, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			staticNotFound(w)
			return
		}
		content = bytes.NewReader(data)
	}
	if ct := contentType(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, name, info.ModTime(), content)
}

// publicPath rejects invalid paths and hidden files such as .gitkeep.
func publicPath(name string) bool {
	if !fs.ValidPath(name) || name == "." {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return false
		}
	}
	return true
}

// contentType covers web types missing from Go's built-in MIME table; the
// distroless image has no /etc/mime.types, and nosniff makes a wrong type fatal.
func contentType(ext string) string {
	switch ext {
	case ".webmanifest":
		return "application/manifest+json"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".ico":
		return "image/x-icon"
	default:
		return ""
	}
}

func staticNotFound(w http.ResponseWriter) {
	// Do not let a 404 inherit the long-lived asset caching policy.
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
}
