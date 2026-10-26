// Package httpapi exposes the JSON API, media endpoints and the embedded
// front end over net/http (Go 1.22+ method/wildcard routing, no framework).
package httpapi

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"bailing/internal/app"
	"bailing/internal/auth"
)

// Server is the HTTP layer.
type Server struct {
	app       *app.App
	log       *slog.Logger
	trusted   map[string]bool
	origins   map[string]bool
	localUser *auth.User // auth.mode == none
	basePath  string

	relaySem chan struct{} // limits concurrently relayed audio streams

	quotaMu   sync.Mutex
	quotaAt   time.Time
	quotaOver bool

	corsMu    sync.Mutex
	corsCache map[string]corsEntry
}

type corsEntry struct {
	at time.Time
	ok bool
}

// New builds the root handler.
func New(a *app.App) (http.Handler, error) {
	s := &Server{app: a, log: a.Log.With("mod", "http"), trusted: map[string]bool{},
		origins: map[string]bool{}, basePath: a.Cfg.Server.BasePath,
		relaySem:  make(chan struct{}, a.Cfg.Sources.Proxy.MaxConcurrent),
		corsCache: map[string]corsEntry{}}
	for _, p := range a.Cfg.Server.TrustedProxies {
		s.trusted[p] = true
	}
	if u, err := url.Parse(a.Cfg.Server.BaseURL); err == nil && u.Host != "" {
		s.origins[u.Scheme+"://"+u.Host] = true
	}
	for _, o := range a.Cfg.Server.AllowedOrigins {
		s.origins[strings.TrimRight(o, "/")] = true
	}
	if a.Auth.NoAuth() {
		u, err := a.Auth.LocalUser()
		if err != nil {
			return nil, err
		}
		s.localUser = u
	}

	mux := http.NewServeMux()
	s.routes(mux)

	var h http.Handler = mux
	h = s.checkOrigin(h)
	h = s.attachUser(h)
	h = s.meter(h)
	h = securityHeaders(h)
	h = s.logAndRecover(h)

	if s.basePath != "" {
		inner := http.StripPrefix(s.basePath, h)
		root := http.NewServeMux()
		root.Handle(s.basePath+"/", inner)
		root.HandleFunc("GET "+s.basePath, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, s.basePath+"/", http.StatusMovedPermanently)
		})
		return root, nil
	}
	return h, nil
}

func (s *Server) routes(mux *http.ServeMux) {
	// public
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("GET /api/home", s.home)

	// signed-in
	mux.HandleFunc("GET /api/me/prefs", requireUser(s.getPrefs))
	mux.HandleFunc("PUT /api/me/prefs", requireUser(s.putPrefs))
	mux.HandleFunc("POST /api/me/password", requireUser(s.changePassword))

	mux.HandleFunc("GET /api/stats/summary", requireUser(s.statsSummary))
	mux.HandleFunc("POST /api/stats/play", requireUser(s.statsPlay))
	mux.HandleFunc("POST /api/stats/view", requireUser(s.statsView))

	feat := s.app.Cfg.Features
	if feat.Photos {
		s.photoRoutes(mux)
	}
	if feat.Music {
		s.musicRoutes(mux)
	}

	mux.HandleFunc("GET /api/traffic/summary", requireUser(s.trafficSummary))
	mux.HandleFunc("GET /api/traffic/streams/{sid}", requireUser(s.trafficStream))
	mux.HandleFunc("GET /api/traffic/page", requireUser(s.trafficPage))

	// admin
	mux.HandleFunc("POST /api/admin/rescan", requireAdmin(s.rescan))
	mux.HandleFunc("GET /api/admin/status", requireAdmin(s.status))
	mux.HandleFunc("GET /api/admin/storage", requireAdmin(s.storage))
	mux.HandleFunc("POST /api/admin/cache/clean", requireAdmin(s.cacheClean))

	// unknown API paths must not fall through to the SPA
	// (patterns carry "GET" so they do not conflict with "GET /")
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	mux.HandleFunc("GET /media/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	// front end (SPA)
	mux.Handle("GET /", s.spa())
}

func (s *Server) photoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/albums", requireUser(s.albums))
	mux.HandleFunc("GET /api/albums/{id}", requireUser(s.album))
	mux.HandleFunc("GET /api/photos/{id}/exif", requireUser(s.photoExif))
	mux.HandleFunc("GET /media/photo/{id}/{size}", requireUser(s.photoMedia))
}

func (s *Server) musicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/music/tracks", requireUser(s.tracks))
	mux.HandleFunc("GET /api/music/groups/{kind}", requireUser(s.groups))
	mux.HandleFunc("GET /api/music/tracks/{id}/lyrics", requireUser(s.lyrics))
	mux.HandleFunc("DELETE /api/music/tracks/{id}/lyrics", requireUser(s.lyricsRefetch))
	mux.HandleFunc("POST /api/music/tracks/{id}/duration", requireUser(s.trackDuration))
	mux.HandleFunc("GET /media/track/{id}", requireUser(s.trackMedia))
	mux.HandleFunc("GET /media/cover/{key}", requireUser(s.coverMedia))
	mux.HandleFunc("GET /media/rcover", requireUser(s.remoteCover))

	mux.HandleFunc("GET /api/sources", requireUser(s.sourcesList))
	mux.HandleFunc("GET /api/sources/{id}/search", requireUser(s.sourceSearch))
	mux.HandleFunc("POST /api/play/resolve", requireUser(s.playResolve))
	mux.HandleFunc("GET /api/radio/stations", requireUser(s.radioStations))
	mux.HandleFunc("GET /api/radio/countries", requireUser(s.radioCountries))
	mux.HandleFunc("GET /api/lyrics/lookup", requireUser(s.lyricsLookup))
	mux.HandleFunc("GET /proxy/stream", requireUser(s.proxyStream))
	mux.HandleFunc("GET /api/lx/scripts", requireUser(s.lxList))
	mux.HandleFunc("POST /api/lx/scripts", requireAdmin(s.lxUpload))
	mux.HandleFunc("PUT /api/lx/scripts/{id}", requireAdmin(s.lxToggle))
	mux.HandleFunc("DELETE /api/lx/scripts/{id}", requireAdmin(s.lxDelete))
	mux.HandleFunc("POST /api/lx/reload", requireAdmin(s.lxReload))

	// favourites and playlists (cross-device, per user)
	mux.HandleFunc("GET /api/favorites", requireUser(s.favList))
	mux.HandleFunc("POST /api/favorites", requireUser(s.favAdd))
	mux.HandleFunc("POST /api/favorites/remove", requireUser(s.favRemove))
	mux.HandleFunc("GET /api/playlists", requireUser(s.plList))
	mux.HandleFunc("POST /api/playlists", requireUser(s.plCreate))
	mux.HandleFunc("GET /api/playlists/{id}", requireUser(s.plGet))
	mux.HandleFunc("PUT /api/playlists/{id}", requireUser(s.plUpdate))
	mux.HandleFunc("POST /api/playlists/{id}/items", requireUser(s.plAppend))
	mux.HandleFunc("DELETE /api/playlists/{id}", requireUser(s.plDelete))
}
