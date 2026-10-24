package logger

import "testing"

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 10, "abc"},
		{"abcdef", 3, "abc"},
		{"你好世界", 2, "你好"},
		{"", 5, ""},
		{"hello", 0, ""},
	}
	for _, c := range cases {
		if got := truncateRunes(c.in, c.n); got != c.want {
			t.Errorf("truncateRunes(%q,%d)=%q want %q", c.in, c.n, got, c.want)
		}
	}
}
