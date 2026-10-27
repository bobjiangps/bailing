package lx

import "testing"

func TestStrictMismatch(t *testing.T) {
	mb := float64(1 << 20)
	k128 := int64(3.18 * mb)
	cases := []struct {
		name string
		size int64
		got  string
		ok   bool
	}{
		{"right file", int64(3.2 * mb), "128k", true},
		{"within 15%", int64(3.5 * mb), "", true},
		{"320k handed out for 128k", int64(7.58 * mb), "320k", false},
		{"other file", int64(1.2 * mb), "", false},
		{"size unknown", 0, "", false},
	}
	for _, c := range cases {
		why := strictMismatch(c.size, k128, c.got)
		if (why == "") != c.ok {
			t.Errorf("%s: strictMismatch = %q, want ok=%v", c.name, why, c.ok)
		}
	}
}
