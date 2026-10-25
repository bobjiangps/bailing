// Package photo scans the photo root (one sub-directory = one album),
// keeps the albums/photos tables in sync and produces cached WebP sizes.
package photo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"bailing/internal/imaging"
	"bailing/internal/library"
	"bailing/internal/store"

	"golang.org/x/image/webp"
)

// Sizes of cached renditions.
const (
	SizeS = "s" // shelf / album pages (~480px)
	SizeM = "m" // viewer (~2048px)
	SizeO = "o" // original resolution (HEIC converted to WebP)
)

// ErrPending is returned when a rendition is still being generated.
var ErrPending = errors.New("rendition pending")

// browserNative formats can be served as-is for the "o" size.
var browserNative = map[string]bool{"jpg": true, "jpeg": true, "png": true, "webp": true, "gif": true}

// Library is the photo library service.
type Library struct {
	db       *store.DB
	roots    library.Roots
	cacheDir string
	exts     []string
	vips     *imaging.Vips
	queue    *imaging.Queue
	sizeS    int
	sizeM    int
	log      *slog.Logger

	scanMu   sync.Mutex
	scanning bool
}

// New creates the photo library over one or more directories (every
// sub-directory of each one is an album; photos directly inside a
// directory form one more album named after it).
func New(db *store.DB, roots []string, cacheDir string, exts []string, v *imaging.Vips,
	q *imaging.Queue, sizeS, sizeM int, log *slog.Logger) *Library {
	return &Library{db: db, roots: roots, cacheDir: cacheDir, exts: exts, vips: v, queue: q,
		sizeS: sizeS, sizeM: sizeM, log: log}
}

// Album is a shelf entry.
type Album struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Date        string `json:"date"`
	Cover       string `json:"cover"`
	Count       int    `json:"count"`
	Color       string `json:"color"`
}

// Photo is a page entry inside an album.
type Photo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Width  int    `json:"w"`
	Height int    `json:"h"`
	Color  string `json:"color"`
	Taken  int64  `json:"taken,omitempty"`
	Ready  bool   `json:"ready"`
}

// Roots are the configured photo directories.
func (l *Library) Roots() []string { return l.roots }

// Scanning reports whether a scan is running.
func (l *Library) Scanning() bool {
	l.scanMu.Lock()
	defer l.scanMu.Unlock()
	return l.scanning
}

// Scan incrementally synchronises the database with the photo directories.
func (l *Library) Scan(ctx context.Context) error {
	if len(l.roots) == 0 {
		return nil
	}
	l.scanMu.Lock()
	if l.scanning {
		l.scanMu.Unlock()
		return errors.New("scan already running")
	}
	l.scanning = true
	l.scanMu.Unlock()
	defer func() {
		l.scanMu.Lock()
		l.scanning = false
		l.scanMu.Unlock()
	}()

	start := time.Now()
	// retry photos that failed last time (e.g. vips was installed since)
	l.db.Exec(`UPDATE photos SET processed=0 WHERE processed=2`)
	seenAlbums := map[string]bool{}
	missing := false
	for i, root := range l.roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			// e.g. an unmounted disk: keep its albums instead of dropping them
			l.log.Warn("photo directory not available", "dir", root, "err", err)
			missing = true
			continue
		}
		for _, e := range entries {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !e.IsDir() || library.Hidden(e.Name()) {
				continue
			}
			dir := l.roots.Prefix(i) + e.Name()
			id, err := l.scanAlbum(dir, filepath.Join(root, e.Name()), e.Name())
			if err != nil {
				l.log.Warn("scan album", "dir", dir, "err", err)
				continue
			}
			if id != "" {
				seenAlbums[id] = true
			}
		}
		// Photos lying directly in the configured directory form one more
		// album named after the directory (e.g. a plain folder of
		// screenshots added as an extra root). Stored as "." for the first
		// root and "@<key>" for the others, so Roots.Abs resolves them.
		rootDir := "."
		if i > 0 {
			rootDir = strings.TrimSuffix(l.roots.Prefix(i), "/")
		}
		id, err := l.scanAlbum(rootDir, root, l.roots.Label(i))
		if err != nil {
			l.log.Warn("scan album", "dir", root, "err", err)
		} else if id != "" {
			seenAlbums[id] = true
		}
	}

	// remove albums whose directory disappeared
	rows, err := l.db.Query(`SELECT id FROM albums`)
	if err != nil {
		return err
	}
	var gone []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		if !seenAlbums[id] && !missing {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		l.removeAlbum(id)
	}

	// (re)queue unprocessed photos, e.g. after a restart
	l.queueUnprocessed()
	l.log.Info("photo scan done", "dirs", len(l.roots), "albums", len(seenAlbums),
		"took", time.Since(start).Round(time.Millisecond))
	return nil
}

