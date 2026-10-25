package proxy

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignVerify(t *testing.T) {
	s := NewSigner([]byte("0123456789abcdef0123456789abcdef"), "/bl", time.Hour)
	link := s.URL(Target{URL: "https://example.com/a.mp3", Title: "歌"})
	if !strings.HasPrefix(link, "/bl/proxy/stream?d=") {
		t.Fatalf("bad link %s", link)
	}
	u, _ := url.Parse(link)
	got, err := s.Verify(u.Query().Get("d"), u.Query().Get("s"))
	if err != nil || got.URL != "https://example.com/a.mp3" || got.Title != "歌" || got.Kind != KindAudio {
		t.Fatalf("verify: %+v %v", got, err)
	}
	if _, err := s.Verify(u.Query().Get("d"), "x"+u.Query().Get("s")[1:]); err == nil {
		t.Fatal("tampered signature accepted")
	}
	expired := s.URL(Target{URL: "https://example.com", Exp: time.Now().Add(-time.Minute).Unix()})
	u2, _ := url.Parse(expired)
	if _, err := s.Verify(u2.Query().Get("d"), u2.Query().Get("s")); err == nil {
		t.Fatal("expired link accepted")
	}
}

func TestRewritePlaylist(t *testing.T) {
	base, _ := url.Parse("https://cdn.example.com/live/index.m3u8")
	sign := func(abs, kind string) string { return "[" + kind + "]" + abs }

	media := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:10,\nseg1.ts\n#EXTINF:10,\n/abs/seg2.ts?x=1\n"
	out := RewritePlaylist(media, base, sign)
	for _, want := range []string{
		`URI="[s]https://cdn.example.com/live/key.bin"`,
		"[s]https://cdn.example.com/live/seg1.ts",
		"[s]https://cdn.example.com/abs/seg2.ts?x=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}

	master := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=64000\nlow/index.m3u8\n#EXT-X-MEDIA:TYPE=AUDIO,URI=\"aac/a.m3u8\"\n"
	out = RewritePlaylist(master, base, sign)
	if !strings.Contains(out, "[h]https://cdn.example.com/live/low/index.m3u8") ||
		!strings.Contains(out, `URI="[h]https://cdn.example.com/live/aac/a.m3u8"`) {
		t.Errorf("master rewrite wrong:\n%s", out)
	}
}
