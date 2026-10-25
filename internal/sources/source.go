// Package sources defines the pluggable music-source interfaces.
//
// Phase 1 only registers "local". Phase 2 adds "direct" (URLs, m3u8) and
// "radio" (Radio-Browser); phase 3 adds "lx:<platform>" sources backed by
// LX Music custom-source scripts. All of them speak TrackRef / Playable, so
// the player can mix them in one queue.
package sources

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// Capability flags what a source can do.
type Capability string

const (
	CapSearch  Capability = "search"
	CapResolve Capability = "resolve"
	CapLyrics  Capability = "lyrics"
)

// Info describes a source.
type Info struct {
	ID           string       `json:"id"`   // "local" | "direct" | "radio" | "lx:kw" ...
	Name         string       `json:"name"` //
	Kind         string       `json:"kind"` // local | online | radio | lx
	Capabilities []Capability `json:"capabilities"`
	Qualities    []string     `json:"qualities,omitempty"`
}

// TrackRef is a source-independent song reference. Extra keeps private
// fields such as LX Music's songmid / hash.
type TrackRef struct {
	Source   string         `json:"source"`
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Artist   string         `json:"artist"`
	Album    string         `json:"album"`
	Duration int64          `json:"duration_ms"`
	Cover    string         `json:"cover"`
	Extra    map[string]any `json:"extra,omitempty"`
}

// Playable is a resolved, playable address.
type Playable struct {
	URL       string            `json:"url"`
	Format    string            `json:"format"` // mp3 | flac | aac | hls ...
	Headers   map[string]string `json:"-"`      // non-empty => must go through the proxy
	ExpiresAt time.Time         `json:"expires_at"`
	// Mode: "server" (served by bailing, costs ECS traffic),
	// "direct" (browser connects to the origin, no ECS traffic) or
	// "proxy" (relayed through bailing, costs ECS traffic).
	Mode string `json:"mode"`
}

// Source is implemented by every source.
type Source interface{ Info() Info }

// Searcher is implemented by sources that can search.
type Searcher interface {
	Search(ctx context.Context, q string, page int) ([]TrackRef, bool, error)
}

// Resolver turns a TrackRef into a Playable.
type Resolver interface {
	Resolve(ctx context.Context, t TrackRef, quality string) (*Playable, error)
}

// ErrUnsupported is returned when a source lacks a capability.
var ErrUnsupported = errors.New("capability not supported by source")

// Registry holds the registered sources.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Source
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{m: map[string]Source{}} }

// Register adds or replaces a source.
func (r *Registry) Register(s Source) {
	r.mu.Lock()
	r.m[s.Info().ID] = s
	r.mu.Unlock()
}

// Get returns a source by id.
func (r *Registry) Get(id string) (Source, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.m[id]
	return s, ok
}

// List returns all source infos sorted by id.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Info, 0, len(r.m))
	for _, s := range r.m {
		out = append(out, s.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
