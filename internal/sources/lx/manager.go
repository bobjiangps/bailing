package lx

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"bailing/internal/netguard"
	"bailing/internal/store"
)

// MaxScriptSize limits uploaded / loaded scripts.
const MaxScriptSize = 4 << 20

// Script is the public state of one script file.
type Script struct {
	ID          string                `json:"id"`
	File        string                `json:"file"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Version     string                `json:"version"`
	Author      string                `json:"author"`
	Homepage    string                `json:"homepage"`
	Enabled     bool                  `json:"enabled"`
	Status      string                `json:"status"` // loading | ok | error | disabled
	Error       string                `json:"error,omitempty"`
	Update      string                `json:"update,omitempty"`
	Sources     map[string]SourceDecl `json:"sources"`
	Size        int64                 `json:"size"`
	ModTime     int64                 `json:"mtime"`

	vm *VM
}

// Manager loads the scripts in a directory and routes resolve requests.
type Manager struct {
	dir     string
	db      *store.DB
	client  *http.Client
	timeout time.Duration
	ttl     time.Duration
	log     *slog.Logger

	mu      sync.RWMutex
	scripts map[string]*Script
	loadMu  sync.Mutex // one reload at a time

	cacheMu sync.Mutex
	cache   map[string]cachedURL
}

type cachedURL struct {
	url string
	exp time.Time
}

// NewManager creates a manager; call Reload (or Run) to load scripts.
func NewManager(dir string, db *store.DB, client *http.Client, timeout, ttl time.Duration, log *slog.Logger) *Manager {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Manager{dir: dir, db: db, client: client, timeout: timeout, ttl: ttl, log: log,
		scripts: map[string]*Script{}, cache: map[string]cachedURL{}}
}

// Dir returns the scripts directory.
func (m *Manager) Dir() string { return m.dir }

func idOf(file string) string {
	sum := sha1.Sum([]byte(file))
	return hex.EncodeToString(sum[:])[:12]
}

func (m *Manager) enabledState() map[string]bool {
	out := map[string]bool{}
	rows, err := m.db.Query(`SELECT id, enabled FROM lx_scripts`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var en int
		if rows.Scan(&id, &en) == nil {
			out[id] = en == 1
		}
	}
	return out
}

// Run reloads on start and then polls the directory every 30s so scripts
// dropped in by hand are picked up (hot reload).
func (m *Manager) Run(stop <-chan struct{}) {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		m.log.Warn("lx scripts dir", "dir", m.dir, "err", err)
	}
	m.Reload()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			m.closeAll()
			return
		case <-t.C:
			m.Reload()
		}
	}
}

func (m *Manager) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.scripts {
		if s.vm != nil {
			s.vm.Close()
		}
	}
}

// Reload rescans the directory: new or changed files are (re)loaded,
// removed files are dropped. Unchanged scripts keep running.
func (m *Manager) Reload() {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()

	entries, err := os.ReadDir(m.dir)
	if err != nil && !os.IsNotExist(err) {
		m.log.Warn("lx scan", "err", err)
		return
	}
	enabled := m.enabledState()
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".js") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		id := idOf(e.Name())
		seen[id] = true
		en, known := enabled[id]
		if !known {
			en = true
			m.db.Exec(`INSERT OR IGNORE INTO lx_scripts(id, file, enabled, updated_at) VALUES(?,?,1,?)`,
				id, e.Name(), time.Now().Unix())
		}
		m.mu.RLock()
		old := m.scripts[id]
		m.mu.RUnlock()
		if old != nil && old.ModTime == fi.ModTime().Unix() && old.Size == fi.Size() &&
			old.Enabled == en && old.Status != "loading" {
			continue
		}
		m.load(id, e.Name(), fi, en)
	}
	// forget removed files
	m.mu.Lock()
	for id, s := range m.scripts {
		if !seen[id] {
			if s.vm != nil {
				s.vm.Close()
			}
			delete(m.scripts, id)
		}
	}
	m.mu.Unlock()
}

// load (re)starts one script. Must be called with loadMu held.
func (m *Manager) load(id, file string, fi os.FileInfo, enabled bool) {
	s := &Script{ID: id, File: file, Enabled: enabled, Size: fi.Size(),
		ModTime: fi.ModTime().Unix(), Sources: map[string]SourceDecl{}}
	src, err := os.ReadFile(filepath.Join(m.dir, file))
	switch {
	case err != nil:
		s.Status, s.Error = "error", err.Error()
	case len(src) > MaxScriptSize:
		s.Status, s.Error = "error", "script too large"
	}
	if s.Status == "" {
		info := ParseHeader(string(src))
		s.Name, s.Description, s.Version, s.Author, s.Homepage =
			info.Name, info.Description, info.Version, info.Author, info.Homepage
		if s.Name == "" {
			s.Name = strings.TrimSuffix(file, filepath.Ext(file))
			info.Name = s.Name
		}
		if !enabled {
			s.Status = "disabled"
		} else {
			s.Status = "loading"
		}
		m.swap(s)
		if enabled {
			vm, err := Start(string(src), info, m.client, m.timeout, m.log.With("script", s.Name))
			if err != nil {
				s.Status, s.Error = "error", err.Error()
				m.log.Warn("lx script failed", "file", file, "err", err)
			} else {
				s.vm, s.Status, s.Sources = vm, "ok", vm.Sources()
				s.Update = vm.UpdateMessage()
				m.log.Info("lx script loaded", "file", file, "name", s.Name, "platforms", keys(s.Sources))
			}
		}
	}
	m.swap(s)
	m.clearCache()
}

func (m *Manager) swap(s *Script) {
	m.mu.Lock()
	if old := m.scripts[s.ID]; old != nil && old != s && old.vm != nil {
		old.vm.Close()
	}
	cp := *s
	m.scripts[s.ID] = &cp
	m.mu.Unlock()
}

// List returns all scripts sorted by name.
func (m *Manager) List() []Script {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Script, 0, len(m.scripts))
	for _, s := range m.scripts {
		cp := *s
		cp.vm = nil
		if s.vm != nil {
			cp.Update = s.vm.UpdateMessage()
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Save writes an uploaded script and loads it.
func (m *Manager) Save(file string, content []byte) (string, error) {
	file = filepath.Base(strings.TrimSpace(file))
	if !strings.EqualFold(filepath.Ext(file), ".js") {
		file += ".js"
	}
	if strings.ContainsAny(file, `/\:*?"<>|`) || strings.HasPrefix(file, ".") || len(file) > 120 {
		return "", errors.New("invalid file name")
	}
	if len(content) == 0 || len(content) > MaxScriptSize {
		return "", errors.New("script is empty or too large")
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(m.dir, file), content, 0o644); err != nil {
		return "", fmt.Errorf("cannot write script: %w", err)
	}
	id := idOf(file)
	m.db.Exec(`INSERT INTO lx_scripts(id, file, enabled, updated_at) VALUES(?,?,1,?)
		ON CONFLICT(id) DO UPDATE SET enabled=1, updated_at=excluded.updated_at`, id, file, time.Now().Unix())
	m.Reload()
	return id, nil
}

