package provider

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------- 工具函数 ----------

func TestLineTo(t *testing.T) {
	cases := []struct {
		provider, in, want string
	}{
		{"tencent", "默认", "默认"},
		{"tencent", "电信", "电信"},
		{"aliyun", "默认", "default"},
		{"aliyun", "移动", "mobile"},
		{"volc", "默认", "default"},
		{"volc", "教育网", "edu"},
		{"tencent", "cn_telecom", "电信"},
		{"aliyun", "cmcc", "mobile"},
		{"tencent", "未知线路", "未知线路"}, // 未登记透传
	}
	for _, c := range cases {
		if got := LineTo(c.provider, c.in); got != c.want {
			t.Errorf("LineTo(%s,%q)=%q want %q", c.provider, c.in, got, c.want)
		}
	}
}

func TestLineFrom(t *testing.T) {
	cases := []struct {
		provider, in, want string
	}{
		{"tencent", "默认", "默认"},
		{"tencent", "电信", "电信"},
		{"aliyun", "default", "默认"},
		{"aliyun", "telecom", "电信"},
		{"volc", "oversea", "境外"},
		{"aliyun", "unknown-line", "unknown-line"},
		{"tencent", "", "默认"},
	}
	for _, c := range cases {
		if got := LineFrom(c.provider, c.in); got != c.want {
			t.Errorf("LineFrom(%s,%q)=%q want %q", c.provider, c.in, got, c.want)
		}
	}
}

