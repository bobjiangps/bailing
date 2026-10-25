package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bailing/internal/auth"
)

// Favourites and playlists. Items are opaque JSON snapshots written by the
// front end (a local Track or an online TrackRef); the server only checks
// that "source" and "id" are present so it can de-duplicate and so a
// favourite can be found again.

const (
	maxItemBytes      = 32 << 10
	maxFavorites      = 5000
	maxPlaylists      = 200
	maxPlaylistItems  = 2000
	maxPlaylistName   = 80
	collectionsBodyMB = 8 << 20
)

type itemKey struct {
	Source string `json:"source"`
	ID     string `json:"id"`
}

// checkItem validates an item snapshot and returns its key.
func checkItem(raw json.RawMessage) (itemKey, error) {
	var k itemKey
	if len(raw) == 0 || len(raw) > maxItemBytes {
		return k, errors.New("item missing or too large")
	}
	if err := json.Unmarshal(raw, &k); err != nil {
		return k, errors.New("item must be an object")
	}
	if !reSource.MatchString(k.Source) || k.ID == "" || len(k.ID) > 512 {
		return k, errors.New("item needs a valid source and id")
	}
	return k, nil
}

// ---- favourites ----------------------------------------------------------------

type favEntry struct {
	Kind string          `json:"kind"`
	Item json.RawMessage `json:"item"`
	At   int64           `json:"at"`
}