// SetEnabled enables or disables a script.
func (m *Manager) SetEnabled(id string, on bool) error {
	m.mu.RLock()
	s := m.scripts[id]
	m.mu.RUnlock()
	if s == nil {
		return os.ErrNotExist
	}
	m.db.Exec(`INSERT INTO lx_scripts(id, file, enabled, updated_at) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled, updated_at=excluded.updated_at`,
		id, s.File, store.Bool(on), time.Now().Unix())
	m.Reload()
	return nil
}

// Delete removes a script file.
func (m *Manager) Delete(id string) error {
	m.mu.RLock()
	s := m.scripts[id]
	m.mu.RUnlock()
	if s == nil {
		return os.ErrNotExist
	}
	if err := os.Remove(filepath.Join(m.dir, s.File)); err != nil && !os.IsNotExist(err) {
		return err
	}
	m.db.Exec(`DELETE FROM lx_scripts WHERE id=?`, id)
	m.Reload()
	return nil
}

// Qualities returns, per platform, the union of qualities of running scripts
// that can resolve music URLs on it.
func (m *Manager) Qualities() map[string][]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	set := map[string]map[string]bool{}
	for _, s := range m.scripts {
		if s.vm == nil || s.Status != "ok" {
			continue
		}
		for p, d := range s.Sources {
			if !d.Supports("musicUrl") {
				continue
			}
			if set[p] == nil {
				set[p] = map[string]bool{}
			}
			for _, q := range d.Qualitys {
				set[p][q] = true
			}
		}
	}
	out := map[string][]string{}
	for p, qs := range set {
		list := make([]string, 0, len(qs))
		for _, q := range qualityOrder {
			if qs[q] {
				list = append(list, q)
			}
		}
		for q := range qs { // unknown names last
			if !contains(qualityOrder, q) {
				list = append(list, q)
			}
		}
		out[p] = list
	}
	return out
}

