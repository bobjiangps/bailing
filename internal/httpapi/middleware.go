package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"bailing/internal/auth"
	"bailing/internal/traffic"
)

// ---- recover + access log ---------------------------------------------------

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) logAndRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "path", r.URL.Path, "err", rec, "stack", string(debug.Stack()))
				http.Error(sw, "internal error", http.StatusInternalServerError)
			}
			if s.log.Enabled(r.Context(), slog.LevelDebug) || sw.status >= 500 {
				s.log.Log(r.Context(), levelFor(sw.status), "http", "method", r.Method, "path", r.URL.Path,
					"status", sw.status, "took", time.Since(start).Round(time.Millisecond))
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

func levelFor(status int) slog.Level {
	if status >= 500 {
		return slog.LevelWarn
	}
	return slog.LevelDebug
}

// ---- security headers -------------------------------------------------------

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		// media-src / connect-src allow remote http(s) so direct streams and
		// hls.js can reach the origin (on an https site the browser still
		// blocks http: media as mixed content — the player relays those).
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob: https:; "+
			"media-src 'self' blob: https: http:; connect-src 'self' https: http:; style-src 'self' 'unsafe-inline'; "+
			"script-src 'self' 'unsafe-inline'; font-src 'self' data:; frame-ancestors 'self'")
		next.ServeHTTP(w, r)
	})
}

// ---- client ip ----------------------------------------------------------------

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !s.trusted[host] {
		return host
	}
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	return host
}

// ---- authentication -----------------------------------------------------------

// attachUser puts the current user (if any) into the request context.
func (s *Server) attachUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u *auth.User
		if s.app.Auth.NoAuth() {
			u = s.localUser
		} else {
			u = s.app.Auth.UserByToken(auth.TokenFromRequest(r))
		}
		if u != nil {
			r = r.WithContext(auth.WithUser(r.Context(), u))
		}
		next.ServeHTTP(w, r)
	})
}

// requireUser rejects anonymous requests with 401.
func requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if auth.FromContext(r.Context()) == nil {
			fail(w, http.StatusUnauthorized, "unauthorized", "login required")
			return
		}
		h(w, r)
	}
}

// requireAdmin rejects non-admin users with 403.
func requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !auth.FromContext(r.Context()).IsAdmin {
			fail(w, http.StatusForbidden, "forbidden", "admin only")
			return
		}
		h(w, r)
	})
}

// ---- CSRF: Origin check on state-changing requests ---------------------------

func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser clients (curl, desktop shell) send no Origin. Browsers
			// always send it on cross-site POSTs, so this stays safe.
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				fail(w, http.StatusForbidden, "bad_origin", "cross-site request blocked")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if s.originAllowed(r, origin) {
			next.ServeHTTP(w, r)
			return
		}
		fail(w, http.StatusForbidden, "bad_origin", "cross-site request blocked")
	})
}

func (s *Server) originAllowed(r *http.Request, origin string) bool {
	if s.origins[origin] {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// same host as the request (covers base_url left empty in dev / desktop)
	host := r.Host
	if xf := r.Header.Get("X-Forwarded-Host"); xf != "" && s.trusted[remoteHost(r)] {
		host = xf
	}
	return strings.EqualFold(u.Host, host)
}

func remoteHost(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// ---- traffic metering ----------------------------------------------------------

type meterKey struct{}

// countingWriter counts every byte of the response body.
type countingWriter struct {
	http.ResponseWriter
	m      *traffic.Meter
	cat    string
	stream *traffic.Stream
	page   *traffic.Page
}

func (w *countingWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.m.Add(w.cat, w.stream, w.page, int64(n))
	return n, err
}

func (w *countingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *countingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func categoryOf(path string) string {
	switch {
	case strings.HasPrefix(path, "/media/photo/"):
		return traffic.Photo
	case strings.HasPrefix(path, "/media/cover/"), path == "/media/rcover":
		return traffic.Cover
	case strings.HasPrefix(path, "/media/track/"):
		return traffic.Track
	case strings.HasPrefix(path, "/proxy/"):
		return traffic.Proxy
	}
	return traffic.App
}

// pageCookie carries the id of the currently opened browser page. The front
// end writes a fresh random value on every page load (see app.html), so the
// per-page total starts from zero after a reload / reopen.
const pageCookie = "bl_pg"

// pageID returns the validated page id of the request, or "".
func pageID(r *http.Request) string {
	c, err := r.Cookie(pageCookie)
	if err != nil || !reSID.MatchString(c.Value) {
		return ""
	}
	return c.Value
}

func (s *Server) meter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cat := categoryOf(r.URL.Path)
		var pg *traffic.Page
		if id := pageID(r); id != "" {
			pg = s.app.Traffic.OpenPage(id)
		}
		s.app.Traffic.Request(cat, pg)
		cw := &countingWriter{ResponseWriter: w, m: s.app.Traffic, cat: cat, page: pg}
		next.ServeHTTP(cw, r.WithContext(context.WithValue(r.Context(), meterKey{}, cw)))
	})
}

// attachStream links the current response to a live stream counter.
func attachStream(r *http.Request, st *traffic.Stream) {
	if cw, ok := r.Context().Value(meterKey{}).(*countingWriter); ok {
		cw.stream = st
	}
}
