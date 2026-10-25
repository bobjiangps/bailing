package lx

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"

	"bailing/internal/netguard"
	"bailing/internal/sources"
)

// Platform search adapters. LX Music custom-source scripts only turn a song
// into a playable URL; searching is done by LX's built-in per-platform
// search, re-implemented here after lx-music-desktop
// (https://github.com/lyswhut/lx-music-desktop, Apache-2.0). The results
// are converted to LX's "musicInfo" shape and stored in TrackRef.Extra so a
// script can resolve them unchanged.
//
// These are unofficial endpoints of third-party services; they may change
// or stop working at any time.

type platform interface {
	search(ctx context.Context, c *http.Client, q string, page, limit int) ([]map[string]any, int, error)
}

// PlatformNames are the display names of LX platform codes.
var PlatformNames = map[string]string{
	"kw": "酷我", "kg": "酷狗", "tx": "QQ音乐", "wy": "网易云", "mg": "咪咕",
}

var platforms = map[string]platform{
	"kw": kwPlatform{}, "kg": kgPlatform{}, "tx": txPlatform{}, "wy": wyPlatform{}, "mg": mgPlatform{},
}

// HasSearch reports whether a platform has a search adapter.
func HasSearch(p string) bool { _, ok := platforms[p]; return ok }

// flex decodes a JSON string or number into a string.
type flex string

func (f *flex) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flex(s)
		return nil
	}
	if string(b) == "null" {
		*f = ""
		return nil
	}
	*f = flex(b)
	return nil
}

func (f flex) Int() int64 {
	n, _ := strconv.ParseFloat(string(f), 64)
	return int64(n)
}

func clean(s string) string {
	return strings.TrimSpace(html.UnescapeString(strings.ReplaceAll(s, "&nbsp;", " ")))
}

func interval(sec int64) string {
	if sec <= 0 {
		return ""
	}
	if sec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec%3600/60, sec%60)
	}
	return fmt.Sprintf("%02d:%02d", sec/60, sec%60)
}

func sizeStr(n int64) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2fMB", float64(n)/1024/1024)
}

// typesOf builds LX's types / _types fields.
func typesOf(items ...[3]string) ([]map[string]any, map[string]any) {
	var types []map[string]any
	_types := map[string]any{}
	for _, it := range items {
		q, size, hash := it[0], it[1], it[2]
		if size == "" && hash == "" {
			continue
		}
		t := map[string]any{"type": q, "size": size}
		v := map[string]any{"size": size}
		if hash != "" {
			t["hash"] = hash
			v["hash"] = hash
		}
		types = append(types, t)
		_types[q] = v
	}
	return types, _types
}

func baseInfo(source, id, name, singer, album, albumID string, sec int64, img string) map[string]any {
	return map[string]any{
		"source": source, "songmid": id, "name": name, "singer": singer,
		"albumName": album, "albumId": albumID, "interval": interval(sec),
		"img": img, "lrc": nil, "otherSource": nil, "typeUrl": map[string]any{},
	}
}

func getJSON(ctx context.Context, c *http.Client, req *http.Request, out any) error {
	req = req.WithContext(ctx)
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", netguard.UserAgent)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// toRef converts a musicInfo map into a TrackRef.
func toRef(p string, mi map[string]any, sec int64) sources.TrackRef {
	str := func(k string) string { s, _ := mi[k].(string); return s }
	img := str("img")
	if strings.HasPrefix(img, "http://") {
		img = "https://" + strings.TrimPrefix(img, "http://")
	}
	return sources.TrackRef{Source: "lx:" + p, ID: str("songmid"), Title: str("name"),
		Artist: str("singer"), Album: str("albumName"), Duration: sec * 1000, Cover: img, Extra: mi}
}

// Search runs a platform search and returns TrackRefs.
func Search(ctx context.Context, c *http.Client, p, q string, page, limit int) ([]sources.TrackRef, int, error) {
	pl, ok := platforms[p]
	if !ok {
		return nil, 0, fmt.Errorf("search is not available for platform %q", p)
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 50 {
		limit = 30
	}
	list, total, err := pl.search(ctx, c, q, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("%s search: %w", p, err)
	}
	out := make([]sources.TrackRef, 0, len(list))
	for _, mi := range list {
		sec, _ := mi["_sec"].(int64)
		delete(mi, "_sec")
		if r := toRef(p, mi, sec); r.ID != "" {
			out = append(out, r)
		}
	}
	return out, total, nil
}