func TestWriteTtl(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 600}, {60, 600}, {600, 600}, {3600, 3600}, {86400, 86400}, {99999, 86400},
	}
	for _, c := range cases {
		if got := writeTtl(c.in); got != c.want {
			t.Errorf("writeTtl(%d)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestWriteValue(t *testing.T) {
	cases := []struct{ typ, in, want string }{
		{"CNAME", "example.com.", "example.com"},
		{"MX", "mail.example.com.", "mail.example.com"},
		{"NS", "ns1.example.com.", "ns1.example.com"},
		{"A", "1.2.3.4", "1.2.3.4"},
		{"TXT", "v=spf1 -all", "v=spf1 -all"},
		{"CNAME", "example.com", "example.com"},
	}
	for _, c := range cases {
		if got := writeValue(c.typ, c.in); got != c.want {
			t.Errorf("writeValue(%s,%q)=%q want %q", c.typ, c.in, got, c.want)
		}
	}
}

func TestRawURLEncode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abcXYZ019", "abcXYZ019"},
		{"-_.~", "-_.~"},
		{"a b", "a%20b"},
		{"/", "%2F"},
		{"默认", "%E9%BB%98%E8%AE%A4"},
		{"a=b&c", "a%3Db%26c"},
	}
	for _, c := range cases {
		if got := rawURLEncode(c.in); got != c.want {
			t.Errorf("rawURLEncode(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

// ---------- 签名黄金用例（固定时钟，独立计算对比） ----------

const (
	testAK = "AKIDz8krbsJ5yKBZQpn74WFkmLPx3TEST"
	testSK = "Gu5t9xGARNpq86cd98joQYCN3TEST"
)

var fixedTime = time.Date(2019, 2, 25, 16, 44, 25, 0, time.UTC) // Unix 1551113065

func TestTencentSignature(t *testing.T) {
	p := newTencent(testAK, testSK, nil)
	p.now = func() time.Time { return fixedTime }

	params := map[string]any{"Domain": "example.com", "Limit": 30}
	req, err := p.BuildRequest("DescribeDomainList", params)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "POST" || req.URL != "https://dnspod.tencentcloudapi.com" {
		t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
	}

	// 独立计算签名
	ts := fixedTime.Unix()
	date := fixedTime.UTC().Format("2006-01-02")
	payload := string(req.Body)
	hashedBody := hexSHA256(payload)
	canonical := "POST\n/\n\n" +
		"content-type:application/json; charset=utf-8\n" +
		"host:dnspod.tencentcloudapi.com\n" +
		"x-tc-action:" + strings.ToLower("DescribeDomainList") + "\n\n" +
		"content-type;host;x-tc-action\n" + hashedBody
	credScope := date + "/dnspod/tc3_request"
	strToSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(ts, 10) + "\n" + credScope + "\n" + hexSHA256(canonical)

	kDate := hmacSHA256([]byte(date), []byte("TC3"+testSK))
	kService := hmacSHA256([]byte("dnspod"), kDate)
	kSigning := hmacSHA256([]byte("tc3_request"), kService)
	wantSig := hex.EncodeToString(hmacSHA256([]byte(strToSign), kSigning))

	// 校验 Authorization 头
	auth := findHeader(req.Headers, "Authorization")
	if !strings.Contains(auth, "Signature="+wantSig) {
		t.Errorf("signature mismatch\nwant %s\n got %s", wantSig, auth)
	}
	if !strings.Contains(auth, "Credential="+testAK+"/"+credScope) {
		t.Errorf("credential scope mismatch: %s", auth)
	}
	// 校验时间戳头
	if got := findHeader(req.Headers, "X-TC-Timestamp"); got != strconv.FormatInt(ts, 10) {
		t.Errorf("X-TC-Timestamp=%s want %d", got, ts)
	}
}

func TestAliyunSignature(t *testing.T) {
	p := newAliyun(testAK, testSK, nil)
	p.now = func() time.Time { return fixedTime }

	req, err := p.BuildRequest("DescribeDomainRecords", map[string]any{
		"DomainName": "example.com", "PageSize": 100, "PageNumber": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "alidns.aliyuncs.com" {
		t.Fatalf("unexpected host: %s", u.Host)
	}

	q := u.Query()
	gotSig := q.Get("Signature")
	q.Del("Signature")

	// 独立计算：排序参数 → canonical → strToSign → sig
	var pairs []string
	for k := range q {
		pairs = append(pairs, k)
	}
	// 简单排序（参数不多）
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j] < pairs[i] {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	var canonical strings.Builder
	for i, k := range pairs {
		if i > 0 {
			canonical.WriteByte('&')
		}
		canonical.WriteString(rawURLEncode(k))
		canonical.WriteByte('=')
		canonical.WriteString(rawURLEncode(q.Get(k)))
	}
	strToSign := "GET&" + rawURLEncode("/") + "&" + rawURLEncode(canonical.String())
	wantSig := base64.StdEncoding.EncodeToString(hmacSHA1([]byte(strToSign), []byte(testSK+"&")))

	if gotSig != wantSig {
		t.Errorf("signature mismatch\nwant %s\n got %s", wantSig, gotSig)
	}
	if q.Get("Action") != "DescribeDomainRecords" {
		t.Errorf("Action=%s", q.Get("Action"))
	}
	if q.Get("Version") != "2015-01-09" {
		t.Errorf("Version=%s", q.Get("Version"))
	}
}

func TestVolcSignature(t *testing.T) {
	p := newVolc(testAK, testSK, nil)
	p.now = func() time.Time { return fixedTime }

	// 读操作（GET query 签名）
	req, err := p.BuildRequest("ListRecords", map[string]any{"ZID": 100, "PageNumber": 1, "PageSize": 100})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "GET" {
		t.Fatalf("ListRecords should be GET, got %s", req.Method)
	}
	dt := fixedTime.UTC().Format("20060102T150405Z")
	if got := findHeader(req.Headers, "X-Date"); got != dt {
		t.Errorf("X-Date=%s want %s", got, dt)
	}
	auth := findHeader(req.Headers, "Authorization")
	if !strings.Contains(auth, "HMAC-SHA256 Credential="+testAK+"/") {
		t.Errorf("auth credential missing: %s", auth)
	}

	// 写操作（POST body 签名）
	req2, err := p.BuildRequest("CreateRecord", map[string]any{"Host": "@", "Type": "A", "Value": "1.2.3.4", "TTL": 600, "Line": "default"})
	if err != nil {
		t.Fatal(err)
	}
	if req2.Method != "POST" || req2.Body == nil {
		t.Fatalf("CreateRecord should be POST with body, got %s body=%v", req2.Method, req2.Body)
	}
	// X-Content-Sha256 应等于 body 的 sha256 hex
	bodyHash := findHeader(req2.Headers, "X-Content-Sha256")
	if bodyHash != hexSHA256(string(req2.Body)) {
		t.Errorf("X-Content-Sha256=%s want %s", bodyHash, hexSHA256(string(req2.Body)))
	}
}

func findHeader(headers []string, name string) string {
	for _, h := range headers {
		if i := strings.Index(h, ":"); i > 0 && strings.EqualFold(strings.TrimSpace(h[:i]), name) {
			return strings.TrimSpace(h[i+1:])
		}
	}
	return ""
}