// scanAlbum indexes one album. dir is the stored relative path (with the
// root prefix), albumPath the absolute directory, name its display name.
func (l *Library) scanAlbum(dir, albumPath, name string) (string, error) {
	albumID := library.ID(dir)
	files, err := os.ReadDir(albumPath)
	if err != nil {
		return "", err
	}

	var about About
	if b, err := os.ReadFile(filepath.Join(albumPath, "about.txt")); err == nil {
		about = ParseAbout(b)
	}
	if about.Title == "" {
		about.Title = name
	}

	type fileInfo struct {
		name  string
		size  int64
		mtime int64
	}
	var photos []fileInfo
	var albumMtime int64
	for _, f := range files {
		if f.IsDir() || library.Hidden(f.Name()) || !library.HasExt(f.Name(), l.exts) {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		photos = append(photos, fileInfo{f.Name(), info.Size(), info.ModTime().Unix()})
		if info.ModTime().Unix() > albumMtime {
			albumMtime = info.ModTime().Unix()
		}
	}
	if len(photos) == 0 {
		return "", nil // empty folders are not albums
	}

	// existing rows for change detection
	existing := map[string][2]int64{}
	rows, err := l.db.Query(`SELECT id, size, mtime FROM photos WHERE album_id=?`, albumID)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var id string
		var size, mtime int64
		rows.Scan(&id, &size, &mtime)
		existing[id] = [2]int64{size, mtime}
	}
	rows.Close()

	tx, err := l.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO albums(id, dir, title, description, date_text, photo_count, mtime, sort_mode)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, description=excluded.description,
		date_text=excluded.date_text, photo_count=excluded.photo_count, mtime=excluded.mtime,
		sort_mode=excluded.sort_mode`,
		albumID, dir, about.Title, about.Description, about.Date, len(photos), albumMtime,
		about.Sort); err != nil {
		return "", err
	}

	var changed []string
	seen := map[string]bool{}
	coverID := ""
	sort.Slice(photos, func(i, j int) bool {
		return library.NaturalKey(photos[i].name) < library.NaturalKey(photos[j].name)
	})
	for _, p := range photos {
		rel := filepath.ToSlash(filepath.Join(dir, p.name))
		id := library.ID(rel)
		seen[id] = true
		if coverID == "" || (about.Cover != "" && strings.EqualFold(p.name, about.Cover)) {
			coverID = id
		}
		if old, ok := existing[id]; ok && old[0] == p.size && old[1] == p.mtime {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO photos(id, album_id, rel_path, size, mtime, sort_key, processed)
			VALUES(?,?,?,?,?,?,0)
			ON CONFLICT(id) DO UPDATE SET size=excluded.size, mtime=excluded.mtime, processed=0`,
			id, albumID, rel, p.size, p.mtime, library.NaturalKey(p.name)); err != nil {
			return "", err
		}
		changed = append(changed, id)
	}
	for id := range existing {
		if !seen[id] {
			tx.Exec(`DELETE FROM photos WHERE id=?`, id)
			l.removeCache(id)
		}
	}
	// the album colour follows its cover photo (empty until the cover is processed)
	if _, err := tx.Exec(`UPDATE albums SET cover_photo_id=?,
		color=COALESCE((SELECT color FROM photos WHERE id=?), '') WHERE id=?`,
		coverID, coverID, albumID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	for _, id := range changed {
		l.removeCache(id)
	}
	return albumID, nil
}

