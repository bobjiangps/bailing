package httpapi

import "testing"

func TestCacheKey(t *testing.T) {
	cases := map[string]string{
		"abcdefghijkl_s.webp":     "abcdefghijkl",
		"abcdefghijkl_o.webp":     "abcdefghijkl",
		"0123456789abcdef.jpg":    "0123456789abcdef",
		"0123456789abcdef_m.webp": "0123456789abcdef",
		"plain":                   "plain",
	}
	for in, want := range cases {
		if got := cacheKey(in); got != want {
			t.Errorf("cacheKey(%q) = %q, want %q", in, got, want)
		}
	}
}
