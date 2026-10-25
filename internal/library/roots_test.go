package library

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRoots(t *testing.T) {
	a := filepath.FromSlash("/srv/music")
	b := filepath.FromSlash("/mnt/usb/songs")
	r := Roots{a, b}

	if r.Prefix(0) != "" {
		t.Fatalf("first root must stay unprefixed, got %q", r.Prefix(0))
	}
	p := r.Prefix(1)
	if !strings.HasPrefix(p, "@") || !strings.HasSuffix(p, "/") || len(p) != 10 {
		t.Fatalf("prefix = %q", p)
	}
	if got := r.Abs("x/y.mp3"); got != filepath.Join(a, "x", "y.mp3") {
		t.Fatalf("abs root0 = %q", got)
	}
	if got := r.Abs(p + "z.mp3"); got != filepath.Join(b, "z.mp3") {
		t.Fatalf("abs root1 = %q", got)
	}
	// prefix is stable when the roots are reordered
	if (Roots{a, "/other", b}).Prefix(2) != p {
		t.Fatal("prefix depends on position")
	}
	if r.Label(1) != "songs" {
		t.Fatalf("label = %q", r.Label(1))
	}
	if (Roots{}).Abs("a") != "" {
		t.Fatal("no roots must give empty path")
	}
}
