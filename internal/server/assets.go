// Package server hosts the embedded browser UI and the local HTTP/WebSocket
// API of the SftpBox engine.
package server

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:webassets
var embedded embed.FS

// webAssets returns the rooted filesystem for the built UI.
func webAssets() (fs.FS, error) {
	return fs.Sub(embedded, "webassets")
}

// cspForApp is the restrictive Content-Security-Policy applied to the app
// shell. Everything (scripts, styles, workers) is served from the loopback
// origin itself; no remote origins are allowed.
func cspForApp() string {
	return strings.Join([]string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'", // Vite injects small inline style blocks
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"media-src 'self' blob:",
		"object-src 'none'",
		"frame-src 'self' blob:",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

// spaHandler serves the built single-page application.
//
// The browser opens client-side routes directly - the launcher hands it a
// URL such as /bootstrap/<code>. None of those map to a real file in the
// embedded bundle, so they must return index.html (the SPA shell) rather
// than a 404 or a redirect. Genuinely-missing hashed assets under /assets/
// stay 404 so the browser never receives HTML with a JS content type.
type spaHandler struct {
	assets fs.FS
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" || strings.HasSuffix(p, "/") {
		p = "index.html"
	}
	// Reject any traversal; asset paths are flat within webassets.
	if strings.Contains(p, "..") {
		http.NotFound(w, r)
		return
	}
	_, statErr := fs.Stat(h.assets, p)
	exists := statErr == nil
	// A missing hashed asset must remain a real 404 rather than the shell.
	if !exists && strings.HasPrefix(p, "assets/") {
		http.NotFound(w, r)
		return
	}
	if !exists {
		// Client-side route: serve the SPA shell.
		p = "index.html"
	}
	f, err := h.assets.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(p, ".html") {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Security-Policy", cspForApp())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// ServeContent streams the embedded file without the directory/canonical
	// redirect logic of http.FileServer - so /bootstrap/<code> returns the
	// shell with HTTP 200, not a 301.
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		// embed.fsFile implements ReadSeeker; this fallback is defensive.
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, p, info.ModTime(), rs)
}
