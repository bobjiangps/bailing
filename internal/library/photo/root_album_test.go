package photo

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"bailing/internal/imaging"
	"bailing/internal/store"
)

func TestScanLoosePhotosInRoot(t *testing.T) {
	tmp := t.TempDir()
	rootA := filepath.Join(tmp, "photos")
	rootB := filepath.Join(tmp, "screenshots")
	write := func(p string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(rootA, "trip", "a.jpg")) // normal album
	write(filepath.Join(rootB, "s1.png"))        // loose photos in an extra root
	write(filepath.Join(rootB, "s2.png"))

	db, err := store.Open(filepath.Join(tmp, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := imaging.NewQueue(1)
	defer q.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	l := New(db, []string{rootA, rootB}, filepath.Join(tmp, "cache"), []string{"jpg", "png"},
		imaging.NewVips("", 0, 80), q, 300, 1200, log)

	if err := l.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	albums, err := l.Albums()
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, a := range albums {
		titles[a.Title] = a.ID
	}
	if _, ok := titles["trip"]; !ok {
		t.Fatalf("sub-folder album missing: %v", titles)
	}
	id, ok := titles["screenshots"]
	if !ok {
		t.Fatalf("root album missing: %v", titles)
	}
	_, photos, err := l.Album(id)
	if err != nil || len(photos) != 2 {
		t.Fatalf("root album photos = %d, %v", len(photos), err)
	}
	// stored paths must resolve back to the real files
	var rel string
	db.QueryRow(`SELECT rel_path FROM photos WHERE id=?`, photos[0].ID).Scan(&rel)
	if got := l.roots.Abs(rel); filepath.Dir(got) != rootB {
		t.Fatalf("rel %q resolves to %q", rel, got)
	}
}
