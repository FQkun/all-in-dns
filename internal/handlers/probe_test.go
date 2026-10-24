package handlers

import (
	"testing"

	"github.com/loeia/all-in-dns/internal/dnsprobe"
)

func TestNormalizeValue(t *testing.T) {
	cases := []struct{ typ, in, want string }{
		{"A", "1.2.3.4", "1.2.3.4"},
		{"AAAA", "2001:DB8::1", "2001:db8::1"},
		{"CNAME", "Example.COM.", "example.com"},
		{"MX", "10 Mail.Example.com.", "mail.example.com"},
		{"TXT", `"v=spf1 -all"`, "v=spf1 -all"},
		{"NS", "ns1.example.com.", "ns1.example.com"},
	}
	for _, c := range cases {
		if got := normalizeValue(c.typ, c.in); got != c.want {
			t.Errorf("normalizeValue(%s,%q)=%q want %q", c.typ, c.in, got, c.want)
		}
	}
}

func TestProbeVerdict(t *testing.T) {
	ok := []dnsprobe.Result{
		{Name: "a", Status: "ok", Records: []string{"1.2.3.4"}},
		{Name: "b", Status: "ok", Records: []string{"1.2.3.4"}},
	}
	if v := probeVerdict([]string{"1.2.3.4"}, ok, "A"); v != "ok" {
		t.Fatalf("全一致应为 ok，got %s", v)
	}

	mismatch := []dnsprobe.Result{
		{Name: "a", Status: "ok", Records: []string{"1.2.3.4"}},
		{Name: "b", Status: "ok", Records: []string{"5.6.7.8"}},
	}
	if v := probeVerdict([]string{"1.2.3.4"}, mismatch, "A"); v != "mismatch" {
		t.Fatalf("部分不一致应为 mismatch，got %s", v)
	}

	nx := []dnsprobe.Result{
		{Name: "a", Status: "nxdomain", Records: []string{}},
		{Name: "b", Status: "nxdomain", Records: []string{}},
	}
	if v := probeVerdict([]string{"1.2.3.4"}, nx, "A"); v != "nxdomain" {
		t.Fatalf("全部不存在应为 nxdomain，got %s", v)
	}

	errs := []dnsprobe.Result{
		{Name: "a", Status: "error"},
		{Name: "b", Status: "error"},
	}
	if v := probeVerdict([]string{"1.2.3.4"}, errs, "A"); v != "error" {
		t.Fatalf("全部失败应为 error，got %s", v)
	}
}

func TestFQDN(t *testing.T) {
	cases := []struct{ sub, domain, want string }{
		{"@", "example.com", "example.com"},
		{"", "example.com", "example.com"},
		{"www", "example.com", "www.example.com"},
	}
	for _, c := range cases {
		if got := dnsprobe.FQDN(c.sub, c.domain); got != c.want {
			t.Errorf("FQDN(%q,%q)=%q want %q", c.sub, c.domain, got, c.want)
		}
	}
}
