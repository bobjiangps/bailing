package lx

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bailing/internal/netguard"
)

// a script whose musicUrl answer is always the same link
const fixedURLScript = `/**
 * @name __NAME__
 */
const { EVENT_NAMES, on, send } = globalThis.lx
on(EVENT_NAMES.request, () => Promise.resolve('__URL__'))
send(EVENT_NAMES.inited, { sources: { kw: { name: '酷我', type: 'music', actions: ['musicUrl'], qualitys: ['128k', '320k'] } } })
`

// Source A hands out a 39-byte error body, source B the real file: the
// progress must say A is broken, then switch to B and download from B.
func TestResolveReportsSourceSteps(t *testing.T) {
	audio := bytes.Repeat([]byte{0xff}, 1<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad.mp3" {
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, `{"code":403,"msg":"no permission"}`)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		http.ServeContent(w, r, "ok.mp3", time.Time{}, bytes.NewReader(audio))
	}))
	defer srv.Close()
	base := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	client := netguard.NewClient(netguard.Options{AllowPrivate: true, Timeout: 5 * time.Second})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(t.TempDir(), nil, client, 5*time.Second, time.Minute, log)
	for name, path := range map[string]string{"A": "/bad.mp3", "B": "/ok.mp3"} {
		src := strings.NewReplacer("__NAME__", name, "__URL__", base+path).Replace(fixedURLScript)
		vm, err := Start(src, ParseHeader(src), client, 5*time.Second, log)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()
		m.scripts[name] = &Script{Name: name, Status: "ok", Sources: vm.Sources(), vm: vm}
	}

	mi := map[string]any{"songmid": "1", "_types": map[string]any{"128k": map[string]any{"size": "1.00MB"}}}
	var steps []Attempt
	ctx := WithProgress(WithStrict(context.Background()), func(a Attempt) { steps = append(steps, a) })
	u, q, err := m.Resolve(ctx, "kw", "128k", mi)
	if err != nil || q != "128k" || !strings.HasSuffix(u, "/ok.mp3") {
		t.Fatalf("resolve = %q %q %v", u, q, err)
	}
	want := []string{"A trying", "A bad dead", "B trying", "B ok"}
	var got []string
	for _, s := range steps {
		got = append(got, strings.TrimSpace(s.Script+" "+s.Status+" "+s.Kind))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	if steps[3].Bytes != 1<<20 {
		t.Errorf("ok step bytes = %d", steps[3].Bytes)
	}

	// the second download comes from the cache and still names the source
	steps = nil
	if _, _, err := m.Resolve(ctx, "kw", "128k", mi); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Script != "B" || !steps[0].Cached {
		t.Fatalf("cached steps = %+v", steps)
	}
}
