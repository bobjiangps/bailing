// Package direct is the "direct" source: any http(s) audio URL the user
// pastes — plain files (mp3, flac, m4a …), HLS (.m3u8) or playlist files
// (.m3u / .pls, the first entry is used).
//
// Probing a URL only reads a few KB from the origin. That is *inbound*
// traffic for the server, which Aliyun does not bill.
package direct

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"bailing/internal/netguard"
	"bailing/internal/sources"
)

// Source implements sources.Source / Resolver for pasted URLs.
type Source struct {
	client *http.Client
}

// New creates the direct source.
func New(client *http.Client) *Source { return &Source{client: client} }

// Info implements sources.Source.
func (s *Source) Info() sources.Info {
	return sources.Info{ID: "direct", Name: "Direct URL", Kind: "online",
		Capabilities: []sources.Capability{sources.CapResolve}}
}

// Resolve implements sources.Resolver. TrackRef.ID is the URL.
func (s *Source) Resolve(ctx context.Context, t sources.TrackRef, _ string) (*sources.Playable, error) {
	raw := t.ID
	if v, ok := t.Extra["url"].(string); ok && v != "" {
		raw = v
	}
	return Inspect(ctx, s.client, raw, nil)
}

var extFormat = map[string]string{
	".mp3": "mp3", ".aac": "aac", ".m4a": "m4a", ".mp4": "m4a", ".flac": "flac",
	".ogg": "ogg", ".oga": "ogg", ".opus": "opus", ".wav": "wav", ".webm": "webm",
	".m3u8": "hls",
}

// Inspect works out what a URL is and returns a Playable (Mode left empty:
// the HTTP layer decides direct vs. relay). headers are sent while probing.
func Inspect(ctx context.Context, c *http.Client, raw string, headers map[string]string) (*sources.Playable, error) {
	return inspect(ctx, c, raw, headers, 0)
}

func inspect(ctx context.Context, c *http.Client, raw string, headers map[string]string, depth int) (*sources.Playable, error) {
	u, err := netguard.CheckURL(raw)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if f, ok := extFormat[ext]; ok {
		return &sources.Playable{URL: u.String(), Format: f, Headers: headers}, nil
	}

	// unknown extension, .m3u or .pls: look at the first bytes
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", netguard.UserAgent)
	req.Header.Set("Range", "bytes=0-65535")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach URL: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("URL returned HTTP %d", resp.StatusCode)
	}
	final := resp.Request.URL
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	head, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	text := strings.TrimSpace(string(head))

	switch {
	case strings.HasPrefix(text, "#EXTM3U") && strings.Contains(text, "#EXT-X-"):
		return &sources.Playable{URL: final.String(), Format: "hls", Headers: headers}, nil
	case ext == ".pls" || ct == "audio/x-scpls" || strings.HasPrefix(strings.ToLower(text), "[playlist]"):
		return follow(ctx, c, final, firstPLS(text), headers, depth)
	case ext == ".m3u" || strings.Contains(ct, "mpegurl") || strings.HasPrefix(text, "#EXTM3U"):
		return follow(ctx, c, final, firstM3U(text), headers, depth)
	case strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") ||
		ct == "application/ogg" || ct == "application/octet-stream":
		return &sources.Playable{URL: final.String(), Format: formatOf(ct), Headers: headers}, nil
	case strings.HasPrefix(ct, "text/html"):
		return nil, errors.New("this is a web page, not an audio address")
	}
	// many radio servers send no or odd content types; let the browser try
	return &sources.Playable{URL: final.String(), Format: "audio", Headers: headers}, nil
}

func follow(ctx context.Context, c *http.Client, base *url.URL, entry string, headers map[string]string, depth int) (*sources.Playable, error) {
	if entry == "" {
		return nil, errors.New("the playlist file has no entries")
	}
	if depth >= 3 {
		return nil, errors.New("playlist nesting too deep")
	}
	ref, err := url.Parse(entry)
	if err != nil {
		return nil, errors.New("bad playlist entry")
	}
	return inspect(ctx, c, base.ResolveReference(ref).String(), headers, depth+1)
}

func firstM3U(text string) string {
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if ln != "" && !strings.HasPrefix(ln, "#") {
			return ln
		}
	}
	return ""
}

func firstPLS(text string) string {
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if k, v, ok := strings.Cut(ln, "="); ok && strings.HasPrefix(strings.ToLower(k), "file") {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func formatOf(ct string) string {
	switch ct {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/aac", "audio/aacp":
		return "aac"
	case "audio/mp4", "audio/x-m4a":
		return "m4a"
	case "audio/flac", "audio/x-flac":
		return "flac"
	case "audio/ogg", "application/ogg":
		return "ogg"
	case "audio/wav", "audio/x-wav":
		return "wav"
	}
	return "audio"
}

// TitleFromURL makes a readable title from a URL ("song name.mp3" → "song name").
func TitleFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	base := path.Base(u.Path)
	if p, err := url.PathUnescape(base); err == nil {
		base = p
	}
	base = strings.TrimSuffix(base, path.Ext(base))
	if base == "" || base == "/" || base == "." {
		return u.Host
	}
	return base
}