// qualityOrder is LX's quality naming, best last.
var qualityOrder = []string{"128k", "192k", "320k", "flac", "flac24bit", "hires", "atmos", "master"}

// pickQuality returns want if declared, else the best declared quality not
// above want, else the lowest declared one.
func pickQuality(declared []string, want string) string {
	if len(declared) == 0 {
		return want
	}
	if contains(declared, want) {
		return want
	}
	wi := indexOf(qualityOrder, want)
	best, bestI := "", -1
	for _, q := range declared {
		if i := indexOf(qualityOrder, q); i >= 0 && i <= wi && i > bestI {
			best, bestI = q, i
		}
	}
	if best != "" {
		return best
	}
	lowest, lowI := declared[0], 1<<30
	for _, q := range declared {
		if i := indexOf(qualityOrder, q); i >= 0 && i < lowI {
			lowest, lowI = q, i
		}
	}
	return lowest
}

// Resolve asks the running scripts (in name order) for a playable URL. It
// returns the URL and the quality actually delivered.
//
// Many scripts quietly answer every request with their cheapest link (often
// 128k), whatever quality was asked for. The platforms' search results tell
// how big each quality of the song is (musicInfo._types), so the probe's
// file size shows which quality really came back. A downgraded answer is
// kept only as a fallback while the remaining scripts get their chance.
func (m *Manager) Resolve(ctx context.Context, platform, quality string, musicInfo map[string]any) (string, string, error) {
	sizes := typeSizes(musicInfo)
	if avail := bestAvailable(sizes, quality); avail != "" {
		// like LX: never ask for more than the song has
		quality = avail
	}
	key := platform + "|" + fmt.Sprint(musicInfo["songmid"]) + "|" + fmt.Sprint(musicInfo["hash"]) + "|" + quality
	m.cacheMu.Lock()
	if c, ok := m.cache[key]; ok && time.Now().Before(c.exp) {
		m.cacheMu.Unlock()
		q := strings.SplitN(c.url, "\x00", 2)
		return q[1], q[0], nil
	}
	m.cacheMu.Unlock()

	type cand struct {
		name string
		vm   *VM
		q    []string
	}
	var cands []cand
	m.mu.RLock()
	for _, s := range m.scripts {
		if s.vm == nil || s.Status != "ok" {
			continue
		}
		if d, ok := s.Sources[platform]; ok && d.Supports("musicUrl") {
			cands = append(cands, cand{s.Name, s.vm, d.Qualitys})
		}
	}
	m.mu.RUnlock()
	if len(cands) == 0 {
		return "", "", fmt.Errorf("no enabled LX script supports platform %q", platform)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].name < cands[j].name })

	remember := func(u, q string) (string, string, error) {
		m.cacheMu.Lock()
		if len(m.cache) > 2000 {
			m.cache = map[string]cachedURL{}
		}
		m.cache[key] = cachedURL{url: q + "\x00" + u, exp: time.Now().Add(m.ttl)}
		m.cacheMu.Unlock()
		return u, q, nil
	}

	var fbURL, fbQ string // best downgraded answer so far
	var fbSize int64
	var errs []string
	for _, c := range cands {
		q := pickQuality(c.q, quality)
		info := map[string]any{"type": q, "musicInfo": musicInfo}
		out, err := c.vm.Request(ctx, platform, "musicUrl", info)
		if err == nil {
			u := strings.Trim(strings.TrimSpace(out), `"`)
			if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				size, perr := m.probe(ctx, u)
				if perr != nil {
					// the script answered, but with a dead link (its own
					// backend is down): try the next script
					m.log.Info("lx link does not work", "script", c.name, "url", clip(u, 120), "err", perr)
					errs = append(errs, c.name+": "+perr.Error())
					continue
				}
				got := matchQuality(sizes, size)
				if got != "" && sizes[q] > 0 && indexOf(qualityOrder, got) < indexOf(qualityOrder, q) {
					m.log.Info("lx script delivered a lower quality", "script", c.name,
						"asked", q, "got", got, "bytes", size)
					if size > fbSize {
						fbURL, fbQ, fbSize = u, got, size
					}
					errs = append(errs, c.name+": gave "+got+" instead of "+q)
					continue
				}
				if got == "" {
					got = q
				}
				return remember(u, got)
			}
			err = fmt.Errorf("script returned %q", clip(out, 80))
		}
		errs = append(errs, c.name+": "+err.Error())
		if ctx.Err() != nil {
			break
		}
	}
	if fbURL != "" {
		return remember(fbURL, fbQ)
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}

