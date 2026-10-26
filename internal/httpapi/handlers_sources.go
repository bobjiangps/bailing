package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"bailing/internal/auth"
	"bailing/internal/netguard"
	"bailing/internal/proxy"
	"bailing/internal/sources"
	"bailing/internal/sources/lx"
	"bailing/internal/sources/radio"
	"bailing/internal/traffic"
)

// ---- search & resolve ----------------------------------------------------------------

func (s *Server) sourceSearch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	src, ok := s.app.Sources.Get(id)
	if !ok {
		fail(w, http.StatusNotFound, "not_found", "no such source")
		return
	}
	se, ok := src.(sources.Searcher)
	if !ok {
		fail(w, http.StatusBadRequest, "unsupported", "this source cannot search")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 200 {
		fail(w, http.StatusBadRequest, "bad_request", "empty query")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	items, more, err := se.Search(ctx, q, page)
	if err != nil {
		s.log.Warn("source search", "source", id, "err", err)
		fail(w, http.StatusBadGateway, "search_failed", err.Error())
		return
	}
	if items == nil {
		items = []sources.TrackRef{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": more, "page": page})
}

// playResolve turns a queued online item into something the browser can
// play, and decides how: directly from the origin (no server traffic) or
// through the signed relay (server traffic, shown prominently in the UI).
func (s *Server) playResolve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source  string           `json:"source"`
		Track   sources.TrackRef `json:"track"`
		Quality string           `json:"quality"`
		// NeedCORS: the page routes audio through Web Audio (equaliser /
		// spectrum), so it needs to know whether the origin allows CORS for
		// plain audio files too, not only for HLS.
		NeedCORS bool `json:"need_cors"`
	}
	if err := readJSON(r, 256<<10, &body); err != nil || !reSource.MatchString(body.Source) || body.Source == "local" {
		fail(w, http.StatusBadRequest, "bad_request", "invalid resolve request")
		return
	}
	src, ok := s.app.Sources.Get(body.Source)
	if !ok {
		fail(w, http.StatusNotFound, "not_found", "no such source")
		return
	}
	res, ok := src.(sources.Resolver)
	if !ok {
		fail(w, http.StatusBadRequest, "unsupported", "this source cannot resolve")
		return
	}
	body.Track.Source = body.Source
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	p, err := res.Resolve(ctx, body.Track, body.Quality)
	if err != nil {
		s.log.Info("resolve failed", "source", body.Source, "title", body.Track.Title, "err", err)
		fail(w, http.StatusBadGateway, "resolve_failed", err.Error())
		return
	}

	proxyOn := s.app.Cfg.Sources.Proxy.Enabled
	needProxy := len(p.Headers) > 0 // custom headers can only be sent by the server
	if needProxy && !proxyOn {
		fail(w, http.StatusForbidden, "proxy_disabled", "this song needs the server relay, which is disabled")
		return
	}
	out := map[string]any{"url": p.URL, "format": p.Format, "mode": "direct"}
	if p.Quality != "" {
		out["quality"] = p.Quality
	}
	if !p.ExpiresAt.IsZero() {
		out["expires_at"] = p.ExpiresAt.Unix()
	}
	if needProxy {
		out["mode"], out["url"] = "proxy", ""
	}
	if proxyOn {
		kind := proxy.KindAudio
		if p.Format == "hls" {
			kind = proxy.KindPlaylist
		}
		link := s.app.Signer.URL(proxy.Target{URL: p.URL, Headers: p.Headers, Kind: kind,
			Ref: clipStr(body.Track.ID, 200), Title: clipStr(body.Track.Title, 120)})
		out["proxy_url"] = strings.TrimPrefix(link, s.basePath)
	}
	if (p.Format == "hls" || body.NeedCORS) && !needProxy {
		out["cors"] = s.corsOK(ctx, p.URL, requestOrigin(r, s.app.Cfg.Server.BaseURL))
	}
	writeJSON(w, http.StatusOK, out)
}

func requestOrigin(r *http.Request, baseURL string) string {
	if o := r.Header.Get("Origin"); o != "" {
		return o
	}
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return "http://" + r.Host
}

// corsOK checks whether an HLS origin allows our page to fetch it (hls.js
// uses XHR, so it needs CORS). Cached per host for an hour.
func (s *Server) corsOK(ctx context.Context, raw, origin string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	key := u.Host + "|" + origin
	s.corsMu.Lock()
	if c, ok := s.corsCache[key]; ok && time.Since(c.at) < time.Hour {
		s.corsMu.Unlock()
		return c.ok
	}
	s.corsMu.Unlock()

	ok := false
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err == nil {
		req.Header.Set("Origin", origin)
		req.Header.Set("User-Agent", netguard.UserAgent)
		req.Header.Set("Range", "bytes=0-1023") // only the headers matter
		if resp, err := s.app.HTTP.Do(req); err == nil {
			acao := resp.Header.Get("Access-Control-Allow-Origin")
			ok = acao == "*" || strings.EqualFold(acao, origin)
			io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
		}
	}
	s.corsMu.Lock()
	if len(s.corsCache) > 500 {
		s.corsCache = map[string]corsEntry{}
	}
	s.corsCache[key] = corsEntry{at: time.Now(), ok: ok}
	s.corsMu.Unlock()
	return ok
}

// ---- relay -----------------------------------------------------------------------------

func (s *Server) overQuota() bool {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	if time.Since(s.quotaAt) > time.Minute {
		s.quotaOver = s.app.Traffic.OverQuota()
		s.quotaAt = time.Now()
	}
	return s.quotaOver
}

// proxyStream relays a signed target. Every byte is metered as "proxy"
// (and on the playback's sid / the page total) by the meter middleware.
func (s *Server) proxyStream(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Cfg
	if !cfg.Sources.Proxy.Enabled {
		fail(w, http.StatusForbidden, "proxy_disabled", "relay is disabled")
		return
	}
	q := r.URL.Query()
	t, err := s.app.Signer.Verify(q.Get("d"), q.Get("s"))
	if err != nil {
		fail(w, http.StatusForbidden, "bad_link", err.Error())
		return
	}
	if cfg.Traffic.BlockProxyOver && s.overQuota() {
		fail(w, http.StatusForbidden, "over_quota", "monthly traffic quota reached; relay is paused")
		return
	}
	sid := q.Get("sid")
	if !reSID.MatchString(sid) {
		sid = ""
	}
	if sid != "" {
		attachStream(r, s.app.Traffic.OpenStream(sid, auth.FromContext(r.Context()).ID, traffic.Proxy, t.Ref, t.Title))
	}
	if t.Kind == proxy.KindAudio {
		select {
		case s.relaySem <- struct{}{}:
			defer func() { <-s.relaySem }()
		default:
			fail(w, http.StatusTooManyRequests, "relay_busy", "too many relayed streams right now")
			return
		}
	}

	method := http.MethodGet
	if r.Method == http.MethodHead {
		method = http.MethodHead
	}
	req, err := http.NewRequestWithContext(r.Context(), method, t.URL, nil)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_link", "bad target")
		return
	}
	req.Header.Set("User-Agent", netguard.UserAgent)
	req.Header.Set("Accept", "*/*")
	if t.Kind != proxy.KindPlaylist {
		for _, h := range []string{"Range", "If-Range"} {
			if v := r.Header.Get(h); v != "" {
				req.Header.Set(h, v)
			}
		}
	}
	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}
	resp, err := s.app.StreamHTTP.Do(req)
	if err != nil {
		if r.Context().Err() == nil {
			s.log.Info("relay upstream", "err", err)
		}
		fail(w, http.StatusBadGateway, "upstream", "cannot reach the audio source")
		return
	}
	defer resp.Body.Close()

	h := w.Header()
	h.Set("Cache-Control", "no-store")
	if t.Kind == proxy.KindPlaylist && resp.StatusCode < 400 {
		b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			fail(w, http.StatusBadGateway, "upstream", "playlist read failed")
			return
		}
		sign := func(abs, kind string) string {
			u := s.app.Signer.URL(proxy.Target{URL: abs, Headers: t.Headers, Kind: kind, Ref: t.Ref, Title: t.Title})
			if sid != "" {
				u += "&sid=" + sid
			}
			return u
		}
		out := proxy.RewritePlaylist(string(b), resp.Request.URL, sign)
		h.Set("Content-Type", "application/vnd.apple.mpegurl")
		h.Set("Content-Length", strconv.Itoa(len(out)))
		w.WriteHeader(http.StatusOK)
		if method != http.MethodHead {
			io.WriteString(w, out)
		}
		return
	}

	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	if ct := h.Get("Content-Type"); ct == "" || (t.Kind == proxy.KindAudio && strings.HasPrefix(ct, "text/")) {
		h.Set("Content-Type", "audio/mpeg") // ICY servers often say text/html
	}
	if q.Get("dl") == "1" && t.Kind == proxy.KindAudio && resp.StatusCode < 300 {
		// "download to this device": same relay, saved as a file
		setAttachment(w, q.Get("name"), audioExt(h.Get("Content-Type"), t.URL))
	}
	w.WriteHeader(resp.StatusCode)
	if method == http.MethodHead || resp.StatusCode >= 400 {
		return
	}
	buf := make([]byte, 32<<10)
	fl, _ := w.(http.Flusher)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// ---- radio --------------------------------------------------------------------------------

