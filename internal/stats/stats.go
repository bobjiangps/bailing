// Package stats records play / view events and answers the "recent" and
// "most" queries shown on the home page.
package stats

import (
	"time"

	"bailing/internal/store"
)

// Service records and queries statistics.
type Service struct{ db *store.DB }

// New creates the service.
func New(db *store.DB) *Service { return &Service{db: db} }

// Play is one counted playback.
type Play struct {
	Source     string `json:"source"` // local | direct | radio | lx:kw ...
	Ref        string `json:"ref"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Cover      string `json:"cover"`
	ListenedMs int64  `json:"listened_ms"`
}

// RecordPlay stores a play event.
func (s *Service) RecordPlay(userID int64, p Play) error {
	_, err := s.db.Exec(`INSERT INTO play_events(user_id, source, ref, title, artist, cover, listened_ms, at)
		VALUES(?,?,?,?,?,?,?,?)`, userID, p.Source, p.Ref, trim(p.Title, 200), trim(p.Artist, 200),
		trim(p.Cover, 500), p.ListenedMs, time.Now().Unix())
	return err
}

// RecordView stores an album / photo view.
func (s *Service) RecordView(userID int64, kind, ref string) error {
	_, err := s.db.Exec(`INSERT INTO view_events(user_id, kind, ref, at) VALUES(?,?,?,?)`,
		userID, kind, ref, time.Now().Unix())
	return err
}

// Item is one entry in a summary list.
type Item struct {
	Kind   string `json:"kind"` // track | album | photo
	Source string `json:"source,omitempty"`
	Ref    string `json:"ref"`
	Title  string `json:"title"`
	Sub    string `json:"sub,omitempty"`
	Cover  string `json:"cover,omitempty"` // cover key (track) / photo id (album, photo)
	Album  string `json:"album,omitempty"` // album id for photo items
	Count  int    `json:"count,omitempty"`
	At     int64  `json:"at,omitempty"`
}

// Summary groups the four home page lists.
type Summary struct {
	RecentPlays []Item `json:"recent_plays"`
	TopPlays    []Item `json:"top_plays"`
	RecentViews []Item `json:"recent_views"`
	TopViews    []Item `json:"top_views"`
}

// Summary returns the per-user lists (limit items each).
func (s *Service) Summary(userID int64, limit int) (*Summary, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	out := &Summary{RecentPlays: []Item{}, TopPlays: []Item{}, RecentViews: []Item{}, TopViews: []Item{}}

	rows, err := s.db.Query(`SELECT source, ref, title, artist, cover, MAX(at) AS last
		FROM play_events WHERE user_id=? GROUP BY source, ref ORDER BY last DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		it := Item{Kind: "track"}
		rows.Scan(&it.Source, &it.Ref, &it.Title, &it.Sub, &it.Cover, &it.At)
		out.RecentPlays = append(out.RecentPlays, it)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT source, ref, title, artist, cover, COUNT(*) AS n
		FROM play_events WHERE user_id=? GROUP BY source, ref ORDER BY n DESC, MAX(at) DESC LIMIT ?`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		it := Item{Kind: "track"}
		rows.Scan(&it.Source, &it.Ref, &it.Title, &it.Sub, &it.Cover, &it.Count)
		out.TopPlays = append(out.TopPlays, it)
	}
	rows.Close()

	// views: album titles / photo names are joined live so renames show up
	viewSQL := func(order string) string {
		return `SELECT v.kind, v.ref, MAX(v.at) AS last, COUNT(*) AS n,
			COALESCE(a.title, pa.title, ''), COALESCE(p.rel_path, ''),
			COALESCE(a.cover_photo_id, v.ref), COALESCE(a.id, p.album_id, '')
			FROM view_events v
			LEFT JOIN albums a ON v.kind='album' AND a.id=v.ref
			LEFT JOIN photos p ON v.kind='photo' AND p.id=v.ref
			LEFT JOIN albums pa ON pa.id=p.album_id
			WHERE v.user_id=? GROUP BY v.kind, v.ref
			HAVING (a.id IS NOT NULL OR p.id IS NOT NULL)
			ORDER BY ` + order + ` LIMIT ?`
	}
	for _, q := range []struct {
		order string
		dst   *[]Item
	}{{"last DESC", &out.RecentViews}, {"n DESC, last DESC", &out.TopViews}} {
		rows, err = s.db.Query(viewSQL(q.order), userID, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var it Item
			var albumTitle, rel string
			rows.Scan(&it.Kind, &it.Ref, &it.At, &it.Count, &albumTitle, &rel, &it.Cover, &it.Album)
			if it.Kind == "photo" {
				it.Title = baseName(rel)
				it.Sub = albumTitle
			} else {
				it.Title = albumTitle
			}
			*q.dst = append(*q.dst, it)
		}
		rows.Close()
	}
	return out, nil
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
