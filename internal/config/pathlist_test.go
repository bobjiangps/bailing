package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadPaths(t *testing.T, body string) *Config {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return c
}

func TestPathListSingle(t *testing.T) {
	c := loadPaths(t, "paths:\n  music: \"./m\"\n  photos: \"./p\"\n")
	if len(c.Paths.Music) != 1 || !filepath.IsAbs(c.Paths.Music[0]) || filepath.Base(c.Paths.Music[0]) != "m" {
		t.Fatalf("music = %v", c.Paths.Music)
	}
	if len(c.Paths.Photos) != 1 || filepath.Base(c.Paths.Photos[0]) != "p" {
		t.Fatalf("photos = %v", c.Paths.Photos)
	}
}

func TestPathListMany(t *testing.T) {
	c := loadPaths(t, "paths:\n  music: [\"./a\", \"./b\", \"./a/\"]\n")
	if len(c.Paths.Music) != 2 {
		t.Fatalf("want 2 deduplicated roots, got %v", c.Paths.Music)
	}
	if filepath.Base(c.Paths.Music[0]) != "a" || filepath.Base(c.Paths.Music[1]) != "b" {
		t.Fatalf("order not kept: %v", c.Paths.Music)
	}
}
