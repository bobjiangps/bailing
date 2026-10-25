package lyrics

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"bailing/internal/library"
	"bailing/internal/library/music"
	"bailing/internal/store"
)

// Result is returned by GET /api/music/tracks/{id}/lyrics.
type Result struct {
	// Status: ok | none | instrumental
	Status string `json:"status"`
	// Source: lrc | embedded | lrclib | ""
	Source string `json:"source"`
	Synced bool   `json:"synced"`
	Lines  []Line `json:"lines"`
}

// Options configures the lyrics service.
type Options struct {
	Online          bool
	LRCLibBase      string
	NegativeTTL     time.Duration
	Timeout         time.Duration
	SaveBesideMusic bool
	Version         string
}

// Service resolves lyrics in priority order:
// sidecar .lrc → embedded tag → downloaded cache → LRCLIB → none.
type Service struct {
	db    *store.DB
	music *music.Library
	opt   Options
	lib   *lrclib
	log   *slog.Logger

	mu       sync.Mutex
	inflight map[string]*call
	warnOnce sync.Once
}

type call struct {
	done chan struct{}
	res  *Result
}

// New creates the service.
func New(db *store.DB, m *music.Library, opt Options, log *slog.Logger) *Service {
	if opt.NegativeTTL <= 0 {
		opt.NegativeTTL = 7 * 24 * time.Hour
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 8 * time.Second
	}
	return &Service{db: db, music: m, opt: opt, log: log,
		lib:      newLRCLib(opt.LRCLibBase, opt.Timeout, opt.Version),
		inflight: map[string]*call{}}
}