func (s *Server) radioStations(w http.ResponseWriter, r *http.Request) {
	if s.app.Radio == nil {
		fail(w, http.StatusNotFound, "disabled", "radio is disabled")
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	list, err := s.app.Radio.Stations(ctx, radio.Query{Name: clipStr(strings.TrimSpace(q.Get("q")), 100),
		Country: clipStr(q.Get("country"), 2), Tag: clipStr(q.Get("tag"), 40),
		Language: clipStr(q.Get("lang"), 40), Page: page, HTTPSOnly: q.Get("https") == "1"})
	if err != nil {
		fail(w, http.StatusBadGateway, "radio_failed", err.Error())
		return
	}
	items := make([]sources.TrackRef, 0, len(list))
	for _, st := range list {
		items = append(items, radio.ToRef(st))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": len(list) == radio.PageSize})
}

func (s *Server) radioCountries(w http.ResponseWriter, r *http.Request) {
	if s.app.Radio == nil {
		fail(w, http.StatusNotFound, "disabled", "radio is disabled")
		return
	}
	list, err := s.app.Radio.Countries(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, "radio_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"countries": list})
}

// ---- online lyrics -------------------------------------------------------------------------

func (s *Server) lyricsLookup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dur, _ := strconv.ParseInt(q.Get("duration_ms"), 10, 64)
	res := s.app.Lyrics.Lookup(r.Context(), clipStr(q.Get("title"), 200), clipStr(q.Get("artist"), 200),
		clipStr(q.Get("album"), 200), dur)
	writeJSON(w, http.StatusOK, res)
}

// ---- LX scripts (admin) -----------------------------------------------------------------

func (s *Server) lxState() map[string]any {
	type plat struct {
		Code      string   `json:"code"`
		Name      string   `json:"name"`
		Search    bool     `json:"search"`
		Qualities []string `json:"qualities"`
	}
	out := map[string]any{"enabled": s.app.LX != nil, "scripts": []lx.Script{}, "platforms": []plat{}}
	if s.app.LX == nil {
		return out
	}
	qs := s.app.LX.Qualities()
	var ps []plat
	for _, p := range s.app.Cfg.Sources.LX.Platforms {
		q := qs[p]
		if q == nil {
			q = []string{}
		}
		ps = append(ps, plat{Code: p, Name: lx.PlatformNames[p], Search: lx.HasSearch(p), Qualities: q})
	}
	if ps != nil {
		out["platforms"] = ps
	}
	out["scripts"] = s.app.LX.List()
	out["dir"] = s.app.LX.Dir()
	return out
}

func (s *Server) lxList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.lxState())
}

