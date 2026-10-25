package httpapi

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"bailing/internal/auth"
	"bailing/internal/library/music"
	"bailing/internal/library/photo"
	"bailing/internal/netguard"
	"bailing/internal/stats"
	"bailing/internal/traffic"
)

// ---- albums & photos -------------------------------------------------------------

func (s *Server) albums(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.Photos.Albums()
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"albums": list, "scanning": s.app.Photos.Scanning()})
}

func (s *Server) album(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !reID.MatchString(id) {
		fail(w, http.StatusNotFound, "not_found", "no such album")
		return
	}
	a, photos, err := s.app.Photos.Album(id)
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", "no such album")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"album": a, "photos": photos})
}

func (s *Server) photoExif(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !reID.MatchString(id) {
		fail(w, http.StatusNotFound, "not_found", "no such photo")
		return
	}
	ex, err := s.app.Photos.Exif(id)
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", "no such photo")
		return
	}
	if r.URL.Query().Get("gps") != "1" { // location is opt-in (privacy)
		ex.Lat, ex.Lon = 0, 0
	}
	writeJSON(w, http.StatusOK, ex)
}

func (s *Server) photoMedia(w http.ResponseWriter, r *http.Request) {
	id, size := r.PathValue("id"), r.PathValue("size")
	if !reID.MatchString(id) || (size != photo.SizeS && size != photo.SizeM && size != photo.SizeO) {
		http.NotFound(w, r)
		return
	}
	path, err := s.app.Photos.Rendition(r.Context(), id, size)
	switch {
	case errors.Is(err, photo.ErrPending):
		w.Header().Set("Retry-After", "3")
		http.Error(w, "rendition pending", http.StatusServiceUnavailable)
		return
	case errors.Is(err, fs.ErrNotExist):
		http.NotFound(w, r)
		return
	case err != nil:
		s.log.Warn("photo rendition", "id", id, "size", size, "err", err)
		http.Error(w, "image unavailable", http.StatusServiceUnavailable)
		return
	}
	serveFile(w, r, path, "private, max-age=2592000")
}

// serveFile serves a file with ETag + Range support via http.ServeContent.
func serveFile(w http.ResponseWriter, r *http.Request, path, cacheControl string) {
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("ETag", `"`+strconv.FormatInt(st.ModTime().UnixNano(), 36)+"-"+
		strconv.FormatInt(st.Size(), 36)+`"`)
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// ---- music ------------------------------------------------------------------------

func (s *Server) tracks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	off, _ := strconv.Atoi(q.Get("offset"))
	lim, _ := strconv.Atoi(q.Get("limit"))
	// a present-but-empty filter means "unknown artist / album": it must
	// select those tracks, not switch the filter off
	opt := func(k string) *string {
		if !q.Has(k) {
			return nil
		}
		v := q.Get(k)
		return &v
	}
	list, total, err := s.app.Music.Tracks(music.Query{Q: strings.TrimSpace(q.Get("q")),
		Folder: opt("folder"), Artist: opt("artist"), Album: opt("album"),
		Sort: q.Get("sort"), Offset: off, Limit: lim})
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": list, "total": total,
		"scanning": s.app.Music.Scanning()})
}

func (s *Server) groups(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.Music.Groups(r.PathValue("kind"))
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": list})
}

func (s *Server) lyrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !reID.MatchString(id) {
		fail(w, http.StatusNotFound, "not_found", "no such track")
		return
	}
	res, err := s.app.Lyrics.ForTrack(r.Context(), id)
	if errors.Is(err, fs.ErrNotExist) {
		fail(w, http.StatusNotFound, "not_found", "no such track")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "lyrics failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// lyricsRefetch drops a (possibly wrong) downloaded match; the next GET
// queries LRCLIB again.
func (s *Server) lyricsRefetch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !reID.MatchString(id) {
		fail(w, http.StatusNotFound, "not_found", "no such track")
		return
	}
	s.app.Lyrics.ClearCache(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) trackDuration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Ms int64 `json:"ms"`
	}
	if !reID.MatchString(id) || readJSON(r, 256, &body) != nil || body.Ms <= 0 || body.Ms > 24*3600*1000 {
		fail(w, http.StatusBadRequest, "bad_request", "invalid duration")
		return
	}
	s.app.Music.SetDuration(id, body.Ms)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// trackMedia streams an audio file. The optional ?sid= ties all range
// requests of one playback to a live traffic counter.
func (s *Server) trackMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !reID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	t, err := s.app.Music.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if sid := r.URL.Query().Get("sid"); reSID.MatchString(sid) {
		u := auth.FromContext(r.Context())
		attachStream(r, s.app.Traffic.OpenStream(sid, u.ID, traffic.Track, id, t.Title))
	}
	if r.URL.Query().Get("dl") == "1" {
		name := t.Title
		if t.Artist != "" {
			name = t.Artist + " - " + t.Title
		}
		setAttachment(w, name, "."+t.Format)
	}
	serveFile(w, r, t.Path, "private, max-age=86400")
}

