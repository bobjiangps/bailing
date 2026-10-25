package music

import "testing"

func TestGuessTitle(t *testing.T) {
	cases := []struct {
		base       string
		titleFirst bool
		title      string
		artist     string
	}{
		{"01 - 周杰倫 - 可爱女人.mp3", false, "可爱女人", "周杰倫"},
		{"可爱女人 - 周杰倫.mp3", true, "可爱女人", "周杰倫"},
		{"可爱女人 - 周杰倫.mp3", false, "周杰倫", "可爱女人"},
		{"Song - Live - M2M.mp3", true, "Song - Live", "M2M"},
		{"03 Just a title.flac", true, "Just a title", ""},
	}
	for _, c := range cases {
		artist := ""
		if got := guessTitle(c.base, &artist, c.titleFirst); got != c.title || artist != c.artist {
			t.Errorf("%q (titleFirst=%v): got %q / %q, want %q / %q", c.base, c.titleFirst, got, artist, c.title, c.artist)
		}
	}
	artist := "Tagged"
	if got := guessTitle("A - B.mp3", &artist, false); got != "B" || artist != "Tagged" {
		t.Errorf("tagged artist must be kept: %q / %q", got, artist)
	}
}

func TestOrderVote(t *testing.T) {
	if v := orderVote("Pretty Boy - M2M.mp3", "Pretty Boy", "M2M"); v != 1 {
		t.Errorf("title first: %d", v)
	}
	if v := orderVote("02 - m2m - pretty boy.mp3", "Pretty Boy", "M2M"); v != -1 {
		t.Errorf("artist first: %d", v)
	}
	if v := orderVote("whatever.mp3", "Pretty Boy", "M2M"); v != 0 {
		t.Errorf("no evidence: %d", v)
	}
	if v := orderVote("A - B.mp3", "X", "Y"); v != 0 {
		t.Errorf("unrelated name: %d", v)
	}
}
