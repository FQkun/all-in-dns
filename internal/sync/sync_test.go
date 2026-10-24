package sync

import "testing"

func TestNormLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "默认"},
		{"默认", "默认"},
		{"default", "默认"},
		{"DEFAULT", "默认"},
		{" Default ", "默认"},
		{"电信", "电信"},
		{"oversea", "oversea"},
	}
	for _, c := range cases {
		if got := NormLine(c.in); got != c.want {
			t.Errorf("NormLine(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestRecKey(t *testing.T) {
	cases := []struct {
		sub, typ, value, line, want string
	}{
		{"@", "A", "1.2.3.4", "默认", "@|A|1.2.3.4|默认"},
		{"www", "cname", "Example.COM.", "default", "www|CNAME|example.com|默认"},
		{"@", "MX", "10 mail.example.com.", "默认", "@|MX|mail.example.com|默认"},
		{"@", "mx", "mail.example.com.", "默认", "@|MX|mail.example.com|默认"},
		{"@", "TXT", "v=spf1 -all", "默认", "@|TXT|v=spf1 -all|默认"},
		{"api", "AAAA", "2001:DB8::1", "电信", "api|AAAA|2001:db8::1|电信"},
		{"@", "NS", "ns1.Example.COM.", "default", "@|NS|ns1.example.com|默认"},
	}
	for _, c := range cases {
		if got := RecKey(c.sub, c.typ, c.value, c.line); got != c.want {
			t.Errorf("RecKey(%q,%q,%q,%q)=%q want %q", c.sub, c.typ, c.value, c.line, got, c.want)
		}
	}
}

func TestNormalizeRecord(t *testing.T) {
	pri := 10
	rec := &recordRow{
		SubDomain: "", Type: "A", Line: "", Value: "1.2.3.4", TTL: 600, Priority: &pri,
	}
	n := normalizeRecord(rec)
	if n.Sub != "@" || n.Line != "默认" || n.Priority == nil || *n.Priority != 10 {
		t.Fatalf("normalizeRecord mismatch: %+v", n)
	}
}

func TestCompactInts(t *testing.T) {
	got := compactInts([]int{0, 1, 0, 2, 3})
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("compactInts len=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("compactInts=%v want %v", got, want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("你好世界", 2); got != "你好" {
		t.Errorf("truncateRunes=%q want %q", got, "你好")
	}
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Errorf("truncateRunes=%q want %q", got, "abc")
	}
}
