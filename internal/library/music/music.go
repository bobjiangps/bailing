// Package music scans the music root recursively, reads tags (title,
// artist, album, embedded cover, embedded lyrics) like a car stereo does, and
// stores the result in the tracks table.
package music

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bailing/internal/imaging"
	"bailing/internal/library"
	"bailing/internal/store"

	"github.com/dhowden/tag"
)

// Track is the JSON shape returned to the client.
type Track struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Duration int64  `json:"duration_ms"`
	Cover    string `json:"cover"`
	Color    string `json:"color"`
	Format   string `json:"format"`
	Folder   string `json:"folder"`
	TrackNo  int    `json:"track_no,omitempty"`
	Year     int    `json:"year,omitempty"`
}

// Group is an entry of the folder / artist / album browse lists.
type Group struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Cover string `json:"cover"`
}

// Library is the music library service.
type Library struct {
	db       *store.DB
	roots    library.Roots
	cacheDir string
	exts     []string
	vips     *imaging.Vips
	queue    *imaging.Queue
	log      *slog.Logger

	scanMu   sync.Mutex
	scanning bool

	// naming order learnt during one scan (only touched by the scan goroutine):
	// folder -> +1 "Title - Artist", -1 "Artist - Title", 0 unknown
	folderOrder map[string]int
	globalOrder *int
}

// New creates the music library over one or more directories.
func New(db *store.DB, roots []string, cacheDir string, exts []string, v *imaging.Vips,
	q *imaging.Queue, log *slog.Logger) *Library {
	return &Library{db: db, roots: roots, cacheDir: cacheDir, exts: exts, vips: v, queue: q, log: log}
}

// Roots returns the music directories.
func (l *Library) Roots() []string { return l.roots }

// Scanning reports whether a scan is running.
func (l *Library) Scanning() bool {
	l.scanMu.Lock()
	defer l.scanMu.Unlock()
	return l.scanning
}

type existingRow struct {
	size, mtime int64
	lrc         string
}

// Scan walks the music directories and syncs the tracks table.
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
	l.folderOrder = map[string]int{}
	l.globalOrder = nil

	existing := map[string]existingRow{}
	rows, err := l.db.Query(`SELECT id, size, mtime, lrc_path FROM tracks`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var r existingRow
		rows.Scan(&id, &r.size, &r.mtime, &r.lrc)
		existing[id] = r
	}
	rows.Close()

	seen := map[string]bool{}
	added, updated, skipped := 0, 0, 0
	missing := false
	for i, root := range l.roots {
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			// e.g. an unmounted disk: keep its tracks instead of dropping them
			l.log.Warn("music directory not available", "dir", root, "err", err)
			missing = true
			continue
		}
		prefix := l.roots.Prefix(i)
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable entry: skip
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			name := d.Name()
			if d.IsDir() {
				if path != root && library.Hidden(name) {
					return filepath.SkipDir
				}
				return nil
			}
			if library.Hidden(name) {
				return nil
			}
			if !library.HasExt(name, l.exts) {
				if e := library.ExtOf(name); e == "ape" || e == "wma" || e == "dsf" {
					skipped++
				}
				return nil
			}
			inner, _ := filepath.Rel(root, path)
			inner = filepath.ToSlash(inner)
			rel := prefix + inner
			id := library.ID(rel)
			seen[id] = true
			info, err := d.Info()
			if err != nil {
				return nil
			}
			lrc := findLRC(path)
			lrcRel := ""
			if lrc != "" {
				r, _ := filepath.Rel(root, lrc)
				lrcRel = prefix + filepath.ToSlash(r)
			}
			old, ok := existing[id]
			if ok && old.size == info.Size() && old.mtime == info.ModTime().Unix() {
				if old.lrc != lrcRel {
					l.db.Exec(`UPDATE tracks SET lrc_path=? WHERE id=?`, lrcRel, id)
				}
				return nil
			}
			if err := l.indexFile(id, rel, l.folderOf(i, inner), path, lrcRel, info); err != nil {
				l.log.Warn("index track", "file", rel, "err", err)
				return nil
			}
			if ok {
				updated++
			} else {
				added++
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	removed := 0
	for id := range existing {
		if !seen[id] && !missing {
			l.db.Exec(`DELETE FROM tracks WHERE id=?`, id)
			l.db.Exec(`DELETE FROM lyrics_cache WHERE track_id=?`, id)
			removed++
		}
	}
	l.log.Info("music scan done", "dirs", len(l.roots), "tracks", len(seen), "added", added, "updated", updated,
		"removed", removed, "unsupported_skipped", skipped, "took", time.Since(start).Round(time.Millisecond))
	return nil
}

