// Package radio is the "radio" source backed by Radio-Browser
// (https://www.radio-browser.info), a free, community-maintained directory
// of internet radio stations with an open API.
//
// Stations normally play directly in the browser (no server traffic); the
// relay is used only when needed (http stream on an https site, or an HLS
// stream without CORS).
package radio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"bailing/internal/sources"
	"bailing/internal/sources/direct"
)

// Station is the subset of Radio-Browser's station object we use.
type Station struct {
	UUID        string `json:"stationuuid"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	URLResolved string `json:"url_resolved"`
	Homepage    string `json:"homepage"`
	Favicon     string `json:"favicon"`
	Tags        string `json:"tags"`
	Country     string `json:"country"`
	CountryCode string `json:"countrycode"`
	Language    string `json:"language"`
	Codec       string `json:"codec"`
	Bitrate     int    `json:"bitrate"`
	HLS         int    `json:"hls"`
	Votes       int    `json:"votes"`
	ClickCount  int    `json:"clickcount"`
	LastCheckOK int    `json:"lastcheckok"`
}

// Country is one entry of the country list.
type Country struct {
	Name  string `json:"name"`
	Code  string `json:"iso_3166_1"`
	Count int    `json:"stationcount"`
}

// Query is a station search.
type Query struct {
	Name, Country, Tag, Language string
	Page                         int
	// HTTPSOnly keeps stations whose stream is https: an https site can
	// play them directly, so they never need the (traffic-costing) relay.
	HTTPSOnly bool
}

// PageSize is the number of stations per page.
const PageSize = 40

type cached struct {
	at  time.Time
	raw []byte
}

// Source is the radio source.
type Source struct {
	bases  []string
	client *http.Client
	ua     string

	mu    sync.Mutex
	cache map[string]cached
}

// New creates the radio source. apiBase is e.g. https://de1.api.radio-browser.info.
func New(apiBase, version string, client *http.Client) *Source {
	bases := []string{strings.TrimRight(apiBase, "/")}
	if fb := "https://all.api.radio-browser.info"; bases[0] != fb {
		bases = append(bases, fb)
	}
	return &Source{bases: bases, client: client, ua: "bailing/" + version,
		cache: map[string]cached{}}
}

// Info implements sources.Source.
func (s *Source) Info() sources.Info {
	return sources.Info{ID: "radio", Name: "Radio", Kind: "radio",
		Capabilities: []sources.Capability{sources.CapSearch, sources.CapResolve}}
}

func (s *Source) get(ctx context.Context, path string, ttl time.Duration, out any) error {
	s.mu.Lock()
	if c, ok := s.cache[path]; ok && time.Since(c.at) < ttl {
		s.mu.Unlock()
		return json.Unmarshal(c.raw, out)
	}
	s.mu.Unlock()

	var lastErr error
	for _, base := range s.bases {
		ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
		raw, err := s.fetch(ctx2, base+path)
		cancel()
		if err == nil {
			if err = json.Unmarshal(raw, out); err != nil {
				return err
			}
			s.mu.Lock()
			if len(s.cache) > 300 {
				s.cache = map[string]cached{}
			}
			s.cache[path] = cached{at: time.Now(), raw: raw}
			s.mu.Unlock()
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func (s *Source) fetch(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.ua)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("radio-browser: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// Stations searches the directory (cached 10 minutes).
func (s *Source) Stations(ctx context.Context, q Query) ([]Station, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	v := url.Values{}
	if q.Name != "" {
		v.Set("name", q.Name)
	}
	if q.Country != "" {
		v.Set("countrycode", strings.ToUpper(q.Country))
	}
	if q.Tag != "" {
		v.Set("tag", strings.ToLower(q.Tag))
	}
	if q.Language != "" {
		v.Set("language", strings.ToLower(q.Language))
	}
	v.Set("hidebroken", "true")
	if q.HTTPSOnly {
		v.Set("is_https", "true")
	}
	v.Set("order", "clickcount")
	v.Set("reverse", "true")
	v.Set("limit", strconv.Itoa(PageSize))
	v.Set("offset", strconv.Itoa((q.Page-1)*PageSize))
	var list []Station
	if err := s.get(ctx, "/json/stations/search?"+v.Encode(), 10*time.Minute, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// Countries returns countries ordered by station count (cached a day).
func (s *Source) Countries(ctx context.Context) ([]Country, error) {
	var list []Country
	err := s.get(ctx, "/json/countries?order=stationcount&reverse=true&hidebroken=true", 24*time.Hour, &list)
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, c := range list {
		if c.Code != "" && c.Count >= 5 {
			out = append(out, c)
		}
	}
	return out, nil
}

// ByUUID looks up one station.
func (s *Source) ByUUID(ctx context.Context, uuid string) (*Station, error) {
	var list []Station
	if err := s.get(ctx, "/json/stations/byuuid?uuids="+url.QueryEscape(uuid), time.Hour, &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("station not found")
	}
	return &list[0], nil
}

// Click tells Radio-Browser a station was played (their popularity
// ranking depends on it). Fire and forget.
func (s *Source) Click(uuid string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, _ = s.fetch(ctx, s.bases[0]+"/json/url/"+url.PathEscape(uuid))
	}()
}

// ToRef converts a station into a TrackRef the player can queue.
func ToRef(st Station) sources.TrackRef {
	sub := strings.TrimSpace(strings.Join(nonEmpty(st.Country, firstTags(st.Tags, 3)), " · "))
	cover := st.Favicon
	if !strings.HasPrefix(cover, "https://") {
		cover = "" // http icons would be mixed content on an https site
	}
	return sources.TrackRef{Source: "radio", ID: st.UUID, Title: strings.TrimSpace(st.Name),
		Artist: sub, Cover: cover, Extra: map[string]any{
			"url": firstNonEmpty(st.URLResolved, st.URL), "hls": st.HLS == 1,
			"codec": st.Codec, "bitrate": st.Bitrate, "country": st.CountryCode,
			"homepage": st.Homepage, "tags": st.Tags, "votes": st.Votes,
		}}
}

// Search implements sources.Searcher (by name).
func (s *Source) Search(ctx context.Context, q string, page int) ([]sources.TrackRef, bool, error) {
	list, err := s.Stations(ctx, Query{Name: q, Page: page})
	if err != nil {
		return nil, false, err
	}
	out := make([]sources.TrackRef, 0, len(list))
	for _, st := range list {
		out = append(out, ToRef(st))
	}
	return out, len(list) == PageSize, nil
}

// Resolve implements sources.Resolver.
func (s *Source) Resolve(ctx context.Context, t sources.TrackRef, _ string) (*sources.Playable, error) {
	raw, _ := t.Extra["url"].(string)
	hls, _ := t.Extra["hls"].(bool)
	codec, _ := t.Extra["codec"].(string)
	if raw == "" {
		st, err := s.ByUUID(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		raw, hls, codec = firstNonEmpty(st.URLResolved, st.URL), st.HLS == 1, st.Codec
	}
	if t.ID != "" {
		s.Click(t.ID)
	}
	p, err := direct.Inspect(ctx, s.client, raw, nil)
	if err != nil {
		return nil, err
	}
	if hls {
		p.Format = "hls"
	} else if p.Format == "audio" && codec != "" {
		p.Format = strings.ToLower(strings.TrimSuffix(codec, "+"))
	}
	return p, nil
}

func firstTags(tags string, n int) string {
	parts := strings.Split(tags, ",")
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
		if len(out) == n {
			break
		}
	}
	return strings.Join(out, ", ")
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
