package httpapi

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"

	"bailing/internal/lyrics"
)

// ---- manual lyrics search (wrong / missing automatic match) -------------------------------

// lyricsSearch: GET /api/lyrics/search?title=&artist=&album=&duration_ms=
func (s *Server) lyricsSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dur, _ := strconv.ParseInt(q.Get("duration_ms"), 10, 64)
	list, err := s.app.Lyrics.Search(r.Context(), clipStr(q.Get("title"), 200),
		clipStr(q.Get("artist"), 200), clipStr(q.Get("album"), 200), dur)
	if err != nil {
		lyricsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": list})
}

// lyricsPickTrack: PUT /api/music/tracks/{id}/lyrics {"lrclib_id": 123}
func (s *Server) lyricsPickTrack(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		LRCLibID int64 `json:"lrclib_id"`
	}
	if !reID.MatchString(id) || readJSON(r, 256, &body) != nil || body.LRCLibID <= 0 {
		fail(w, http.StatusBadRequest, "bad_request", "invalid lyrics choice")
		return
	}
	res, err := s.app.Lyrics.PickForTrack(r.Context(), id, body.LRCLibID)
	if errors.Is(err, fs.ErrNotExist) {
		fail(w, http.StatusNotFound, "not_found", "no such track")
		return
	}
	if err != nil {
		lyricsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type onlinePick struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	LRCLibID int64  `json:"lrclib_id"`
}

// lyricsPickOnline: PUT /api/lyrics/pick {"title","artist","album","lrclib_id"}
func (s *Server) lyricsPickOnline(w http.ResponseWriter, r *http.Request) {
	var b onlinePick
	if readJSON(r, 4096, &b) != nil || b.LRCLibID <= 0 || b.Title == "" {
		fail(w, http.StatusBadRequest, "bad_request", "invalid lyrics choice")
		return
	}
	res, err := s.app.Lyrics.PickOnline(r.Context(), clipStr(b.Title, 200), clipStr(b.Artist, 200),
		clipStr(b.Album, 200), b.LRCLibID)
	if err != nil {
		lyricsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// lyricsClearOnline: DELETE /api/lyrics/pick?title=&artist=&album=
func (s *Server) lyricsClearOnline(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.app.Lyrics.ClearOnline(clipStr(q.Get("title"), 200), clipStr(q.Get("artist"), 200),
		clipStr(q.Get("album"), 200))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func lyricsFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, lyrics.ErrOffline):
		fail(w, http.StatusConflict, "lyrics_offline", "online lyrics are disabled (lyrics.online)")
	case errors.Is(err, lyrics.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found", "lyrics not found")
	default:
		fail(w, http.StatusBadGateway, "lyrics_failed", "lyrics service unreachable: "+err.Error())
	}
}
