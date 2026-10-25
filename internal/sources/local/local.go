// Package local adapts the local music library to the sources interfaces.
package local

import (
	"context"

	"bailing/internal/library/music"
	"bailing/internal/sources"
)

// Source is the "local" music source.
type Source struct {
	lib      *music.Library
	basePath string // server.base_path, prefixed to media URLs
}

// New creates the local source.
func New(lib *music.Library, basePath string) *Source {
	return &Source{lib: lib, basePath: basePath}
}

// Info implements sources.Source.
func (s *Source) Info() sources.Info {
	return sources.Info{ID: "local", Name: "Local", Kind: "local",
		Capabilities: []sources.Capability{sources.CapSearch, sources.CapResolve, sources.CapLyrics}}
}

// Search implements sources.Searcher.
func (s *Source) Search(ctx context.Context, q string, page int) ([]sources.TrackRef, bool, error) {
	const size = 50
	if page < 1 {
		page = 1
	}
	list, total, err := s.lib.Tracks(music.Query{Q: q, Offset: (page - 1) * size, Limit: size})
	if err != nil {
		return nil, false, err
	}
	out := make([]sources.TrackRef, 0, len(list))
	for _, t := range list {
		out = append(out, sources.TrackRef{Source: "local", ID: t.ID, Title: t.Title,
			Artist: t.Artist, Album: t.Album, Duration: t.Duration, Cover: t.Cover})
	}
	return out, page*size < total, nil
}

// Resolve implements sources.Resolver: local tracks are always served by
// bailing itself, so they always cost server traffic.
func (s *Source) Resolve(ctx context.Context, t sources.TrackRef, quality string) (*sources.Playable, error) {
	ti, err := s.lib.Get(t.ID)
	if err != nil {
		return nil, err
	}
	return &sources.Playable{URL: s.basePath + "/media/track/" + ti.ID, Format: ti.Format,
		Mode: "server"}, nil
}
