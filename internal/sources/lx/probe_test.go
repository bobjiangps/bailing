package lx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeRejectsNonAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json": // what kg "128k" answered with: a 39-byte error body
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":0,"error":"no such quality"}`))
		case "/tiny":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Header().Set("Content-Range", "bytes 0-1/39")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("ID"))
		case "/song":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Header().Set("Content-Range", "bytes 0-1/7950672")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("ID"))
		}
	}))
	defer srv.Close()
	m := &Manager{client: srv.Client()}
	ctx := context.Background()

	for _, p := range []string{"/json", "/tiny"} {
		if _, err := m.probe(ctx, srv.URL+p); err == nil || !strings.Contains(err.Error(), "dead link") {
			t.Errorf("probe(%s) err = %v, want a dead link", p, err)
		}
	}
	if size, err := m.probe(ctx, srv.URL+"/song"); err != nil || size != 7950672 {
		t.Errorf("probe(/song) = %d, %v", size, err)
	}
}
