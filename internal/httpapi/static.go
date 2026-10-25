package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"bailing/internal/webui"
)

const notBuiltPage = `<!doctype html><meta charset="utf-8"><title>bailing</title>
<body style="font-family:sans-serif;background:#efebe6;color:#4a4744;padding:3rem">
<h2>百灵 · bailing API is running</h2>
<p>The front end has not been built into this binary yet.</p>
<p>Development: run <code>npm run dev</code> in <code>web/</code> and open <a href="http://localhost:5173">http://localhost:5173</a>.</p>
<p>Release: run <code>make web</code> then rebuild the Go binary.</p></body>`

// spa serves the embedded SvelteKit build. Unknown paths get index.html so
// the client-side router can handle them.
func (s *Server) spa() http.Handler {
	if !webui.Built() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(notBuiltPage))
		})
	}
	fsys := webui.FS()
	index, _ := fs.ReadFile(fsys, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(fsys, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "_app/immutable/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else if name == "service-worker.js" || name == "manifest.webmanifest" {
					// PWA: must be revalidated so updates reach installed apps
					w.Header().Set("Cache-Control", "no-cache")
					if name == "manifest.webmanifest" {
						w.Header().Set("Content-Type", "application/manifest+json")
					}
				} else {
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
				http.ServeFileFS(w, r, fsys, name)
				return
			}
			// a missing asset should 404 instead of returning HTML
			if path.Ext(name) != "" && !strings.HasSuffix(name, ".html") {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
