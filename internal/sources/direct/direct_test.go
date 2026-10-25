package direct

import "testing"

func TestPlaylistEntries(t *testing.T) {
	if got := firstM3U("#EXTM3U\n#EXTINF:-1,Radio\n\nhttp://s.example.com:8000/live\n"); got != "http://s.example.com:8000/live" {
		t.Errorf("m3u: %q", got)
	}
	if got := firstPLS("[playlist]\nNumberOfEntries=1\nFile1=http://s.example.com/x\nTitle1=X\n"); got != "http://s.example.com/x" {
		t.Errorf("pls: %q", got)
	}
}

func TestTitleFromURL(t *testing.T) {
	if got := TitleFromURL("https://x.com/music/%E6%99%B4%E5%A4%A9.mp3?k=1"); got != "晴天" {
		t.Errorf("got %q", got)
	}
	if got := TitleFromURL("https://radio.example.com/"); got != "radio.example.com" {
		t.Errorf("got %q", got)
	}
}
