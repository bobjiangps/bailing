package music

import (
	"strings"
	"testing"
)

func TestSortOrder(t *testing.T) {
	cases := []struct {
		sort   string
		desc   bool
		prefix string
	}{
		{"", false, "folder, disc_no"},
		{"", true, "folder DESC"},
		{"title", false, "title COLLATE NOCASE ASC"},
		{"artist", true, "artist COLLATE NOCASE DESC"},
		{"added", true, "added_at DESC"},
		{"recent", false, "added_at ASC"},
		{"mtime", true, "mtime DESC"},
		{"duration", false, "duration_ms ASC"},
		{"x; DROP TABLE tracks", false, "folder, disc_no"},
	}
	for _, c := range cases {
		if got := sortOrder(c.sort, c.desc); !strings.HasPrefix(got, c.prefix) {
			t.Errorf("sortOrder(%q,%v) = %q, want prefix %q", c.sort, c.desc, got, c.prefix)
		}
	}
}