func (s *Server) lxUpload(w http.ResponseWriter, r *http.Request) {
	if s.app.LX == nil {
		fail(w, http.StatusNotFound, "disabled", "LX sources are disabled (sources.lx.enabled)")
		return
	}
	var body struct {
		File    string `json:"file"`
		Content string `json:"content"`
	}
	if err := readJSON(r, 3*lx.MaxScriptSize, &body); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", "invalid upload")
		return
	}
	if _, err := s.app.LX.Save(body.File, []byte(body.Content)); err != nil {
		fail(w, http.StatusBadRequest, "bad_script", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.lxState())
}

func (s *Server) lxToggle(w http.ResponseWriter, r *http.Request) {
	if s.app.LX == nil {
		fail(w, http.StatusNotFound, "disabled", "LX sources are disabled")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSON(r, 256, &body); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := s.app.LX.SetEnabled(r.PathValue("id"), body.Enabled); err != nil {
		fail(w, http.StatusNotFound, "not_found", "no such script")
		return
	}
	writeJSON(w, http.StatusOK, s.lxState())
}

func (s *Server) lxDelete(w http.ResponseWriter, r *http.Request) {
	if s.app.LX == nil {
		fail(w, http.StatusNotFound, "disabled", "LX sources are disabled")
		return
	}
	if err := s.app.LX.Delete(r.PathValue("id")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(w, http.StatusNotFound, "not_found", "no such script")
		} else {
			fail(w, http.StatusInternalServerError, "error", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, s.lxState())
}

func (s *Server) lxReload(w http.ResponseWriter, r *http.Request) {
	if s.app.LX != nil {
		s.app.LX.Reload()
	}
	writeJSON(w, http.StatusOK, s.lxState())
}

func clipStr(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// audioExt picks a file extension for a downloaded song.
func audioExt(contentType, rawURL string) string {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch ct {
	case "audio/flac", "audio/x-flac":
		return ".flac"
	case "audio/mp4", "audio/x-m4a", "audio/m4a", "audio/aac", "audio/x-aac":
		return ".m4a"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	}
	if u, err := url.Parse(rawURL); err == nil {
		switch e := strings.ToLower(path.Ext(u.Path)); e {
		case ".mp3", ".flac", ".m4a", ".aac", ".ogg", ".opus", ".wav":
			return e
		}
	}
	return ".mp3"
}
