// Package lyrics finds lyrics for a track (sidecar .lrc, embedded tag,
// downloaded cache, LRCLIB online) and parses LRC into timed lines.
package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Line is one lyric line. T is the start time in ms, or -1 for unsynced text.
type Line struct {
	T     int64  `json:"t"`
	Text  string `json:"text"`
	Trans string `json:"trans,omitempty"`
}

var (
	reTime   = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	reMeta   = regexp.MustCompile(`^\[([a-zA-Z#]+):(.*)\]\s*$`)
	reWordTS = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`)
)

// Parse parses LRC text. When a timestamp occurs twice, the second line is
// treated as a translation of the first (common in Chinese LRC files).
// Text without any timestamps is returned as unsynced lines (T = -1).
// synced reports whether timestamps were found.
func Parse(text string) (lines []Line, synced bool) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var offset int64
	byTime := map[int64]*Line{}
	var order []int64

	for _, raw := range strings.Split(text, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if m := reMeta.FindStringSubmatch(raw); m != nil && !reTime.MatchString(raw) {
			if strings.EqualFold(m[1], "offset") {
				offset, _ = strconv.ParseInt(strings.TrimSpace(m[2]), 10, 64)
			}
			continue
		}
		stamps := reTime.FindAllStringSubmatchIndex(raw, -1)
		if len(stamps) == 0 {
			continue
		}
		// text follows the last leading timestamp
		content := strings.TrimSpace(reWordTS.ReplaceAllString(raw[stamps[len(stamps)-1][1]:], ""))
		for _, st := range stamps {
			mm, _ := strconv.ParseInt(raw[st[2]:st[3]], 10, 64)
			ss, _ := strconv.ParseInt(raw[st[4]:st[5]], 10, 64)
			var frac int64
			if st[6] >= 0 {
				f := raw[st[6]:st[7]]
				frac, _ = strconv.ParseInt(f, 10, 64)
				switch len(f) {
				case 1:
					frac *= 100
				case 2:
					frac *= 10
				}
			}
			t := mm*60000 + ss*1000 + frac - offset // positive offset = lyrics earlier
			if t < 0 {
				t = 0
			}
			if l, ok := byTime[t]; ok {
				if l.Trans == "" && content != "" && content != l.Text {
					l.Trans = content
				}
				continue
			}
			byTime[t] = &Line{T: t, Text: content}
			order = append(order, t)
		}
	}

	if len(order) == 0 {
		for _, raw := range strings.Split(text, "\n") {
			raw = strings.TrimSpace(raw)
			if raw == "" || reMeta.MatchString(raw) {
				continue
			}
			lines = append(lines, Line{T: -1, Text: raw})
		}
		return lines, false
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	for _, t := range order {
		lines = append(lines, *byTime[t])
	}
	// trim empty lines at the very beginning (credits spacing)
	for len(lines) > 0 && lines[0].Text == "" {
		lines = lines[1:]
	}
	return lines, true
}
