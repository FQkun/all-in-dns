package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func intp(i int) *int { return &i }

func TestSpaceshipTTL(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 600}, {30, 60}, {60, 60}, {600, 600}, {3600, 3600}, {86400, 3600},
	}
	for _, c := range cases {
		if got := spaceshipTTL(c.in); got != c.want {
			t.Errorf("spaceshipTTL(%d)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestSpaceshipParseCAA(t *testing.T) {
	flag, tag, val := parseCAA(`0 issue "letsencrypt.org"`)
	if flag != 0 || tag != "issue" || val != "letsencrypt.org" {
		t.Fatalf("parseCAA = %d %q %q", flag, tag, val)
	}
	flag, tag, val = parseCAA("128 iodef mailto:a@b.com")
	if flag != 128 || tag != "iodef" || val != "mailto:a@b.com" {
		t.Fatalf("parseCAA = %d %q %q", flag, tag, val)
	}
}

func TestSpaceshipParseSRV(t *testing.T) {
	pri, weight, port, target := parseSRV("10 5 443 target.example.com")
	if pri != 10 || weight != 5 || port != 443 || target != "target.example.com" {
		t.Fatalf("parseSRV = %d %d %d %q", pri, weight, port, target)
	}
	svc, proto := splitSRVName("_sip._tcp")
	if svc != "_sip" || proto != "_tcp" {
		t.Fatalf("splitSRVName = %q %q", svc, proto)
	}
}

func TestSpaceshipItem(t *testing.T) {
	// A 记录用 address 字段
	item, err := spaceshipItem(NormalizedRecord{Sub: "www", Type: "A", Value: "1.2.3.4", TTL: 600})
	if err != nil {
		t.Fatal(err)
	}
	if item["address"] != "1.2.3.4" || item["type"] != "A" || item["name"] != "www" {
		t.Fatalf("A item = %+v", item)
	}
	if _, ok := item["value"]; ok {
		t.Fatal("A 记录不应有 value 字段")
	}

	// CNAME 用 cname 字段
	item, _ = spaceshipItem(NormalizedRecord{Sub: "cdn", Type: "CNAME", Value: "example.com.", TTL: 600})
	if item["cname"] != "example.com" {
		t.Fatalf("CNAME item = %+v", item)
	}

	// MX 用 exchange + preference
	item, _ = spaceshipItem(NormalizedRecord{Sub: "@", Type: "MX", Value: "mail.example.com.", TTL: 600, Priority: intp(20)})
	if item["exchange"] != "mail.example.com" || item["preference"] != 20 {
		t.Fatalf("MX item = %+v", item)
	}

	// TXT 用 value
	item, _ = spaceshipItem(NormalizedRecord{Sub: "@", Type: "TXT", Value: "v=spf1 -all", TTL: 600})
	if item["value"] != "v=spf1 -all" {
		t.Fatalf("TXT item = %+v", item)
	}

	// 不支持的类型
	if _, err := spaceshipItem(NormalizedRecord{Sub: "@", Type: "HTTPS", Value: "x"}); err == nil {
		t.Fatal("不支持的类型应报错")
	}
}

func TestSpaceshipToRemote(t *testing.T) {
	rr := spaceshipToRemote(map[string]any{
		"type": "MX", "name": "@", "exchange": "mail.example.com", "preference": float64(10), "ttl": float64(600),
	})
	if rr.Type != "MX" || rr.Value != "mail.example.com" || rr.Priority == nil || *rr.Priority != 10 {
		t.Fatalf("toRemote = %+v", rr)
	}
	if !strings.Contains(rr.ID, "mx") || !strings.Contains(rr.ID, "mail.example.com") {
		t.Fatalf("合成 ID 异常: %s", rr.ID)
	}
}

func TestSpaceshipBuildRequestAuth(t *testing.T) {
	var gotKey, gotSecret, gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		gotSecret = r.Header.Get("X-API-Secret")
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	}))
	defer server.Close()

	p := newSpaceship("mykey", "mysecret", nil)
	p.baseURL = server.URL

	if _, err := p.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotKey != "mykey" || gotSecret != "mysecret" {
		t.Fatalf("认证头错误: key=%q secret=%q", gotKey, gotSecret)
	}
	if gotMethod != "GET" || gotPath != "/domains" {
		t.Fatalf("请求错误: %s %s", gotMethod, gotPath)
	}
}

func TestSpaceshipListRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"items": [
				{"type":"A","name":"@","address":"1.2.3.4","ttl":600},
				{"type":"CNAME","name":"www","cname":"example.com","ttl":600},
				{"type":"NS","name":"@","nameserver":"ns1.example.com","ttl":3600}
			],
			"total": 3
		}`))
	}))
	defer server.Close()

	p := newSpaceship("k", "s", nil)
	p.baseURL = server.URL

	recs, err := p.ListRecords(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	// NS @ 应被跳过
	if len(recs) != 2 {
		t.Fatalf("期望 2 条（跳过 NS @），got %d: %+v", len(recs), recs)
	}
	if recs[0].Value != "1.2.3.4" || recs[1].Value != "example.com" {
		t.Fatalf("记录解析异常: %+v", recs)
	}
}

func TestSpaceshipSaveAndDeleteRequest(t *testing.T) {
	var lastMethod, lastBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	p := newSpaceship("k", "s", nil)
	p.baseURL = server.URL

	// create → PUT + force
	call, err := p.OpParams("create", "example.com", NormalizedRecord{Sub: "www", Type: "A", Value: "1.2.3.4", TTL: 600}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if call.Action != "saveRecords" {
		t.Fatalf("create action = %s", call.Action)
	}
	req, _ := p.BuildRequest(call.Action, call.Params)
	_, _, _, _ = p.Do(context.Background(), req)
	if lastMethod != "PUT" || !strings.Contains(lastBody, `"force":true`) || !strings.Contains(lastBody, `"address":"1.2.3.4"`) {
		t.Fatalf("save 请求异常: %s %s", lastMethod, lastBody)
	}

	// delete → DELETE
	call, _ = p.OpParams("delete", "example.com", NormalizedRecord{Sub: "www", Type: "A", Value: "1.2.3.4"}, nil)
	req, _ = p.BuildRequest(call.Action, call.Params)
	_, _, _, _ = p.Do(context.Background(), req)
	if lastMethod != "DELETE" || !strings.Contains(lastBody, `"address":"1.2.3.4"`) {
		t.Fatalf("delete 请求异常: %s %s", lastMethod, lastBody)
	}
}