func (s *Server) favList(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	q := `SELECT kind, data, created_at FROM favorites WHERE user_id=?`
	args := []any{u.ID}
	if k := r.URL.Query().Get("kind"); k != "" {
		q += ` AND kind=?`
		args = append(args, k)
	}
	rows, err := s.app.DB.Query(q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "query failed")
		return
	}
	defer rows.Close()
	out := []favEntry{}
	for rows.Next() {
		var e favEntry
		var data string
		if rows.Scan(&e.Kind, &data, &e.At) == nil {
			e.Item = json.RawMessage(data)
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) favAdd(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	var body struct {
		Kind  string            `json:"kind"`
		Item  json.RawMessage   `json:"item"`
		Items []json.RawMessage `json:"items"` // bulk import (e.g. old browser-only favourites)
	}
	if err := readJSON(r, collectionsBodyMB, &body); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.Kind != "track" && body.Kind != "station" {
		fail(w, http.StatusBadRequest, "bad_request", "kind must be track or station")
		return
	}
	items := body.Items
	if body.Item != nil {
		items = append(items, body.Item)
	}
	var count int
	s.app.DB.QueryRow(`SELECT COUNT(*) FROM favorites WHERE user_id=?`, u.ID).Scan(&count)
	if count+len(items) > maxFavorites {
		fail(w, http.StatusBadRequest, "too_many", "too many favourites")
		return
	}
	tx, err := s.app.DB.Begin()
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	for i, raw := range items {
		k, err := checkItem(raw)
		if err != nil {
			fail(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		// bulk imports keep their order: earlier items are newer
		at := now - int64(i)
		if _, err := tx.Exec(`INSERT INTO favorites(user_id, kind, source, ref, data, created_at)
			VALUES(?,?,?,?,?,?) ON CONFLICT(user_id, source, ref) DO UPDATE SET data=excluded.data`,
			u.ID, body.Kind, k.Source, k.ID, string(raw), at); err != nil {
			fail(w, http.StatusInternalServerError, "error", "save failed")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) favRemove(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	var k itemKey
	if err := readJSON(r, 4096, &k); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	s.app.DB.Exec(`DELETE FROM favorites WHERE user_id=? AND source=? AND ref=?`, u.ID, k.Source, k.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- playlists -----------------------------------------------------------------

type playlistInfo struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Count     int             `json:"count"`
	First     json.RawMessage `json:"first,omitempty"` // first item, for the cover
	UpdatedAt int64           `json:"updated_at"`
}

func cleanName(n string) (string, error) {
	n = strings.TrimSpace(n)
	if n == "" {
		return "", errors.New("name is required")
	}
	if r := []rune(n); len(r) > maxPlaylistName {
		n = string(r[:maxPlaylistName])
	}
	return n, nil
}

// ownPlaylist returns the playlist id from the path if it belongs to the user.
func (s *Server) ownPlaylist(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u := auth.FromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		var owner int64
		err = s.app.DB.QueryRow(`SELECT user_id FROM playlists WHERE id=?`, id).Scan(&owner)
		if err == nil && owner == u.ID {
			return id, true
		}
	}
	fail(w, http.StatusNotFound, "not_found", "no such playlist")
	return 0, false
}

func (s *Server) plList(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	rows, err := s.app.DB.Query(`SELECT p.id, p.name, p.updated_at,
			(SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id=p.id),
			COALESCE((SELECT data FROM playlist_items i WHERE i.playlist_id=p.id ORDER BY pos LIMIT 1), '')
		FROM playlists p WHERE p.user_id=? ORDER BY p.updated_at DESC`, u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "query failed")
		return
	}
	defer rows.Close()
	out := []playlistInfo{}
	for rows.Next() {
		var p playlistInfo
		var first string
		if rows.Scan(&p.ID, &p.Name, &p.UpdatedAt, &p.Count, &first) == nil {
			if first != "" {
				p.First = json.RawMessage(first)
			}
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlists": out})
}

func (s *Server) plCreate(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	var body struct {
		Name  string            `json:"name"`
		Items []json.RawMessage `json:"items"`
	}
	if err := readJSON(r, collectionsBodyMB, &body); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	name, err := cleanName(body.Name)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var n int
	s.app.DB.QueryRow(`SELECT COUNT(*) FROM playlists WHERE user_id=?`, u.ID).Scan(&n)
	if n >= maxPlaylists {
		fail(w, http.StatusBadRequest, "too_many", "too many playlists")
		return
	}
	tx, err := s.app.DB.Begin()
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	res, err := tx.Exec(`INSERT INTO playlists(user_id, name, created_at, updated_at) VALUES(?,?,?,?)`,
		u.ID, name, now, now)
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	id, _ := res.LastInsertId()
	if err := writeItems(tx, id, 0, body.Items); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name})
}

// writeItems inserts items starting at position from.
func writeItems(tx *sql.Tx, id int64, from int, items []json.RawMessage) error {
	if from+len(items) > maxPlaylistItems {
		return errors.New("a playlist holds at most 2000 items")
	}
	for i, raw := range items {
		k, err := checkItem(raw)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO playlist_items(playlist_id, pos, source, ref, data) VALUES(?,?,?,?,?)`,
			id, from+i, k.Source, k.ID, string(raw)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) plGet(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ownPlaylist(w, r)
	if !ok {
		return
	}
	var name string
	var updated int64
	s.app.DB.QueryRow(`SELECT name, updated_at FROM playlists WHERE id=?`, id).Scan(&name, &updated)
	rows, err := s.app.DB.Query(`SELECT data FROM playlist_items WHERE playlist_id=? ORDER BY pos`, id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "query failed")
		return
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil {
			items = append(items, json.RawMessage(d))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name, "updated_at": updated, "items": items})
}

// plUpdate renames a playlist and/or replaces all of its items (used for
// removing and reordering).
func (s *Server) plUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ownPlaylist(w, r)
	if !ok {
		return
	}
	var body struct {
		Name  *string            `json:"name"`
		Items *[]json.RawMessage `json:"items"`
	}
	if err := readJSON(r, collectionsBodyMB, &body); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	tx, err := s.app.DB.Begin()
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	defer tx.Rollback()
	if body.Name != nil {
		name, err := cleanName(*body.Name)
		if err != nil {
			fail(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		tx.Exec(`UPDATE playlists SET name=? WHERE id=?`, name, id)
	}
	if body.Items != nil {
		tx.Exec(`DELETE FROM playlist_items WHERE playlist_id=?`, id)
		if err := writeItems(tx, id, 0, *body.Items); err != nil {
			fail(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	tx.Exec(`UPDATE playlists SET updated_at=? WHERE id=?`, time.Now().UnixMilli(), id)
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// plAppend adds items at the end, skipping ones already in the playlist.
func (s *Server) plAppend(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ownPlaylist(w, r)
	if !ok {
		return
	}
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := readJSON(r, collectionsBodyMB, &body); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	tx, err := s.app.DB.Begin()
	if err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	defer tx.Rollback()
	var next int
	tx.QueryRow(`SELECT COALESCE(MAX(pos)+1, 0) FROM playlist_items WHERE playlist_id=?`, id).Scan(&next)
	var fresh []json.RawMessage
	for _, raw := range body.Items {
		k, err := checkItem(raw)
		if err != nil {
			fail(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		var dup int
		tx.QueryRow(`SELECT COUNT(*) FROM playlist_items WHERE playlist_id=? AND source=? AND ref=?`,
			id, k.Source, k.ID).Scan(&dup)
		if dup == 0 {
			fresh = append(fresh, raw)
		}
	}
	if err := writeItems(tx, id, next, fresh); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	tx.Exec(`UPDATE playlists SET updated_at=? WHERE id=?`, time.Now().UnixMilli(), id)
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, "error", "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": len(fresh), "skipped": len(body.Items) - len(fresh)})
}

func (s *Server) plDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ownPlaylist(w, r)
	if !ok {
		return
	}
	s.app.DB.Exec(`DELETE FROM playlists WHERE id=?`, id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
