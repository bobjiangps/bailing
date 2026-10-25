// Package traffic meters the bytes bailing sends to clients.
//
// The ECS instance is billed by outbound traffic, so every media response is
// counted (per day and category) and every playback stream that goes through
// the server (local tracks, P2 proxy) gets a live byte counter the player can
// show next to a timer.
package traffic

import (
	"log/slog"
	"sort"
	"sync"
	"time"

	"bailing/internal/store"
)

// Categories.
const (
	Photo = "photo"
	Cover = "cover"
	Track = "track"
	Proxy = "proxy"
	App   = "app"
)

// Categories lists all categories in display order.
var Categories = []string{Track, Proxy, Photo, Cover, App}

type dayKey struct{ day, cat string }

type delta struct{ bytes, reqs int64 }

// Stream is a live counter for one playback session (identified by the
// client-generated sid query parameter).
type Stream struct {
	SID      string `json:"sid"`
	UserID   int64  `json:"-"`
	Category string `json:"category"`
	Ref      string `json:"ref"`
	Title    string `json:"title"`
	Bytes    int64  `json:"bytes"`
	Started  int64  `json:"started_at"`
	Last     int64  `json:"last_at"`
	dirty    bool
}

// Page is the running total of one opened browser page (identified by the
// client-generated bl_pg cookie, renewed on every page load). It covers
// every byte the server sent to that page: tracks, relays, photos, covers,
// app files and API responses. Kept in memory only.
type Page struct {
	ID       string           `json:"-"`
	Bytes    int64            `json:"bytes"`
	Requests int64            `json:"requests"`
	ByCat    map[string]int64 `json:"by_category"`
	Started  int64            `json:"started_at"`
	Last     int64            `json:"last_at"`
}

const maxPages = 2000

// Meter accumulates counters in memory and flushes them to SQLite.
type Meter struct {
	db  *store.DB
	log *slog.Logger

	mu      sync.Mutex
	pending map[dayKey]*delta
	streams map[string]*Stream
	pages   map[string]*Page

	quotaBytes int64
	warnPct    int
	resetDay   int
}

// New creates a meter. quotaGB = 0 disables the quota.
func New(db *store.DB, quotaGB float64, warnPct, resetDay int, log *slog.Logger) *Meter {
	if warnPct <= 0 || warnPct > 100 {
		warnPct = 80
	}
	return &Meter{db: db, log: log, pending: map[dayKey]*delta{}, streams: map[string]*Stream{},
		pages:      map[string]*Page{},
		quotaBytes: int64(quotaGB * 1024 * 1024 * 1024), warnPct: warnPct, resetDay: resetDay}
}

func today() string { return time.Now().Format("2006-01-02") }

// Request counts one request for a category (and page, if any).
func (m *Meter) Request(cat string, pg *Page) {
	m.mu.Lock()
	m.get(dayKey{today(), cat}).reqs++
	if pg != nil {
		pg.Requests++
		pg.Last = time.Now().Unix()
	}
	m.mu.Unlock()
}

// Add counts n bytes for a category and (optionally) a stream and a page.
func (m *Meter) Add(cat string, st *Stream, pg *Page, n int64) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	m.get(dayKey{today(), cat}).bytes += n
	now := time.Now().Unix()
	if st != nil {
		st.Bytes += n
		st.Last = now
		st.dirty = true
	}
	if pg != nil {
		pg.Bytes += n
		pg.ByCat[cat] += n
		pg.Last = now
	}
	m.mu.Unlock()
}

// OpenPage returns the running total for page id, creating it if needed.
func (m *Meter) OpenPage(id string) *Page {
	if id == "" || len(id) > 64 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if pg, ok := m.pages[id]; ok {
		return pg
	}
	if len(m.pages) >= maxPages {
		return nil
	}
	now := time.Now().Unix()
	pg := &Page{ID: id, ByCat: map[string]int64{}, Started: now, Last: now}
	m.pages[id] = pg
	return pg
}

// PageInfo returns a snapshot of a page total.
func (m *Meter) PageInfo(id string) (Page, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pg, ok := m.pages[id]
	if !ok {
		return Page{}, false
	}
	cp := *pg
	cp.ByCat = make(map[string]int64, len(pg.ByCat))
	for k, v := range pg.ByCat {
		cp.ByCat[k] = v
	}
	return cp, true
}

func (m *Meter) get(k dayKey) *delta {
	d := m.pending[k]
	if d == nil {
		d = &delta{}
		m.pending[k] = d
	}
	return d
}

// OpenStream returns the live counter for sid, creating it if needed.
// Range requests of the same playback share one sid and thus one counter.
func (m *Meter) OpenStream(sid string, userID int64, cat, ref, title string) *Stream {
	if sid == "" || len(sid) > 64 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.streams[sid]; ok {
		if st.UserID == userID {
			return st
		}
		return nil // someone else's sid: do not attach
	}
	now := time.Now().Unix()
	st := &Stream{SID: sid, UserID: userID, Category: cat, Ref: ref, Title: title,
		Started: now, Last: now, dirty: true}
	m.streams[sid] = st
	return st
}

// StreamInfo returns a snapshot of a live stream owned by userID.
func (m *Meter) StreamInfo(sid string, userID int64) (Stream, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.streams[sid]
	if !ok || st.UserID != userID {
		return Stream{}, false
	}
	return *st, true
}

