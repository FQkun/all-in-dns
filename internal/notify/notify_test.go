package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChannelWants(t *testing.T) {
	all := &Channel{Events: `["all"]`}
	if !all.Wants(EventSyncFailed) || !all.Wants(EventBackupDone) {
		t.Fatal("all 应匹配所有事件")
	}
	partial := &Channel{Events: `["sync_failed"]`}
	if !partial.Wants(EventSyncFailed) {
		t.Fatal("应匹配 sync_failed")
	}
	if partial.Wants(EventBackupDone) {
		t.Fatal("不应匹配未订阅事件")
	}
}

func TestWecomContent(t *testing.T) {
	ev := Event{
		Type: EventSyncFailed, Level: "error",
		Domain: "example.com", Provider: "aliyun",
		Record: "www CNAME example.com", Message: "DomainRecordDuplicate",
		Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	c := wecomContent(ev)
	for _, want := range []string{"同步失败", "example.com", "阿里云", "DomainRecordDuplicate", "2026-01-02 03:04:05"} {
		if !strings.Contains(c, want) {
			t.Errorf("内容缺少 %q:\n%s", want, c)
		}
	}
}

func TestPlainContent(t *testing.T) {
	ev := Event{Type: EventVerifyFailed, Level: "warning", Domain: "a.com", Provider: "tencent", Message: "域名不存在"}
	c := plainContent(ev)
	if !strings.Contains(c, "域名验证失败") || !strings.Contains(c, "腾讯云") || !strings.Contains(c, "a.com") {
		t.Fatalf("plain 内容异常: %s", c)
	}
}

func TestDedupKey(t *testing.T) {
	a := dedupKey(Event{Type: EventSyncFailed, Domain: "a.com", Provider: "tencent", Message: "x"})
	b := dedupKey(Event{Type: EventSyncFailed, Domain: "a.com", Provider: "tencent", Message: "x"})
	c := dedupKey(Event{Type: EventSyncFailed, Domain: "b.com", Provider: "tencent", Message: "x"})
	if a != b {
		t.Fatal("相同事件应得到相同去重键")
	}
	if a == c {
		t.Fatal("不同域名应得到不同去重键")
	}
}

func TestSendWecom(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	n := &Notifier{http: server.Client()}
	ch := Channel{Type: "wecom", Config: `{"url":"` + server.URL + `"}`}
	ev := Event{Type: EventTest, Level: "info", Message: "测试"}
	if err := n.Send(context.Background(), ch, ev); err != nil {
		t.Fatal(err)
	}
	if got["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v", got["msgtype"])
	}
	md, _ := got["markdown"].(map[string]any)
	if !strings.Contains(md["content"].(string), "测试消息") {
		t.Fatalf("content = %v", md["content"])
	}
}

func TestSendWecomError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer server.Close()

	n := &Notifier{http: server.Client()}
	ch := Channel{Type: "wecom", Config: `{"url":"` + server.URL + `"}`}
	if err := n.Send(context.Background(), ch, Event{Type: EventTest}); err == nil {
		t.Fatal("企业微信返回错误时应报错")
	}
}

func TestSendWebhook(t *testing.T) {
	var got Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(200)
	}))
	defer server.Close()

	n := &Notifier{http: server.Client()}
	ch := Channel{Type: "webhook", Config: `{"url":"` + server.URL + `"}`}
	if err := n.Send(context.Background(), ch, Event{Type: EventSyncFailed, Domain: "a.com", Message: "x"}); err != nil {
		t.Fatal(err)
	}
	if got.Domain != "a.com" || got.Type != EventSyncFailed {
		t.Fatalf("webhook 收到的事件异常: %+v", got)
	}
}

func TestUnknownChannelType(t *testing.T) {
	n := &Notifier{http: http.DefaultClient}
	if err := n.Send(context.Background(), Channel{Type: "nope", Config: "{}"}, Event{Type: EventTest}); err == nil {
		t.Fatal("未知渠道类型应报错")
	}
}