// folderOf is the folder shown in the "folders" tab. Folders of the first
// directory look as before; those of further directories start with the
// directory's name, e.g. "nas-music/Jazz".
func (l *Library) folderOf(root int, inner string) string {
	dir := path.Dir(inner)
	if root == 0 {
		return dir
	}
	if dir == "." {
		return l.roots.Label(root)
	}
	return l.roots.Label(root) + "/" + dir
}

func (l *Library) indexFile(id, rel, folder, path, lrcRel string, info fs.FileInfo) error {
	ext := library.ExtOf(path)
	t := Track{ID: id, Format: ext, Folder: folder}
	var albumArtist, genre string
	var discNo int
	hasLyrics := false

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	md, terr := tag.ReadFrom(f)
	f.Close()
	if terr == nil {
		t.Title = strings.TrimSpace(md.Title())
		t.Artist = strings.TrimSpace(md.Artist())
		t.Album = strings.TrimSpace(md.Album())
		albumArtist = strings.TrimSpace(md.AlbumArtist())
		genre = strings.TrimSpace(md.Genre())
		t.Year = md.Year()
		t.TrackNo, _ = md.Track()
		discNo, _ = md.Disc()
		hasLyrics = strings.TrimSpace(md.Lyrics()) != ""
		if pic := md.Picture(); pic != nil && len(pic.Data) > 0 {
			t.Cover, t.Color = l.storeCover(pic.Data, pic.Ext)
		}
	}
	guessed := false
	if t.Title == "" {
		guessed = true
		t.Title = guessTitle(filepath.Base(path), &t.Artist, l.nameOrder(filepath.Dir(path)) > 0)
	}
	if t.Cover == "" {
		if img := findFolderImage(path); img != "" {
			if b, err := os.ReadFile(img); err == nil {
				t.Cover, t.Color = l.storeCover(b, library.ExtOf(img))
			}
		}
	}
	t.Duration = Duration(path, ext)

	_, err = l.db.Exec(`INSERT INTO tracks(id, rel_path, folder, title, artist, album, album_artist,
		track_no, disc_no, year, genre, duration_ms, format, size, mtime, cover_key, color, lrc_path,
		has_embedded_lyrics, name_guessed, added_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET folder=excluded.folder, title=excluded.title, artist=excluded.artist,
		album=excluded.album, album_artist=excluded.album_artist, track_no=excluded.track_no,
		disc_no=excluded.disc_no, year=excluded.year, genre=excluded.genre, duration_ms=excluded.duration_ms,
		format=excluded.format, size=excluded.size, mtime=excluded.mtime, cover_key=excluded.cover_key,
		color=excluded.color, lrc_path=excluded.lrc_path, has_embedded_lyrics=excluded.has_embedded_lyrics,
		name_guessed=excluded.name_guessed`,
		id, rel, t.Folder, t.Title, t.Artist, t.Album, albumArtist, t.TrackNo, discNo, t.Year, genre,
		t.Duration, ext, info.Size(), info.ModTime().Unix(), t.Cover, t.Color, lrcRel, store.Bool(hasLyrics),
		store.Bool(guessed), time.Now().Unix())
	if err == nil {
		// a changed file may have new tags: drop previously downloaded lyrics
		l.db.Exec(`DELETE FROM lyrics_cache WHERE track_id=? AND status<>'found'`, id)
	}
	return err
}