// ForTrack returns lyrics for a local track id.
func (s *Service) ForTrack(ctx context.Context, id string) (*Result, error) {
	t, err := s.music.Get(id)
	if err != nil {
		return nil, err
	}

	// 1. sidecar .lrc
	if t.LRCPath != "" {
		if b, err := os.ReadFile(t.LRCPath); err == nil {
			if r := build(library.DecodeText(b), "lrc"); r != nil {
				return r, nil
			}
		}
	}
	// 2. embedded lyrics tag
	if t.HasLyrics {
		if r := build(music.EmbeddedLyrics(t.Path), "embedded"); r != nil {
			return r, nil
		}
	}
	// 3. cache
	var status, source, body string
	var fetched int64
	err = s.db.QueryRow(`SELECT status, source, body, fetched_at FROM lyrics_cache WHERE track_id=?`, id).
		Scan(&status, &source, &body, &fetched)
	if err == nil {
		switch status {
		case "found":
			if r := build(body, source); r != nil {
				return r, nil
			}
		case "instrumental":
			return &Result{Status: "instrumental", Source: source, Lines: []Line{}}, nil
		case "none":
			if time.Since(time.Unix(fetched, 0)) < s.opt.NegativeTTL || !s.opt.Online {
				return none(), nil
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// 4. online
	if !s.opt.Online {
		return none(), nil
	}
	return s.fetchOnce(ctx, t), nil
}

// fetchOnce de-duplicates concurrent downloads for the same track.
func (s *Service) fetchOnce(ctx context.Context, t *music.TrackInfo) *Result {
	s.mu.Lock()
	if c, ok := s.inflight[t.ID]; ok {
		s.mu.Unlock()
		select {
		case <-c.done:
			return c.res
		case <-ctx.Done():
			return none()
		}
	}
	c := &call{done: make(chan struct{})}
	s.inflight[t.ID] = c
	s.mu.Unlock()

	c.res = s.fetch(t)

	s.mu.Lock()
	delete(s.inflight, t.ID)
	s.mu.Unlock()
	close(c.done)
	return c.res
}

func (s *Service) fetch(t *music.TrackInfo) *Result {
	// detached from the request: a closed browser tab should not waste the lookup
	ctx, cancel := context.WithTimeout(context.Background(), s.opt.Timeout*2)
	defer cancel()
	rec, err := s.lib.lookup(ctx, t.Title, t.Artist, t.Album, t.Duration)
	now := time.Now().Unix()
	if err != nil && !errors.Is(err, errNotFound) {
		// network problem: do not cache, try again next time
		s.log.Warn("lrclib lookup", "title", t.Title, "err", err)
		return none()
	}
	if rec == nil {
		s.db.Exec(`INSERT OR REPLACE INTO lyrics_cache(track_id, status, source, body, fetched_at)
			VALUES(?, 'none', 'lrclib', '', ?)`, t.ID, now)
		return none()
	}
	if rec.Instrumental && rec.SyncedLyrics == "" && rec.PlainLyrics == "" {
		s.db.Exec(`INSERT OR REPLACE INTO lyrics_cache(track_id, status, source, body, fetched_at)
			VALUES(?, 'instrumental', 'lrclib', '', ?)`, t.ID, now)
		return &Result{Status: "instrumental", Source: "lrclib", Lines: []Line{}}
	}
	body := rec.SyncedLyrics
	if body == "" {
		body = rec.PlainLyrics
	}
	s.db.Exec(`INSERT OR REPLACE INTO lyrics_cache(track_id, status, source, body, fetched_at)
		VALUES(?, 'found', 'lrclib', ?, ?)`, t.ID, body, now)
	if s.opt.SaveBesideMusic && !rec.Approx {
		// synced if available, otherwise plain text (still a valid .lrc for us)
		p := strings.TrimSuffix(t.Path, fileExt(t.Path)) + ".lrc"
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				s.log.Warn("cannot save .lrc beside music (only cached in DB)", "path", p, "err", err)
			} else {
				s.log.Info("lyrics saved", "path", p)
			}
		}
	}
	s.log.Info("lyrics downloaded", "title", t.Title, "artist", t.Artist, "synced", rec.SyncedLyrics != "",
		"approx", rec.Approx)
	if r := build(body, "lrclib"); r != nil {
		return r
	}
	return none()
}

func fileExt(p string) string {
	if i := strings.LastIndex(p, "."); i > strings.LastIndexAny(p, `/\`) {
		return p[i:]
	}
	return ""
}

func build(text, source string) *Result {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines, synced := Parse(text)
	if len(lines) == 0 {
		return nil
	}
	return &Result{Status: "ok", Source: source, Synced: synced, Lines: lines}
}

func none() *Result { return &Result{Status: "none", Lines: []Line{}} }

type memEntry struct {
	at  time.Time
	res *Result
}

var (
	memMu    sync.Mutex
	memCache = map[string]memEntry{}
)

// Lookup finds lyrics for an online track (no local file) on LRCLIB.
// Results, including "not found", are kept in memory for 6 hours.
func (s *Service) Lookup(ctx context.Context, title, artist, album string, durMs int64) *Result {
	title = strings.TrimSpace(title)
	if title == "" || !s.opt.Online {
		return none()
	}
	key := strings.ToLower(title + "\x00" + artist + "\x00" + album)
	memMu.Lock()
	if e, ok := memCache[key]; ok && time.Since(e.at) < 6*time.Hour {
		memMu.Unlock()
		return e.res
	}
	memMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, s.opt.Timeout*2)
	defer cancel()
	rec, err := s.lib.lookup(ctx, title, firstArtist(artist), album, durMs)
	if err != nil && !errors.Is(err, errNotFound) {
		return none() // network problem: do not cache
	}
	res := none()
	if rec != nil {
		if rec.Instrumental && rec.SyncedLyrics == "" && rec.PlainLyrics == "" {
			res = &Result{Status: "instrumental", Source: "lrclib", Lines: []Line{}}
		} else {
			body := rec.SyncedLyrics
			if body == "" {
				body = rec.PlainLyrics
			}
			if r := build(body, "lrclib"); r != nil {
				res = r
			}
		}
	}
	memMu.Lock()
	if len(memCache) > 1000 {
		memCache = map[string]memEntry{}
	}
	memCache[key] = memEntry{at: time.Now(), res: res}
	memMu.Unlock()
	return res
}

// firstArtist keeps the first of "A、B" / "A / B" / "A, B" style lists,
// which matches LRCLIB entries far more often.
func firstArtist(a string) string {
	for _, sep := range []string{"、", " / ", "/", ", ", ";", "&"} {
		if i := strings.Index(a, sep); i > 0 {
			return strings.TrimSpace(a[:i])
		}
	}
	return strings.TrimSpace(a)
}

// ClearCache forgets downloaded lyrics for a track (e.g. wrong match).
func (s *Service) ClearCache(id string) {
	s.db.Exec(`DELETE FROM lyrics_cache WHERE track_id=?`, id)
}