// Flush writes pending counters to the database.
func (m *Meter) Flush() {
	m.mu.Lock()
	pending := m.pending
	m.pending = map[dayKey]*delta{}
	var dirty []Stream
	cutoff := time.Now().Add(-2 * time.Hour).Unix()
	for sid, st := range m.streams {
		if st.dirty {
			dirty = append(dirty, *st)
			st.dirty = false
		}
		if st.Last < cutoff {
			delete(m.streams, sid)
		}
	}
	// a page silent for 12h is closed (or asleep for good)
	pageCutoff := time.Now().Add(-12 * time.Hour).Unix()
	for id, pg := range m.pages {
		if pg.Last < pageCutoff {
			delete(m.pages, id)
		}
	}
	m.mu.Unlock()

	if len(pending) == 0 && len(dirty) == 0 {
		return
	}
	tx, err := m.db.Begin()
	if err != nil {
		m.log.Warn("traffic flush", "err", err)
		return
	}
	for k, d := range pending {
		tx.Exec(`INSERT INTO traffic_daily(day, category, bytes, requests) VALUES(?,?,?,?)
			ON CONFLICT(day, category) DO UPDATE SET bytes=bytes+excluded.bytes,
			requests=requests+excluded.requests`, k.day, k.cat, d.bytes, d.reqs)
	}
	for _, st := range dirty {
		tx.Exec(`INSERT INTO stream_sessions(sid, user_id, category, ref, title, bytes, started_at, last_at)
			VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(sid) DO UPDATE SET bytes=excluded.bytes,
			last_at=excluded.last_at`, st.SID, st.UserID, st.Category, st.Ref, st.Title, st.Bytes,
			st.Started, st.Last)
	}
	if err := tx.Commit(); err != nil {
		m.log.Warn("traffic flush commit", "err", err)
	}
}

// Run flushes every interval until stop is closed (then flushes once more).
func (m *Meter) Run(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			m.Flush()
			// keep 400 days of daily rows and 90 days of stream rows
			m.db.Exec(`DELETE FROM traffic_daily WHERE day < ?`,
				time.Now().AddDate(0, 0, -400).Format("2006-01-02"))
			m.db.Exec(`DELETE FROM stream_sessions WHERE started_at < ?`,
				time.Now().AddDate(0, 0, -90).Unix())
		case <-stop:
			m.Flush()
			return
		}
	}
}

// ---- reporting ----------------------------------------------------------------

// Day is one bar of the 30-day chart.
type Day struct {
	Day   string `json:"day"`
	Bytes int64  `json:"bytes"`
}

// Summary is returned by GET /api/traffic/summary.
type Summary struct {
	Today        int64            `json:"today"`
	Month        int64            `json:"month"`
	MonthStart   string           `json:"month_start"`
	ByCategory   map[string]int64 `json:"by_category"` // current billing month
	Days         []Day            `json:"days"`        // last 30 days
	QuotaBytes   int64            `json:"quota_bytes"`
	WarnPercent  int              `json:"warn_percent"`
	Warn         bool             `json:"warn"`
	Over         bool             `json:"over"`
	RecentStream []Stream         `json:"recent_streams"`
}

// MonthStart returns the first day of the current billing month.
func (m *Meter) MonthStart(now time.Time) time.Time {
	d := m.resetDay
	start := time.Date(now.Year(), now.Month(), d, 0, 0, 0, 0, now.Location())
	if now.Day() < d {
		start = start.AddDate(0, -1, 0)
	}
	return start
}

// Summary flushes and aggregates.
func (m *Meter) Summary(recent int) (*Summary, error) {
	m.Flush()
	now := time.Now()
	s := &Summary{ByCategory: map[string]int64{}, QuotaBytes: m.quotaBytes, WarnPercent: m.warnPct,
		Days: []Day{}, RecentStream: []Stream{}}
	ms := m.MonthStart(now).Format("2006-01-02")
	s.MonthStart = ms
	td := now.Format("2006-01-02")

	rows, err := m.db.Query(`SELECT day, category, bytes FROM traffic_daily WHERE day >= ?`,
		minDay(ms, now.AddDate(0, 0, -29).Format("2006-01-02")))
	if err != nil {
		return nil, err
	}
	perDay := map[string]int64{}
	for rows.Next() {
		var day, cat string
		var b int64
		rows.Scan(&day, &cat, &b)
		perDay[day] += b
		if day >= ms {
			s.Month += b
			s.ByCategory[cat] += b
		}
		if day == td {
			s.Today += b
		}
	}
	rows.Close()
	for i := 29; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		s.Days = append(s.Days, Day{Day: d, Bytes: perDay[d]})
	}
	if m.quotaBytes > 0 {
		s.Warn = s.Month*100 >= m.quotaBytes*int64(m.warnPct)
		s.Over = s.Month >= m.quotaBytes
	}

	if recent <= 0 {
		recent = 20
	}
	srows, err := m.db.Query(`SELECT sid, category, ref, title, bytes, started_at, last_at
		FROM stream_sessions ORDER BY started_at DESC LIMIT ?`, recent)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var st Stream
		srows.Scan(&st.SID, &st.Category, &st.Ref, &st.Title, &st.Bytes, &st.Started, &st.Last)
		s.RecentStream = append(s.RecentStream, st)
	}
	srows.Close()
	sort.SliceStable(s.RecentStream, func(i, j int) bool {
		return s.RecentStream[i].Started > s.RecentStream[j].Started
	})
	return s, nil
}

// OverQuota reports whether the monthly quota is exhausted (P2: blocks proxy).
func (m *Meter) OverQuota() bool {
	if m.quotaBytes <= 0 {
		return false
	}
	s, err := m.Summary(1)
	return err == nil && s.Over
}

func minDay(a, b string) string {
	if a < b {
		return a
	}
	return b
}