// nameParts splits "01 - A - B.mp3" into ["A", "B"] (track number dropped).
func nameParts(base string) []string {
	name := strings.TrimSuffix(base, filepath.Ext(base))
	parts := strings.Split(name, " - ")
	// drop a leading track number "01"
	if len(parts) > 1 && strings.Trim(parts[0], "0123456789. ") == "" {
		parts = parts[1:]
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// guessTitle turns "01 - Artist - Title.mp3" (or "Title - Artist.mp3" when
// titleFirst) into a title, and fills the artist if it is empty.
func guessTitle(base string, artist *string, titleFirst bool) string {
	parts := nameParts(base)
	if len(parts) >= 2 {
		if titleFirst {
			last := len(parts) - 1
			if *artist == "" {
				*artist = parts[last]
			}
			return strings.Join(parts[:last], " - ")
		}
		if *artist == "" {
			*artist = parts[0]
		}
		return strings.Join(parts[1:], " - ")
	}
	name := strings.TrimSuffix(base, filepath.Ext(base))
	return strings.TrimSpace(strings.TrimLeft(name, "0123456789. -_"))
}

// orderVote compares a tagged file's name with its tags:
// +1 for "Title - Artist", -1 for "Artist - Title", 0 if it tells nothing.
func orderVote(base, title, artist string) int {
	p := nameParts(base)
	if len(p) != 2 || title == "" || artist == "" {
		return 0
	}
	eq := strings.EqualFold
	switch {
	case eq(p[0], title) && eq(p[1], artist):
		return 1
	case eq(p[0], artist) && eq(p[1], title):
		return -1
	}
	return 0
}

// nameOrder tells how untagged files in dir are probably named, learnt from
// the tagged files next to them, else from the whole library. Different
// download sites use different orders, so this beats a fixed rule.
func (l *Library) nameOrder(dir string) int {
	if l.folderOrder == nil {
		l.folderOrder = map[string]int{}
	}
	if v, ok := l.folderOrder[dir]; ok {
		return v
	}
	votes, read := 0, 0
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if read >= 30 {
				break
			}
			if e.IsDir() || library.Hidden(e.Name()) || !library.HasExt(e.Name(), l.exts) {
				continue
			}
			if len(nameParts(e.Name())) != 2 {
				continue
			}
			f, err := os.Open(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			md, err := tag.ReadFrom(f)
			f.Close()
			if err != nil {
				continue
			}
			read++
			votes += orderVote(e.Name(), strings.TrimSpace(md.Title()), strings.TrimSpace(md.Artist()))
		}
	}
	v := sign(votes)
	if v == 0 {
		v = l.libraryOrder()
	}
	l.folderOrder[dir] = v
	return v
}

// libraryOrder votes over tracks already indexed from their tags.
func (l *Library) libraryOrder() int {
	if l.globalOrder != nil {
		return *l.globalOrder
	}
	votes := 0
	rows, err := l.db.Query(`SELECT rel_path, title, artist FROM tracks
		WHERE name_guessed = 0 AND mtime > 0 AND artist <> '' LIMIT 2000`)
	if err == nil {
		for rows.Next() {
			var rel, title, artist string
			if rows.Scan(&rel, &title, &artist) == nil {
				votes += orderVote(filepath.Base(rel), title, artist)
			}
		}
		rows.Close()
	}
	v := sign(votes)
	l.globalOrder = &v
	return v
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// storeCover saves the original cover bytes once (content-addressed) and
// returns its key and dominant colour.
func (l *Library) storeCover(data []byte, ext string) (string, string) {
	sum := sha1.Sum(data)
	key := hex.EncodeToString(sum[:])[:16]
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if ext == "" || ext == "jpeg" {
		ext = "jpg"
	}
	orig := l.coverOriginal(key)
	if orig == "" {
		orig = filepath.Join(l.cacheDir, key[:2], key+"."+ext)
		os.MkdirAll(filepath.Dir(orig), 0o755)
		if err := os.WriteFile(orig, data, 0o644); err != nil {
			return "", ""
		}
	}
	color, _ := imaging.DominantColorFile(orig)
	return key, color
}

func (l *Library) coverOriginal(key string) string {
	if len(key) < 2 {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(l.cacheDir, key[:2], key+".*"))
	for _, m := range matches {
		if !strings.Contains(filepath.Base(m), "_") { // skip renditions key_s.webp
			return m
		}
	}
	return ""
}

// CoverPath returns a cover rendition ("s" ~300px, "m" ~800px, "o" original).
func (l *Library) CoverPath(ctx context.Context, key, size string) (string, error) {
	for _, c := range key {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return "", fs.ErrNotExist
		}
	}
	orig := l.coverOriginal(key)
	if orig == "" {
		return "", fs.ErrNotExist
	}
	px := 0
	switch size {
	case "s":
		px = 300
	case "m":
		px = 800
	default:
		return orig, nil
	}
	out := filepath.Join(l.cacheDir, key[:2], fmt.Sprintf("%s_%s.webp", key, size))
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	job := l.queue.Submit("cover-"+size+":"+key, func() error {
		jctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return l.vips.Thumbnail(jctx, orig, out, px)
	}, true)
	wctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := job.Wait(wctx); err != nil {
		return orig, nil // vips missing or slow: serve the original
	}
	return out, nil
}

// findLRC looks for "<name>.lrc" next to the file or in a Lyrics/ sub folder.
func findLRC(audio string) string {
	dir := filepath.Dir(audio)
	base := strings.TrimSuffix(filepath.Base(audio), filepath.Ext(audio))
	for _, d := range []string{dir, filepath.Join(dir, "Lyrics"), filepath.Join(dir, "lyrics")} {
		for _, ext := range []string{".lrc", ".LRC", ".Lrc"} {
			p := filepath.Join(d, base+ext)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// findFolderImage finds cover.jpg / folder.jpg / <same name>.jpg etc.
func findFolderImage(audio string) string {
	dir := filepath.Dir(audio)
	base := strings.TrimSuffix(filepath.Base(audio), filepath.Ext(audio))
	names := []string{base, "cover", "Cover", "folder", "Folder", "front", "Front", "AlbumArt"}
	for _, n := range names {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp", ".JPG", ".PNG"} {
			p := filepath.Join(dir, n+ext)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// ---- queries ----------------------------------------------------------------

// Query filters the track list. Nil filters are off; a filter pointing to
// "" selects tracks without that tag (the "unknown artist" group).
type Query struct {
	Q, Sort               string
	Desc                  bool // reverse the sort
	Folder, Artist, Album *string
	Offset, Limit         int
}

// sortOrder builds the ORDER BY clause for Query.Sort:
//
//	""         folder, disc, track number (the order on disk)
//	title      song name           artist / album: grouped, then track order
//	added      recently added      mtime: file modification time
//	duration   song length
//
// added / mtime are newest first; desc reverses only the first key, the
// tie-breakers keep albums in track order.
func sortOrder(sort string, desc bool) string {
	dir := " ASC"
	if desc {
		dir = " DESC"
	}
	switch sort {
	case "title":
		return "title COLLATE NOCASE" + dir + ", artist COLLATE NOCASE"
	case "artist":
		return "artist COLLATE NOCASE" + dir + ", album COLLATE NOCASE, disc_no, track_no, title COLLATE NOCASE"
	case "album":
		return "album COLLATE NOCASE" + dir + ", disc_no, track_no, title COLLATE NOCASE"
	case "added", "recent":
		return "added_at" + dir + ", rel_path"
	case "mtime":
		return "mtime" + dir + ", rel_path"
	case "duration":
		return "duration_ms" + dir + ", title COLLATE NOCASE"
	}
	if desc {
		return "folder DESC, disc_no DESC, track_no DESC, rel_path DESC"
	}
	return "folder, disc_no, track_no, rel_path"
}

const trackCols = `id, title, artist, album, duration_ms, cover_key, color, format, folder, track_no, year`

func scanTrack(rs interface{ Scan(...any) error }) (Track, error) {
	var t Track
	err := rs.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Duration, &t.Cover, &t.Color,
		&t.Format, &t.Folder, &t.TrackNo, &t.Year)
	return t, err
}

// Tracks returns a page of tracks plus the total count.
func (l *Library) Tracks(q Query) ([]Track, int, error) {
	where := []string{"1=1"}
	var args []any
	if q.Q != "" {
		like := "%" + strings.ToLower(q.Q) + "%"
		where = append(where, "(lower(title) LIKE ? OR lower(artist) LIKE ? OR lower(album) LIKE ? OR lower(rel_path) LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if q.Folder != nil {
		where = append(where, "folder = ?")
		args = append(args, *q.Folder)
	}
	if q.Artist != nil {
		where = append(where, "artist = ?")
		args = append(args, *q.Artist)
	}
	if q.Album != nil {
		where = append(where, "album = ?")
		args = append(args, *q.Album)
	}
	order := sortOrder(q.Sort, q.Desc)
	if q.Limit <= 0 || q.Limit > 5000 {
		q.Limit = 500
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := l.db.QueryRow(`SELECT COUNT(*) FROM tracks WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := l.db.Query(`SELECT `+trackCols+` FROM tracks WHERE `+w+` ORDER BY `+order+
		` LIMIT ? OFFSET ?`, append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Track{}
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// Groups lists folders / artists / albums with counts.
func (l *Library) Groups(kind string) ([]Group, error) {
	col := map[string]string{"folders": "folder", "artists": "artist", "albums": "album"}[kind]
	if col == "" {
		return nil, fmt.Errorf("unknown group %q", kind)
	}
	rows, err := l.db.Query(`SELECT ` + col + `, COUNT(*), MAX(cover_key) FROM tracks
		GROUP BY ` + col + ` ORDER BY ` + col + ` COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Name, &g.Count, &g.Cover); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// TrackInfo is the full row used by lyrics and streaming.
type TrackInfo struct {
	Track
	Path      string // absolute path of the audio file
	LRCPath   string // absolute path of the sidecar .lrc ("" if none)
	HasLyrics bool   // embedded lyrics present
}

// Get returns one track.
func (l *Library) Get(id string) (*TrackInfo, error) {
	var ti TrackInfo
	var rel, lrc string
	err := l.db.QueryRow(`SELECT `+trackCols+`, rel_path, lrc_path, has_embedded_lyrics FROM tracks WHERE id=?`, id).
		Scan(&ti.ID, &ti.Title, &ti.Artist, &ti.Album, &ti.Duration, &ti.Cover, &ti.Color, &ti.Format,
			&ti.Folder, &ti.TrackNo, &ti.Year, &rel, &lrc, &ti.HasLyrics)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fs.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	ti.Path = l.roots.Abs(rel)
	if lrc != "" {
		ti.LRCPath = l.roots.Abs(lrc)
	}
	return &ti, nil
}

// SetDuration stores a duration reported by the browser when the header
// parser could not determine it.
func (l *Library) SetDuration(id string, ms int64) {
	l.db.Exec(`UPDATE tracks SET duration_ms=? WHERE id=? AND duration_ms=0`, ms, id)
}

// EmbeddedLyrics reads the lyrics tag from the audio file.
func EmbeddedLyrics(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	md, err := tag.ReadFrom(f)
	if err != nil {
		return ""
	}
	return md.Lyrics()
}

// Count returns the number of tracks.
func (l *Library) Count() (n int) {
	l.db.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&n)
	return
}
