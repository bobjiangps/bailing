package lyrics

import (
	"context"
	"errors"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Manual lyrics search: when the automatic match fails (badly named files,
// tags in another script …) the user types title / artist themselves, picks
// one of the LRCLIB results, and that choice wins over everything else.

// ErrOffline is returned when online lyrics are disabled in the config.
var ErrOffline = errors.New("online lyrics are disabled")

// ErrNotFound is returned when the chosen LRCLIB record no longer exists.
var ErrNotFound = errNotFound

// Candidate is one manual search result.
type Candidate struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Album        string `json:"album"`
	DurationMs   int64  `json:"duration_ms"`
	Synced       bool   `json:"synced"`
	Instrumental bool   `json:"instrumental"`
	Preview      string `json:"preview"`
}

const maxCandidates = 30

// Search looks up LRCLIB with user-supplied keywords. When durMs is known,
// results closest in length come first (they are most likely the same
// recording, so the timestamps fit).
func (s *Service) Search(ctx context.Context, title, artist, album string, durMs int64) ([]Candidate, error) {
	if !s.opt.Online {
		return nil, ErrOffline
	}
	title, artist, album = strings.TrimSpace(title), strings.TrimSpace(artist), strings.TrimSpace(album)
	if title == "" && artist == "" {
		return []Candidate{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.opt.Timeout*2)
	defer cancel()
	list, err := s.lib.search(ctx, title, artist, album)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(list))
	for _, r := range list {
		if r.ID == 0 || (r.SyncedLyrics == "" && r.PlainLyrics == "" && !r.Instrumental) {
			continue
		}
		out = append(out, Candidate{
			ID: r.ID, Title: r.TrackName, Artist: r.ArtistName, Album: r.AlbumName,
			DurationMs: int64(math.Round(r.Duration * 1000)), Synced: r.SyncedLyrics != "",
			Instrumental: r.Instrumental && r.SyncedLyrics == "" && r.PlainLyrics == "",
			Preview:      preview(r),
		})
	}
	sortCandidates(out, durMs)
	if len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	return out, nil
}

// sortCandidates: same length (±5s) first, closest first, synced before plain.
func sortCandidates(c []Candidate, durMs int64) {
	diff := func(x Candidate) float64 {
		if durMs <= 0 || x.DurationMs <= 0 {
			return math.MaxFloat64 / 2
		}
		return math.Abs(float64(x.DurationMs - durMs))
	}
	sort.SliceStable(c, func(i, j int) bool {
		di, dj := diff(c[i]), diff(c[j])
		ci, cj := di <= 5000, dj <= 5000
		if ci != cj {
			return ci
		}
		if c[i].Synced != c[j].Synced {
			return c[i].Synced
		}
		if ci {
			return di < dj
		}
		return false
	})
}

// preview: the first two non-empty lines, plain text.
func preview(r lrclibRecord) string {
	text := r.PlainLyrics
	if text == "" {
		text = stripTimes(r.SyncedLyrics)
	}
	var parts []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if utf8.RuneCountInString(l) > 40 {
			l = string([]rune(l)[:40]) + "…"
		}
		parts = append(parts, l)
		if len(parts) == 2 {
			break
		}
	}
	return strings.Join(parts, " / ")
}

// fetchPick downloads the chosen record and turns it into (status, body).
func (s *Service) fetchPick(ctx context.Context, lrclibID int64) (status, body string, err error) {
	if !s.opt.Online {
		return "", "", ErrOffline
	}
	ctx, cancel := context.WithTimeout(ctx, s.opt.Timeout*2)
	defer cancel()
	rec, err := s.lib.byID(ctx, lrclibID)
	if err != nil {
		return "", "", err
	}
	body = rec.SyncedLyrics
	if body == "" {
		body = rec.PlainLyrics
	}
	if strings.TrimSpace(body) == "" {
		if rec.Instrumental {
			return "instrumental", "", nil
		}
		return "", "", errNotFound
	}
	return "found", body, nil
}

func resultOf(status, body string) *Result {
	if status == "instrumental" {
		return &Result{Status: "instrumental", Source: "manual", Lines: []Line{}}
	}
	if r := build(body, "manual"); r != nil {
		return r
	}
	return none()
}

// PickForTrack stores the chosen LRCLIB record for a local track. It is
// kept in lyrics_cache with source "manual", which ForTrack checks before
// the sidecar .lrc and the embedded tag. With save_beside_music the lyrics
// are also written next to the audio file (an existing .lrc is kept as
// .lrc.bak first), so other players see them too.
func (s *Service) PickForTrack(ctx context.Context, id string, lrclibID int64) (*Result, error) {
	t, err := s.music.Get(id)
	if err != nil {
		return nil, err
	}
	status, body, err := s.fetchPick(ctx, lrclibID)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO lyrics_cache(track_id, status, source, body, fetched_at)
		VALUES(?, ?, 'manual', ?, ?)`, id, status, body, time.Now().Unix()); err != nil {
		return nil, err
	}
	if status == "found" && s.opt.SaveBesideMusic {
		p := t.LRCPath
		if p == "" {
			p = strings.TrimSuffix(t.Path, fileExt(t.Path)) + ".lrc"
		}
		if old, err := os.ReadFile(p); err == nil && string(old) != body {
			_ = os.WriteFile(p+".bak", old, 0o644)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			s.log.Warn("cannot save .lrc beside music (only kept in DB)", "path", p, "err", err)
		} else {
			s.log.Info("lyrics saved (manual pick)", "path", p)
		}
	}
	s.log.Info("lyrics picked by hand", "track", id, "lrclib_id", lrclibID)
	return resultOf(status, body), nil
}

// onlineKey identifies an online song (it has no row in tracks).
func onlineKey(title, artist, album string) string {
	n := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	return n(title) + "\x00" + n(artist) + "\x00" + n(album)
}

// PickOnline stores the chosen LRCLIB record for an online song (LX source,
// links) under its title / artist / album, in table lyrics_override.
func (s *Service) PickOnline(ctx context.Context, title, artist, album string, lrclibID int64) (*Result, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errNotFound
	}
	status, body, err := s.fetchPick(ctx, lrclibID)
	if err != nil {
		return nil, err
	}
	key := onlineKey(title, artist, album)
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO lyrics_override(key, status, body, lrclib_id, updated_at)
		VALUES(?, ?, ?, ?, ?)`, key, status, body, lrclibID, time.Now().Unix()); err != nil {
		return nil, err
	}
	memMu.Lock()
	delete(memCache, key)
	memMu.Unlock()
	return resultOf(status, body), nil
}

// ClearOnline forgets a manual pick (and the memory cache) for an online song.
func (s *Service) ClearOnline(title, artist, album string) {
	key := onlineKey(title, artist, album)
	s.db.Exec(`DELETE FROM lyrics_override WHERE key=?`, key)
	memMu.Lock()
	delete(memCache, key)
	memMu.Unlock()
}

// override returns a manual pick for an online song, or nil.
func (s *Service) override(key string) *Result {
	var status, body string
	if err := s.db.QueryRow(`SELECT status, body FROM lyrics_override WHERE key=?`, key).
		Scan(&status, &body); err != nil {
		return nil
	}
	return resultOf(status, body)
}
