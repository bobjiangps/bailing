// Package proxy implements the signed audio relay (/proxy/stream).
//
// A relay link carries its target in the query string, signed with a
// server-side HMAC key and an expiry, so only links the server handed out
// can be used — the server cannot be abused as an open proxy. HLS playlists
// are rewritten on the fly so every segment also goes through the relay.
package proxy

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Kinds of relayed resources.
const (
	KindAudio    = "a" // a whole audio stream / file (concurrency-limited)
	KindPlaylist = "h" // an HLS playlist (rewritten)
	KindSegment  = "s" // an HLS segment / key / init section
)

// Target is the signed payload of a relay link.
type Target struct {
	URL     string            `json:"u"`
	Headers map[string]string `json:"h,omitempty"`
	Exp     int64             `json:"e"`
	Kind    string            `json:"k,omitempty"`
	Ref     string            `json:"r,omitempty"`
	Title   string            `json:"t,omitempty"`
}

// Signer creates and verifies relay links.
type Signer struct {
	key      []byte
	basePath string
	ttl      time.Duration
}

// LoadOrCreateKey reads the HMAC key from path, creating a random one if
// the file does not exist yet.
func LoadOrCreateKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return b, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// NewSigner creates a signer. basePath is server.base_path.
func NewSigner(key []byte, basePath string, ttl time.Duration) *Signer {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Signer{key: key, basePath: basePath, ttl: ttl}
}

func (s *Signer) mac(data string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:18])
}

// URL returns a relay link (path + query) for t.
func (s *Signer) URL(t Target) string {
	if t.Exp == 0 {
		t.Exp = time.Now().Add(s.ttl).Unix()
	}
	if t.Kind == "" {
		t.Kind = KindAudio
	}
	b, _ := json.Marshal(t)
	d := base64.RawURLEncoding.EncodeToString(b)
	return s.basePath + "/proxy/stream?d=" + d + "&s=" + s.mac(d)
}

// Verify checks a link's signature and expiry.
func (s *Signer) Verify(d, sig string) (*Target, error) {
	if d == "" || len(d) > 16384 || !hmac.Equal([]byte(sig), []byte(s.mac(d))) {
		return nil, errors.New("bad signature")
	}
	b, err := base64.RawURLEncoding.DecodeString(d)
	if err != nil {
		return nil, errors.New("bad payload")
	}
	var t Target
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, errors.New("bad payload")
	}
	if time.Now().Unix() > t.Exp {
		return nil, errors.New("link expired")
	}
	return &t, nil
}

var reURI = regexp.MustCompile(`URI="([^"]*)"`)

// RewritePlaylist rewrites every URI in an HLS playlist with sign(abs, kind).
// base is the final URL the playlist was fetched from (after redirects).
func RewritePlaylist(body string, base *url.URL, sign func(abs, kind string) string) string {
	master := strings.Contains(body, "#EXT-X-STREAM-INF")
	lines := strings.Split(body, "\n")
	for i, raw := range lines {
		ln := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "#") {
			kind := KindSegment // EXT-X-KEY, EXT-X-MAP …
			if strings.HasPrefix(ln, "#EXT-X-MEDIA:") || strings.HasPrefix(ln, "#EXT-X-I-FRAME-STREAM-INF") {
				kind = KindPlaylist
			}
			lines[i] = reURI.ReplaceAllStringFunc(ln, func(m string) string {
				u := m[5 : len(m)-1]
				if u == "" {
					return m
				}
				return `URI="` + sign(resolve(base, u), kind) + `"`
			})
			continue
		}
		kind := KindSegment
		if master || strings.HasSuffix(strings.ToLower(pathOf(ln)), ".m3u8") {
			kind = KindPlaylist
		}
		lines[i] = sign(resolve(base, ln), kind)
	}
	return strings.Join(lines, "\n")
}

func resolve(base *url.URL, ref string) string {
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

func pathOf(s string) string {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		return s[:i]
	}
	return s
}

// IsPlaylist reports whether a response looks like an HLS playlist.
func IsPlaylist(contentType, urlPath string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "mpegurl") || strings.HasSuffix(strings.ToLower(urlPath), ".m3u8")
}
