package lx

import "testing"

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{
		"7.5MB": 15 << 19, "512KB": 512 << 10, "1.5 GB": 3 << 29,
		"12345": 12345, "": 0, "abc": 0,
	} {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestMatchQuality(t *testing.T) {
	// 狂野之城 on wy: 128k 3.03MB, 320k 7.58MB, flac 21.73MB
	mi := map[string]any{"_types": map[string]any{
		"128k": map[string]any{"size": "3.03MB"},
		"320k": map[string]any{"size": "7.58MB"},
		"flac": map[string]any{"size": "21.73MB"},
	}}
	sizes := typeSizes(mi)
	if len(sizes) != 3 {
		t.Fatalf("typeSizes = %v", sizes)
	}
	cases := map[int64]string{3180295: "128k", 7950672: "320k", 22782227: "flac", 5_000_000: "", 0: ""}
	for size, want := range cases {
		if got := matchQuality(sizes, size); got != want {
			t.Errorf("matchQuality(%d) = %q, want %q", size, got, want)
		}
	}
	if q := bestAvailable(sizes, "flac24bit"); q != "flac" {
		t.Errorf("bestAvailable(flac24bit) = %q", q)
	}
	if q := bestAvailable(sizes, "320k"); q != "320k" {
		t.Errorf("bestAvailable(320k) = %q", q)
	}
	if q := bestAvailable(map[string]int64{}, "320k"); q != "" {
		t.Errorf("bestAvailable(empty) = %q", q)
	}
}
