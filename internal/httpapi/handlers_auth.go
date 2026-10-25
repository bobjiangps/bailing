package httpapi

import (
	"errors"
	"net/http"
	"time"

	"bailing/internal/auth"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.app.Version})
}

type loginReq struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

func (s *Server) cookiePath() string {
	if s.basePath != "" {
		return s.basePath + "/"
	}
	return "/"
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.app.Auth.NoAuth() {
		writeJSON(w, http.StatusOK, map[string]any{"user": s.localUser})
		return
	}
	var req loginReq
	if err := readJSON(r, 4096, &req); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	wantToken := r.URL.Query().Get("token") == "1"
	kind := "cookie"
	if wantToken {
		kind = "token"
	}
	token, u, err := s.app.Auth.Login(s.clientIP(r), req.Name, req.Password, r.UserAgent(), kind)
	switch {
	case errors.Is(err, auth.ErrLocked):
		fail(w, http.StatusTooManyRequests, "locked", "too many attempts, try again later")
		return
	case errors.Is(err, auth.ErrBadCredentials):
		fail(w, http.StatusUnauthorized, "bad_credentials", "wrong name or password")
		return
	case err != nil:
		s.log.Error("login", "err", err)
		fail(w, http.StatusInternalServerError, "error", "login failed")
		return
	}
	if wantToken {
		writeJSON(w, http.StatusOK, map[string]any{"user": u, "token": token})
		return
	}
	c := &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     s.cookiePath(),
		HttpOnly: true,
		Secure:   s.app.Cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	}
	if req.Remember {
		c.MaxAge = int(s.app.Auth.SessionTTL() / time.Second)
	}
	http.SetCookie(w, c)
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if t := auth.TokenFromRequest(r); t != "" {
		s.app.Auth.Logout(t)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: s.cookiePath(),
		MaxAge: -1, HttpOnly: true, Secure: s.app.Cfg.SecureCookies(), SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"user":     auth.FromContext(r.Context()), // null for guests
		"no_auth":  s.app.Auth.NoAuth(),
		"features": s.features(),
	})
}

func (s *Server) features() map[string]bool {
	f := s.app.Cfg.Features
	return map[string]bool{"photos": f.Photos, "music": f.Music}
}

// home is public. Guests only get decoration data; members get counts.
func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	out := map[string]any{"user": u, "features": s.features()}
	if u != nil {
		albums, photos := s.app.Photos.Count()
		out["counts"] = map[string]int{"albums": albums, "photos": photos, "tracks": s.app.Music.Count()}
		if sum, err := s.app.Traffic.Summary(0); err == nil {
			out["traffic"] = map[string]any{"warn": sum.Warn, "over": sum.Over, "month": sum.Month,
				"quota_bytes": sum.QuotaBytes}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- prefs ------------------------------------------------------------------

func (s *Server) getPrefs(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	var raw string
	s.app.DB.QueryRow(`SELECT json FROM prefs WHERE user_id=?`, u.ID).Scan(&raw)
	if raw == "" {
		raw = "{}"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(raw))
}

func (s *Server) putPrefs(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	var v map[string]any
	if err := readJSON(r, 8192, &v); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	b, _ := jsonMarshal(v)
	if _, err := s.app.DB.Exec(`INSERT INTO prefs(user_id, json) VALUES(?,?)
		ON CONFLICT(user_id) DO UPDATE SET json=excluded.json`, u.ID, string(b)); err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
