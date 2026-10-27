package lyrics

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func manualService(t *testing.T) *Service {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/get/42":
			json.NewEncoder(w).Encode(lrclibRecord{ID: 42, TrackName: "晴天", ArtistName: "周杰伦",
				SyncedLyrics: "[00:01.00]故事的小黄花"})
			return
		case strings.HasPrefix(r.URL.Path, "/api/get/"):
			http.NotFound(w, r)
			return
		case q.Get("track_name") == "晴天" && q.Get("artist_name") == "周杰伦":
			json.NewEncoder(w).Encode([]lrclibRecord{
				{ID: 1, TrackName: "晴天", ArtistName: "周杰伦", Duration: 300, PlainLyrics: "live\nversion"},
				{ID: 2, TrackName: "晴天", ArtistName: "周杰伦", Duration: 269, PlainLyrics: "plain"},
			})
		case q.Get("track_name") == "晴天":
			json.NewEncoder(w).Encode([]lrclibRecord{
				{ID: 2, TrackName: "晴天", ArtistName: "周杰伦", Duration: 269, PlainLyrics: "plain"},
				{ID: 3, TrackName: "晴天", ArtistName: "周杰倫", Duration: 270,
					SyncedLyrics: "[00:01.00]故事的小黄花\n[00:05.00]从出生那年就飘着"},
				{ID: 4, TrackName: "晴天", ArtistName: "x", Duration: 270}, // no lyrics: dropped
			})
		case q.Get("q") != "":
			json.NewEncoder(w).Encode([]lrclibRecord{})
		default:
			json.NewEncoder(w).Encode([]lrclibRecord{})
		}
	}))
	t.Cleanup(srv.Close)
	return &Service{opt: Options{Online: true, Timeout: 5 * time.Second},
		lib: newLRCLib(srv.URL, 5*time.Second, "test"), log: slog.Default()}
}

func TestManualSearchMergesAndSorts(t *testing.T) {
	list, err := manualService(t).Search(context.Background(), "晴天", "周杰伦", "", 269_500)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	// same length first, synced before plain; the 300s live version last;
	// duplicates merged; records without lyrics dropped
	want := []int64{3, 2, 1}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
	if !strings.Contains(list[0].Preview, "故事的小黄花 / 从出生那年就飘着") || list[0].DurationMs != 270_000 {
		t.Fatalf("bad candidate: %+v", list[0])
	}
}

func TestManualSearchOffline(t *testing.T) {
	s := manualService(t)
	s.opt.Online = false
	if _, err := s.Search(context.Background(), "晴天", "", "", 0); err != ErrOffline {
		t.Fatalf("want ErrOffline, got %v", err)
	}
}

func TestFetchPick(t *testing.T) {
	s := manualService(t)
	status, body, err := s.fetchPick(context.Background(), 42)
	if err != nil || status != "found" || !strings.Contains(body, "小黄花") {
		t.Fatalf("got %q %q %v", status, body, err)
	}
	if r := resultOf(status, body); r.Status != "ok" || r.Source != "manual" || !r.Synced {
		t.Fatalf("bad result: %+v", r)
	}
	if _, _, err := s.fetchPick(context.Background(), 7); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