// setAttachment makes the browser save the response as "<name><ext>".
func setAttachment(w http.ResponseWriter, name, ext string) {
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	name = clipStr(name, 120)
	if name == "" {
		name = "audio"
	}
	full := name + ext
	ascii := strings.Map(func(r rune) rune {
		if r > 126 {
			return '_'
		}
		return r
	}, full)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		ascii, url.PathEscape(full)))
}

func (s *Server) coverMedia(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !reCover.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	size := r.URL.Query().Get("size")
	if size != "s" && size != "m" {
		size = "s"
	}
	path, err := s.app.Music.CoverPath(r.Context(), key, size)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveFile(w, r, path, "private, max-age=2592000")
}

// coverHosts are the image hosts of the music platforms whose covers the
// search results link to. Only these may be fetched through /media/rcover,
// so the endpoint cannot be used as a general proxy.
var coverHosts = []string{"kuwo.cn", "kugou.com", "gtimg.cn", "qq.com", "126.net", "163.com", "migu.cn"}

// remoteCover fetches an online cover for browsers that cannot load it
// directly (some networks / proxies block these image hosts in the browser
// only). The page uses it only after the direct load failed.
func (s *Server) remoteCover(w http.ResponseWriter, r *http.Request) {
	u, err := url.Parse(r.URL.Query().Get("u"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		http.NotFound(w, r)
		return
	}
	host := strings.ToLower(u.Hostname())
	allowed := false
	for _, h := range coverHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			allowed = true
			break
		}
	}
	if !allowed {
		http.NotFound(w, r)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	req.Header.Set("User-Agent", netguard.UserAgent)
	resp, err := s.app.HTTP.Do(req)
	if err != nil {
		fail(w, http.StatusBadGateway, "upstream", "cover not reachable")
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		http.NotFound(w, r)
		return
	}
	const maxCover = 3 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxCover+1))
	if err != nil || len(b) > maxCover {
		http.NotFound(w, r)
		return
	}
	if ct == "image/jpg" {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Write(b)
}

func (s *Server) sourcesList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": s.app.Sources.List()})
}

// ---- stats ------------------------------------------------------------------------

func (s *Server) statsSummary(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	sum, err := s.app.Stats.Summary(auth.FromContext(r.Context()).ID, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "query failed")
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) statsPlay(w http.ResponseWriter, r *http.Request) {
	var p stats.Play
	if err := readJSON(r, 4096, &p); err != nil || p.Ref == "" || !reSource.MatchString(p.Source) {
		fail(w, http.StatusBadRequest, "bad_request", "invalid play event")
		return
	}
	if p.Source == "local" { // trust the library, not the client, for local metadata
		if t, err := s.app.Music.Get(p.Ref); err == nil {
			p.Title, p.Artist, p.Cover = t.Title, t.Artist, t.Cover
		} else {
			fail(w, http.StatusBadRequest, "bad_request", "unknown track")
			return
		}
	} else {
		p.Ref = clipStr(p.Ref, 1000) // direct URLs can be long
		if !strings.HasPrefix(p.Cover, "https://") {
			p.Cover = ""
		}
		p.Cover = clipStr(p.Cover, 500)
	}
	s.app.Stats.RecordPlay(auth.FromContext(r.Context()).ID, p)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) statsView(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	}
	if err := readJSON(r, 512, &body); err != nil || (body.Kind != "album" && body.Kind != "photo") ||
		!reID.MatchString(body.Ref) {
		fail(w, http.StatusBadRequest, "bad_request", "invalid view event")
		return
	}
	s.app.Stats.RecordView(auth.FromContext(r.Context()).ID, body.Kind, body.Ref)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- traffic ----------------------------------------------------------------------

func (s *Server) trafficSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.app.Traffic.Summary(30)
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "query failed")
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) trafficStream(w http.ResponseWriter, r *http.Request) {
	st, ok := s.app.Traffic.StreamInfo(r.PathValue("sid"), auth.FromContext(r.Context()).ID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"bytes": 0, "active": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bytes": st.Bytes, "started_at": st.Started,
		"last_at": st.Last, "category": st.Category, "active": true})
}

// trafficPage returns everything the server has sent to the calling browser
// page since it was (re)opened.
func (s *Server) trafficPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	pg, ok := s.app.Traffic.PageInfo(pageID(r))
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"bytes": 0, "requests": 0,
			"by_category": map[string]int64{}, "active": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bytes": pg.Bytes, "requests": pg.Requests,
		"by_category": pg.ByCat, "started_at": pg.Started, "active": true})
}

// ---- admin ------------------------------------------------------------------------

func (s *Server) rescan(w http.ResponseWriter, r *http.Request) {
	what := r.URL.Query().Get("what")
	if what != "photos" && what != "music" {
		what = "all"
	}
	go s.app.Scan(what)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "what": what})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	vips, err := s.app.Vips.Version()
	if err != nil {
		vips = ""
	}
	albums, photos := s.app.Photos.Count()
	var last any
	if t := s.app.LastScan(); !t.IsZero() {
		last = t.Unix()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         s.app.Version,
		"vips":            vips,
		"scanning_photos": s.app.Photos.Scanning(),
		"scanning_music":  s.app.Music.Scanning(),
		"queue":           s.app.Queue.Pending(),
		"last_scan":       last,
		"albums":          albums,
		"photos":          photos,
		"tracks":          s.app.Music.Count(),
	})
}
