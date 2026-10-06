// Package webui serves the embedded production frontend build (single-page app).
package webui

import (
	"embed"
	"encoding/json"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// Handler serves the frontend build embedded in the binary.
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// dist is always embedded; fall back to an empty tree rather than panicking.
		return New(emptyFS{})
	}
	return New(sub)
}

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

// New returns a handler serving static files from root with SPA fallback to index.html.
func New(root fs.FS) http.Handler { return &handler{root: root} }

type handler struct{ root fs.FS }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	urlPath := r.URL.Path
	if !strings.HasPrefix(urlPath, "/") || strings.ContainsAny(urlPath, "\x00\\") {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	for _, segment := range strings.Split(urlPath, "/") {
		if segment == ".." {
			writeProblem(w, http.StatusNotFound, "Not Found")
			return
		}
	}
	clean := path.Clean(urlPath)
	name := strings.TrimPrefix(clean, "/")
	if name != "" && path.Base(name) != ".gitkeep" && fs.ValidPath(name) {
		if info, err := fs.Stat(h.root, name); err == nil && !info.IsDir() {
			h.serveFile(w, r, name, strings.HasPrefix(name, "assets/"))
			return
		}
	}
	// By design, a last segment with a file extension (including dotted names like
	// "/v1.2") is treated as a missing file (404), never as a client-side route.
	if strings.HasPrefix(clean, "/assets/") || path.Ext(path.Base(clean)) != "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	h.serveFile(w, r, "index.html", false)
}

func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, name string, immutable bool) {
	data, err := fs.ReadFile(h.root, name)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	header := w.Header()
	header.Set("Content-Type", contentType)
	if immutable {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		header.Set("Cache-Control", "no-cache")
	}
	if strings.HasPrefix(contentType, "text/html") {
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		header.Set("X-Frame-Options", "DENY")
	}
	header.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func writeProblem(w http.ResponseWriter, code int, title string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
	}{"about:blank", title, code})
}