func (l *Library) removeAlbum(id string) {
	rows, err := l.db.Query(`SELECT id FROM photos WHERE album_id=?`, id)
	if err == nil {
		var ids []string
		for rows.Next() {
			var pid string
			rows.Scan(&pid)
			ids = append(ids, pid)
		}
		rows.Close()
		for _, pid := range ids {
			l.removeCache(pid)
		}
	}
	l.db.Exec(`DELETE FROM albums WHERE id=?`, id)
}

func (l *Library) queueUnprocessed() {
	rows, err := l.db.Query(`SELECT p.id FROM photos p JOIN albums a ON a.id=p.album_id
		WHERE p.processed=0 ORDER BY (a.cover_photo_id = p.id) DESC, p.album_id, p.sort_key`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		pid := id
		l.queue.Submit("photo:"+pid, func() error { return l.process(pid) }, false)
	}
}

// process reads metadata, renders the s size and computes the colour.
func (l *Library) process(id string) error {
	var rel, albumID string
	if err := l.db.QueryRow(`SELECT rel_path, album_id FROM photos WHERE id=?`, id).
		Scan(&rel, &albumID); err != nil {
		return err
	}
	src := l.roots.Abs(rel)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var ex Exif
	var orientation int
	var taken time.Time
	if hdr, err := l.vips.Header(ctx, src); err == nil {
		ex, orientation, taken = parseHeader(hdr)
	} else {
		l.log.Warn("vipsheader", "file", rel, "err", err)
	}

	sPath := l.CachePath(id, SizeS)
	if err := l.vips.Thumbnail(ctx, src, sPath, l.sizeS); err != nil {
		l.log.Warn("thumbnail", "file", rel, "err", err)
		// mark as processed anyway so we do not retry forever; Ready=false
		l.db.Exec(`UPDATE photos SET processed=2 WHERE id=?`, id)
		return err
	}

	w, h := ex.Width, ex.Height
	if f, err := os.Open(sPath); err == nil {
		if cfg, err := webp.DecodeConfig(f); err == nil && cfg.Width > 0 {
			// thumbnails are already rotated; make w/h match their orientation
			if (cfg.Width > cfg.Height) != (w > h) || w == 0 {
				if w == 0 {
					w, h = cfg.Width, cfg.Height
				} else {
					w, h = h, w
				}
			}
		}
		f.Close()
	}
	_ = orientation // orientation handled through the rendered thumbnail

	color, _ := imaging.DominantColorFile(sPath)
	exJSON, _ := json.Marshal(ex)
	var takenUnix int64
	if !taken.IsZero() {
		takenUnix = taken.Unix()
	}
	if _, err := l.db.Exec(`UPDATE photos SET width=?, height=?, taken_at=?, color=?, exif_json=?, processed=1
		WHERE id=?`, w, h, takenUnix, color, string(exJSON), id); err != nil {
		return err
	}
	l.db.Exec(`UPDATE albums SET color=? WHERE id=? AND cover_photo_id=?`, color, albumID, id)

	// pre-render the viewer size in the background
	l.queue.Submit("photo-m:"+id, func() error {
		mctx, mcancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer mcancel()
		mPath := l.CachePath(id, SizeM)
		if _, err := os.Stat(mPath); err == nil {
			return nil
		}
		return l.vips.Thumbnail(mctx, src, mPath, l.sizeM)
	}, false)
	return nil
}

// CachePath returns the WebP cache file for a photo id and size.
func (l *Library) CachePath(id, size string) string {
	return filepath.Join(l.cacheDir, id[:2], id+"_"+size+".webp")
}

func (l *Library) removeCache(id string) {
	for _, s := range []string{SizeS, SizeM, SizeO} {
		os.Remove(l.CachePath(id, s))
	}
}