// typeSizes reads the expected file size of each quality from musicInfo
// (_types: {"320k": {"size": "7.58MB"}, ...}).
func typeSizes(mi map[string]any) map[string]int64 {
	out := map[string]int64{}
	types, _ := mi["_types"].(map[string]any)
	for q, v := range types {
		if t, ok := v.(map[string]any); ok {
			if s, ok := t["size"].(string); ok {
				if n := parseSize(s); n > 0 {
					out[q] = n
				}
			}
		}
	}
	return out
}

// parseSize understands "7.58MB", "512KB", "1.2 GB" and plain byte counts.
func parseSize(s string) int64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	mul := 1.0
	for _, u := range []struct {
		suf string
		m   float64
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(s, u.suf) {
			s, mul = strings.TrimSpace(strings.TrimSuffix(s, u.suf)), u.m
			break
		}
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil || f <= 0 {
		return 0
	}
	return int64(f * mul)
}

// matchQuality names the quality whose expected size is closest to size
// (within 15 %), or "" when the size says nothing.
func matchQuality(sizes map[string]int64, size int64) string {
	if size <= 0 {
		return ""
	}
	best, bestD := "", 0.15
	for q, n := range sizes {
		d := float64(size-n) / float64(n)
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = q, d
		}
	}
	return best
}

// bestAvailable is the best quality the song is offered in that is not
// above want ("" when the search result listed no sizes).
func bestAvailable(sizes map[string]int64, want string) string {
	if len(sizes) == 0 {
		return ""
	}
	if _, ok := sizes[want]; ok {
		return want
	}
	wi := indexOf(qualityOrder, want)
	if wi < 0 {
		return ""
	}
	best, bestI := "", -1
	for q := range sizes {
		if i := indexOf(qualityOrder, q); i >= 0 && i <= wi && i > bestI {
			best, bestI = q, i
		}
	}
	return best
}

// probe fetches the first bytes of a resolved link and returns the full
// file size when the server tells it (0 otherwise). Scripts often keep
// answering with links to a backend that is gone; catching that here gives
// a clear message (and a chance for the next script) instead of a silent
// "cannot play" in the browser. Network errors are not held against the
// link: the browser may still reach it directly.
func (m *Manager) probe(ctx context.Context, u string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, fmt.Errorf("script returned a bad link")
	}
	req.Header.Set("User-Agent", netguard.UserAgent)
	req.Header.Set("Range", "bytes=0-1")
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("dead link (HTTP %d)", resp.StatusCode)
	}
	if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return 0, errors.New("dead link (web page instead of audio)")
	}
	var size int64
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if i := strings.LastIndexByte(cr, '/'); i >= 0 {
			fmt.Sscanf(cr[i+1:], "%d", &size)
		}
	} else if resp.StatusCode == http.StatusOK {
		size = resp.ContentLength
	}
	return size, nil
}

func (m *Manager) clearCache() {
	m.cacheMu.Lock()
	m.cache = map[string]cachedURL{}
	m.cacheMu.Unlock()
}

func keys[V any](mm map[string]V) []string {
	out := make([]string, 0, len(mm))
	for k := range mm {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool { return indexOf(list, s) >= 0 }

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}
