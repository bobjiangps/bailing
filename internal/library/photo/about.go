package photo

import (
	"strings"

	"bailing/internal/library"
)

// About is the parsed content of an album's about.txt.
type About struct {
	Title       string
	Cover       string
	Date        string
	Sort        string // "name" (default) | "time"
	Description string
}

var aboutKeys = map[string]string{
	"title": "title", "标题": "title", "名称": "title",
	"cover": "cover", "封面": "cover",
	"date": "date", "日期": "date",
	"sort": "sort", "排序": "sort",
}

// ParseAbout parses about.txt. Supported layouts:
//
//	title: 京都 · 春          ← key: value header (also 中文键名, full-width colon)
//	cover: IMG_0012.HEIC
//	---
//	description...
//
// A file with no recognised keys is read as "first line = title, rest =
// description".
func ParseAbout(raw []byte) About {
	text := strings.ReplaceAll(library.DecodeText(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	var a About
	var header, body []string
	sep := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			sep = i
			break
		}
	}
	if sep >= 0 {
		header, body = lines[:sep], lines[sep+1:]
	} else {
		header = lines
	}

	recognised := false
	var leftovers []string
	for _, l := range header {
		k, v, ok := splitKV(l)
		if ok {
			if field, known := aboutKeys[strings.ToLower(k)]; known {
				recognised = true
				switch field {
				case "title":
					a.Title = v
				case "cover":
					a.Cover = v
				case "date":
					a.Date = v
				case "sort":
					a.Sort = strings.ToLower(v)
				}
				continue
			}
		}
		leftovers = append(leftovers, l)
	}

	switch {
	case sep >= 0:
		a.Description = strings.TrimSpace(strings.Join(body, "\n"))
	case recognised:
		a.Description = strings.TrimSpace(strings.Join(leftovers, "\n"))
	default:
		// simple layout: first non-empty line is the title
		trimmed := strings.TrimSpace(text)
		first, rest, _ := strings.Cut(trimmed, "\n")
		a.Title = strings.TrimSpace(first)
		a.Description = strings.TrimSpace(rest)
	}
	if a.Sort != "time" {
		a.Sort = "name"
	}
	return a
}

func splitKV(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	i := strings.IndexAny(line, ":：")
	if i <= 0 {
		return "", "", false
	}
	k := strings.TrimSpace(line[:i])
	// skip the separator (":" is 1 byte, "：" is 3 bytes)
	rest := line[i:]
	if strings.HasPrefix(rest, "：") {
		rest = rest[len("："):]
	} else {
		rest = rest[1:]
	}
	if strings.ContainsAny(k, " \t") && len([]rune(k)) > 12 {
		return "", "", false // looks like prose, not a key
	}
	return k, strings.TrimSpace(rest), true
}
