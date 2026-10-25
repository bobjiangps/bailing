package lyrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fake LRCLIB: no exact match; the artist search only knows a longer
// recording of "Pretty Boy"; the title-only search knows 可爱女人 by 周杰伦.
func fakeLRCLib(t *testing.T) *lrclib {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var out []lrclibRecord
		switch {
		case r.URL.Path == "/api/get":
			http.NotFound(w, r)
			return
		case q.Get("track_name") == "Pretty Boy" && q.Get("artist_name") == "M2M":
			out = []lrclibRecord{{TrackName: "Pretty Boy", ArtistName: "M2M", Duration: 279,
				SyncedLyrics: "[00:10.00]Line one\n[00:12.00]Line two"}}
		case q.Get("track_name") == "可爱女人" && q.Get("artist_name") == "":
			out = []lrclibRecord{{TrackName: "可爱女人", ArtistName: "周杰伦", Duration: 239,
				SyncedLyrics: "[00:20.00]想要有直升机"}}
		}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return newLRCLib(srv.URL, 5*time.Second, "test")
}

func TestLookupApproxDuration(t *testing.T) {
	rec, err := fakeLRCLib(t).lookup(context.Background(), "Pretty Boy", "M2M", "", 253_000)
	if err != nil || rec == nil {
		t.Fatalf("want approx match, got %v %v", rec, err)
	}
	if !rec.Approx || rec.SyncedLyrics != "" || !strings.Contains(rec.PlainLyrics, "Line two") ||
		strings.Contains(rec.PlainLyrics, "[00:") {
		t.Fatalf("approx match must be plain text only: %+v", rec)
	}
}

func TestLookupTitleOnlyFallback(t *testing.T) {
	rec, err := fakeLRCLib(t).lookup(context.Background(), "可爱女人", "周杰倫", "", 238_367)
	if err != nil || rec == nil || rec.Approx || rec.SyncedLyrics == "" {
		t.Fatalf("want synced match via title-only search, got %+v %v", rec, err)
	}
	// a different length must not be accepted from the title-only search
	if rec, _ := fakeLRCLib(t).lookup(context.Background(), "可爱女人", "周杰倫", "", 300_000); rec != nil {
		t.Fatalf("title-only search must respect the duration: %+v", rec)
	}
}
