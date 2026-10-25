package httpapi

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Storage overview for the settings page (admin): configured media
// directories, the data directory and how much the caches take.
//
// Media directories are only configured in the config file (paths.photos /
// paths.music). They are deliberately not editable from the browser: a web
// form that lets someone point the server at arbitrary directories would be
// a much bigger risk than editing one file over SSH.

type dirInfo struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

type usage struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

func dirInfos(paths []string) []dirInfo {
	out := make([]dirInfo, 0, len(paths))
	for _, p := range paths {
		st, err := os.Stat(p)
		out = append(out, dirInfo{Path: p, Exists: err == nil && st.IsDir()})
	}
	return out
}

func dirUsage(dir string) usage {
	u := usage{Path: dir}
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			u.Bytes += info.Size()
			u.Files++
		}
		return nil
	})
	return u
}

func fileSize(path string) int64 {
	var n int64
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if st, err := os.Stat(p); err == nil {
			n += st.Size()
		}
	}
	return n
}

func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Cfg
	writeJSON(w, http.StatusOK, map[string]any{
		"config_file": cfg.File(),
		"photos":      dirInfos(s.app.Photos.Roots()),
		"music":       dirInfos(s.app.Music.Roots()),
		"data":        cfg.Paths.Data,
		"db_bytes":    fileSize(cfg.DataPath("bailing.db")),
		"photo_cache": dirUsage(cfg.DataPath("cache", "photo")),
		"cover_cache": dirUsage(cfg.DataPath("cache", "cover")),
	})
}

// cacheClean removes cache files nothing refers to any more:
//   - photo renditions of photos that are no longer in the library
//     (normally removed during the scan; this catches leftovers);
//   - extracted covers no track uses (covers are content-addressed and
//     shared, so the scan never deletes them).
//
// Everything still in use is kept, so nothing has to be regenerated.
func (s *Server) cacheClean(w http.ResponseWriter, r *http.Request) {
	if s.app.Photos.Scanning() || s.app.Music.Scanning() {
		fail(w, http.StatusConflict, "scanning", "a library scan is running, try again later")
		return
	}
	keep := func(query string) (map[string]bool, error) {
		rows, err := s.app.DB.Query(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		m := map[string]bool{}
		for rows.Next() {
			var k string
			if rows.Scan(&k) == nil && k != "" {
				m[k] = true
			}
		}
		return m, rows.Err()
	}
	photos, err := keep(`SELECT id FROM photos`)
	if err != nil {
		fail(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	covers, err := keep(`SELECT DISTINCT cover_key FROM tracks WHERE cover_key <> ''`)
	if err != nil {
		fail(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	var freed int64
	var files int
	prune := func(dir string, inUse map[string]bool) {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if inUse[cacheKey(d.Name())] {
				return nil
			}
			info, ierr := d.Info()
			if os.Remove(p) == nil {
				files++
				if ierr == nil {
					freed += info.Size()
				}
			}
			return nil
		})
	}
	prune(s.app.Cfg.DataPath("cache", "photo"), photos)
	prune(s.app.Cfg.DataPath("cache", "cover"), covers)
	s.app.Log.Info("cache cleaned", "files", files, "bytes", freed)
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "bytes": freed})
}

// cacheKey is the id / cover key a cache file belongs to:
// "<id>_s.webp" → id, "<key>.jpg" → key, "<key>_m.webp" → key.
func cacheKey(name string) string {
	if i := strings.IndexAny(name, "_."); i > 0 {
		return name[:i]
	}
	return name
}
