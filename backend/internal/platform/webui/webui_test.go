package webui

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":        {Data: []byte("<html>app</html>")},
		"assets/app-123.js": {Data: []byte("console.log(1)")},
		"assets/app.css":    {Data: []byte("body{}")},
		"robots.txt":        {Data: []byte("User-agent: *")},
		".gitkeep":          {Data: []byte("")},
		"assets/.gitkeep":   {Data: []byte("")},
	}
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func assertProblem(t *testing.T, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status=%d ct=%q body=%s", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	var p struct {
		Type   string
		Title  string
		Status int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.Type != "about:blank" || p.Status != code || p.Title == "" {
		t.Fatalf("problem=%+v err=%v", p, err)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
}

func TestIndexFallback(t *testing.T) {
	h := New(testFS())
	for _, p := range []string{"/", "/a/b", "/home", "/index.html", "/assets"} {
		w := do(h, http.MethodGet, p)
		if w.Code != 200 || w.Body.String() != "<html>app</html>" && p != "/index.html" {
			t.Fatalf("%s status=%d body=%q", p, w.Code, w.Body)
		}
		if p == "/index.html" && w.Body.String() != "<html>app</html>" {
			t.Fatalf("index body=%q", w.Body)
		}
		hd := w.Header()
		if !strings.HasPrefix(hd.Get("Content-Type"), "text/html") || hd.Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s headers=%v", p, hd)
		}
		if !strings.Contains(hd.Get("Content-Security-Policy"), "default-src 'self'") || !strings.Contains(hd.Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
			hd.Get("X-Frame-Options") != "DENY" || hd.Get("X-Content-Type-Options") != "nosniff" || hd.Get("Referrer-Policy") != "same-origin" {
			t.Fatalf("%s security headers=%v", p, hd)
		}
	}
}

func TestAssetsAndStatic(t *testing.T) {
	h := New(testFS())
	w := do(h, http.MethodGet, "/assets/app-123.js")
	if w.Code != 200 || w.Body.String() != "console.log(1)" || !strings.Contains(w.Header().Get("Content-Type"), "javascript") ||
		w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	if w.Header().Get("Content-Security-Policy") != "" {
		t.Fatal("CSP only on HTML")
	}
	w = do(h, http.MethodGet, "/assets/app.css")
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("css ct=%q", w.Header().Get("Content-Type"))
	}
	w = do(h, http.MethodGet, "/robots.txt")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("robots status=%d cc=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	w = do(h, http.MethodHead, "/assets/app-123.js")
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("head status=%d body=%q", w.Code, w.Body)
	}
}

func TestMissingFilesAre404Problem(t *testing.T) {
	h := New(testFS())
	for _, p := range []string{"/assets/missing.js", "/assets/nested/missing", "/favicon.ico", "/x/y.map", "/.gitkeep", "/assets/.gitkeep"} {
		assertProblem(t, do(h, http.MethodGet, p), http.StatusNotFound)
	}
}

func TestTraversalNeverEscapes(t *testing.T) {
	// secret.txt sits next to (outside) the served dist subtree, as go.mod would in a real checkout.
	parent := fstest.MapFS{
		"secret.txt":       {Data: []byte("TOP-SECRET")},
		"dist/index.html":  {Data: []byte("<index>")},
		"dist/assets/a.js": {Data: []byte("a()")},
	}
	root, err := fs.Sub(parent, "dist")
	if err != nil {
		t.Fatal(err)
	}
	h := New(root)
	// Sanity: the handler does serve content from inside the subtree.
	if w := do(h, http.MethodGet, "/assets/a.js"); w.Code != 200 || w.Body.String() != "a()" {
		t.Fatalf("sanity status=%d body=%q", w.Code, w.Body)
	}
	vectors := []string{
		"/../secret.txt", "/%2e%2e/secret.txt", "/%2E%2E/secret.txt", "/assets/../../secret.txt",
		"/..%2fsecret.txt", "/..%2Fsecret.txt", "/assets/..%2f..%2fsecret.txt", "/a/%2e%2e/%2e%2e/secret.txt",
		"/..\\secret.txt", "/assets/..\\..\\secret.txt", "/%5c..%5csecret.txt", "/dist/../secret.txt",
		"/./../secret.txt", "//secret.txt", "/secret.txt",
	}
	for _, p := range vectors {
		w := do(h, http.MethodGet, p)
		if strings.Contains(w.Body.String(), "TOP-SECRET") {
			t.Fatalf("%s leaked secret: %s", p, w.Body)
		}
		switch {
		case w.Code == http.StatusNotFound:
			assertProblem(t, w, http.StatusNotFound)
		case w.Code == http.StatusOK && w.Body.String() == "<index>":
		default:
			t.Fatalf("%s status=%d body=%q", p, w.Code, w.Body)
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := New(testFS())
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		w := do(h, m, "/")
		assertProblem(t, w, http.StatusMethodNotAllowed)
		if w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("allow=%q", w.Header().Get("Allow"))
		}
	}
}

func TestNoIndexIs404(t *testing.T) {
	h := New(fstest.MapFS{".gitkeep": {}})
	for _, p := range []string{"/", "/home", "/a/b"} {
		assertProblem(t, do(h, http.MethodGet, p), http.StatusNotFound)
	}
}

func TestEmbeddedHandlerWithoutBuild(t *testing.T) {
	// The committed dist contains only .gitkeep; real builds add index.html.
	w := do(Handler(), http.MethodGet, "/.gitkeep")
	assertProblem(t, w, http.StatusNotFound)
}