// Albums lists all albums for the shelf.
func (l *Library) Albums() ([]Album, error) {
	rows, err := l.db.Query(`SELECT id, title, description, date_text, cover_photo_id, photo_count, color
		FROM albums ORDER BY mtime DESC, title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Album{}
	for rows.Next() {
		var a Album
		if err := rows.Scan(&a.ID, &a.Title, &a.Description, &a.Date, &a.Cover, &a.Count, &a.Color); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Album returns one album and its photos.
func (l *Library) Album(id string) (*Album, []Photo, error) {
	a := &Album{}
	err := l.db.QueryRow(`SELECT id, title, description, date_text, cover_photo_id, photo_count, color
		FROM albums WHERE id=?`, id).Scan(&a.ID, &a.Title, &a.Description, &a.Date, &a.Cover, &a.Count, &a.Color)
	if err != nil {
		return nil, nil, err
	}
	var sortMode string
	l.db.QueryRow(`SELECT sort_mode FROM albums WHERE id=?`, id).Scan(&sortMode)
	order := "sort_key"
	if sortMode == "time" { // about.txt "sort: time": by capture time, undated last
		order = "CASE WHEN taken_at=0 THEN 1 ELSE 0 END, taken_at, sort_key"
	}
	rows, err := l.db.Query(`SELECT id, rel_path, width, height, color, taken_at, processed
		FROM photos WHERE album_id=? ORDER BY `+order, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	photos := []Photo{}
	for rows.Next() {
		var p Photo
		var rel string
		var processed int
		if err := rows.Scan(&p.ID, &rel, &p.Width, &p.Height, &p.Color, &p.Taken, &processed); err != nil {
			return nil, nil, err
		}
		p.Name = filepath.Base(rel)
		p.Ready = processed == 1
		photos = append(photos, p)
	}
	return a, photos, rows.Err()
}

// Exif returns the stored EXIF for a photo.
func (l *Library) Exif(id string) (*Exif, error) {
	var raw string
	if err := l.db.QueryRow(`SELECT exif_json FROM photos WHERE id=?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	ex := &Exif{}
	if raw != "" {
		json.Unmarshal([]byte(raw), ex)
	}
	return ex, nil
}

// AlbumOf returns the album id and title of a photo (for statistics).
func (l *Library) AlbumOf(photoID string) (string, string) {
	var aid, title string
	l.db.QueryRow(`SELECT a.id, a.title FROM photos p JOIN albums a ON a.id=p.album_id WHERE p.id=?`,
		photoID).Scan(&aid, &title)
	return aid, title
}

// Rendition returns a file path to serve for (id, size).
// It generates missing renditions with high priority and waits up to 10s.
func (l *Library) Rendition(ctx context.Context, id, size string) (string, error) {
	var rel string
	if err := l.db.QueryRow(`SELECT rel_path FROM photos WHERE id=?`, id).Scan(&rel); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fs.ErrNotExist
		}
		return "", err
	}
	src := l.roots.Abs(rel)
	native := browserNative[library.ExtOf(rel)]
	if size == SizeO && native {
		return src, nil
	}
	path := l.CachePath(id, size)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	px := l.sizeS
	switch size {
	case SizeM:
		px = l.sizeM
	case SizeO:
		px = 16384 // "--size down" keeps the original resolution
	}
	key := "photo-" + size + ":" + id
	if size == SizeS {
		key = "photo:" + id // same key as process(): avoids rendering twice
	}
	job := l.queue.Submit(key, func() error {
		if size == SizeS {
			return l.process(id)
		}
		jctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		return l.vips.Thumbnail(jctx, src, path, px)
	}, true)
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := job.Wait(wctx); err != nil {
		if native {
			return src, nil // fall back to the original file
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", ErrPending
		}
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		if native {
			return src, nil
		}
		return "", ErrPending
	}
	return path, nil
}

// Count returns album and photo totals.
func (l *Library) Count() (albums, photos int) {
	l.db.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albums)
	l.db.QueryRow(`SELECT COUNT(*) FROM photos`).Scan(&photos)
	return
}
