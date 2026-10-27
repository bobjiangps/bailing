package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LRCLIB (https://lrclib.net) is a free, open lyrics database with a public
// API that needs no key. We identify ourselves with a User-Agent as the
// project asks clients to do.

// errNotFound means the provider has no lyrics for the track.
var errNotFound = errors.New("lyrics not found")

type lrclibRecord struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`

	// Approx: same title and artist but a recording of another length
	// (radio edit, live …). Only the plain text is kept: its timestamps
	// would not fit our file.
	Approx bool `json:"-"`
}

type lrclib struct {
	base   string
	client *http.Client
	ua     string
}

func newLRCLib(base string, timeout time.Duration, version string) *lrclib {
	return &lrclib{
		base:   strings.TrimRight(base, "/"),
		client: &http.Client{Timeout: timeout},
		ua:     "bailing/" + version + " (self-hosted family music site)",
	}
}

// lookup tries the exact-match endpoint first, then falls back to search.
func (c *lrclib) lookup(ctx context.Context, title, artist, album string, durMs int64) (*lrclibRecord, error) {
	if title == "" {
		return nil, errNotFound
	}
	if artist != "" && durMs > 0 {
		q := url.Values{}
		q.Set("track_name", title)
		q.Set("artist_name", artist)
		if album != "" {
			q.Set("album_name", album)
		}
		q.Set("duration", fmt.Sprint(int(math.Round(float64(durMs)/1000))))
		var rec lrclibRecord
		err := c.get(ctx, "/api/get?"+q.Encode(), &rec)
		if err == nil && (rec.SyncedLyrics != "" || rec.PlainLyrics != "" || rec.Instrumental) {
			return &rec, nil
		}
		if err != nil && !errors.Is(err, errNotFound) {
			return nil, err
		}
	}

	q := url.Values{}
	if artist != "" {
		q.Set("track_name", title)
		q.Set("artist_name", artist)
	} else {
		q.Set("q", title)
	}
	var list []lrclibRecord
	if err := c.get(ctx, "/api/search?"+q.Encode(), &list); err != nil {
		return nil, err
	}
	if best := pickBest(list, title, durMs); best != nil {
		return best, nil
	}
	if artist != "" {
		// the artist may be spelt differently (周杰倫 / 周杰伦, "feat." …):
		// search by title only, the duration check keeps it honest
		var more []lrclibRecord
		q := url.Values{}
		q.Set("track_name", title)
		if err := c.get(ctx, "/api/search?"+q.Encode(), &more); err == nil && durMs > 0 {
			if best := pickBest(more, title, durMs); best != nil {
				return best, nil
			}
		}
	}
	// same song, other recording length: plain text is better than nothing
	return pickApprox(list, title, artist), nil
}

// pickApprox returns the plain text of an exact title + artist match whose
// duration did not fit, or nil.
func pickApprox(list []lrclibRecord, title, artist string) *lrclibRecord {
	if artist == "" {
		return nil
	}
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	var best *lrclibRecord
	for i := range list {
		r := &list[i]
		if r.Instrumental || norm(r.TrackName) != norm(title) || norm(r.ArtistName) != norm(artist) {
			continue
		}
		text := r.PlainLyrics
		if text == "" {
			text = stripTimes(r.SyncedLyrics)
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		if best == nil || len(text) > len(best.PlainLyrics) {
			best = &lrclibRecord{ID: r.ID, TrackName: r.TrackName, ArtistName: r.ArtistName,
				AlbumName: r.AlbumName, Duration: r.Duration, PlainLyrics: text, Approx: true}
		}
	}
	return best
}

// stripTimes turns LRC into plain text.
func stripTimes(lrc string) string {
	lines, _ := Parse(lrc)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// pickBest prefers synced lyrics whose duration is within 3s of ours.
func pickBest(list []lrclibRecord, title string, durMs int64) *lrclibRecord {
	var best *lrclibRecord
	bestScore := -1.0
	for i := range list {
		r := &list[i]
		if r.SyncedLyrics == "" && r.PlainLyrics == "" && !r.Instrumental {
			continue
		}
		score := 0.0
		if durMs > 0 && r.Duration > 0 {
			diff := math.Abs(r.Duration - float64(durMs)/1000)
			if diff > 5 {
				continue // almost certainly a different recording
			}
			score += 5 - diff
		}
		if r.SyncedLyrics != "" {
			score += 3
		}
		if strings.EqualFold(strings.TrimSpace(r.TrackName), strings.TrimSpace(title)) {
			score += 2
		}
		if score > bestScore {
			best, bestScore = r, score
		}
	}
	if best == nil {
		return nil
	}
	return best
}

// search runs a manual search with user-supplied keywords. Several queries
// are merged (structured, then free text) because LRCLIB's structured search
// misses entries whose artist or title is spelt a little differently.
func (c *lrclib) search(ctx context.Context, title, artist, album string) ([]lrclibRecord, error) {
	var queries []url.Values
	if title != "" {
		q := url.Values{}
		q.Set("track_name", title)
		if artist != "" {
			q.Set("artist_name", artist)
		}
		if album != "" {
			q.Set("album_name", album)
		}
		queries = append(queries, q)
		if artist != "" || album != "" {
			q = url.Values{}
			q.Set("track_name", title)
			queries = append(queries, q)
		}
	}
	free := strings.TrimSpace(title + " " + artist)
	if free != "" {
		q := url.Values{}
		q.Set("q", free)
		queries = append(queries, q)
	}

	seen := map[int64]bool{}
	var out []lrclibRecord
	var firstErr error
	for _, q := range queries {
		var list []lrclibRecord
		if err := c.get(ctx, "/api/search?"+q.Encode(), &list); err != nil {
			if !errors.Is(err, errNotFound) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, r := range list {
			if r.ID != 0 && seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			out = append(out, r)
		}
		if len(out) >= 40 {
			break
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// byID fetches one LRCLIB record (GET /api/get/{id}).
func (c *lrclib) byID(ctx context.Context, id int64) (*lrclibRecord, error) {
	var rec lrclibRecord
	if err := c.get(ctx, fmt.Sprintf("/api/get/%d", id), &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (c *lrclib) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lrclib: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
