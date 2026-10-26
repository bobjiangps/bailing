package lx

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"bailing/internal/sources"
)

// Source is the "lx:<platform>" source: Search uses the platform adapter,
// Resolve goes to the enabled scripts that declared the platform.
type Source struct {
	p      string
	m      *Manager
	client *http.Client
}

// NewSource creates the source for one platform code (kw, kg, tx, wy …).
func NewSource(p string, m *Manager, client *http.Client) *Source {
	return &Source{p: p, m: m, client: client}
}

// Info implements sources.Source. Capabilities follow the loaded scripts.
func (s *Source) Info() sources.Info {
	name := PlatformNames[s.p]
	if name == "" {
		name = s.p
	}
	info := sources.Info{ID: "lx:" + s.p, Name: name, Kind: "lx",
		Capabilities: []sources.Capability{}}
	if HasSearch(s.p) {
		info.Capabilities = append(info.Capabilities, sources.CapSearch)
	}
	if qs, ok := s.m.Qualities()[s.p]; ok {
		info.Capabilities = append(info.Capabilities, sources.CapResolve)
		info.Qualities = qs
	}
	return info
}

// Search implements sources.Searcher.
func (s *Source) Search(ctx context.Context, q string, page int) ([]sources.TrackRef, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	const limit = 30
	list, total, err := Search(ctx, s.client, s.p, q, page, limit)
	if err != nil {
		return nil, false, err
	}
	more := total > page*limit || (total == 0 && len(list) == limit)
	return list, more, nil
}

// Resolve implements sources.Resolver.
func (s *Source) Resolve(ctx context.Context, t sources.TrackRef, quality string) (*sources.Playable, error) {
	if len(t.Extra) == 0 {
		return nil, errors.New("missing song info; search for the song again")
	}
	if quality == "" {
		quality = "320k"
	}
	mi := make(map[string]any, len(t.Extra)+1)
	for k, v := range t.Extra {
		mi[k] = v
	}
	mi["source"] = s.p
	u, q, err := s.m.Resolve(ctx, s.p, quality, mi)
	if err != nil {
		return nil, err
	}
	return &sources.Playable{URL: u, Format: formatFromURL(u, q), Quality: q,
		ExpiresAt: time.Now().Add(s.m.ttl)}, nil
}

func formatFromURL(u, quality string) string {
	p := u
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	switch ext := strings.ToLower(path.Ext(p)); ext {
	case ".mp3", ".flac", ".m4a", ".aac", ".ogg", ".wav":
		return ext[1:]
	case ".m3u8":
		return "hls"
	}
	if strings.HasPrefix(quality, "flac") {
		return "flac"
	}
	return "mp3"
}
