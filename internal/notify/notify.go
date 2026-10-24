// Package notify 提供多渠道消息通知（企业微信/飞书/钉钉/通用 Webhook）。
package notify

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/loeia/all-in-dns/internal/database"
	"github.com/loeia/all-in-dns/internal/redis"
)

// 事件类型。
const (
	EventSyncFailed   = "sync_failed"
	EventVerifyFailed = "verify_failed"
	EventBackupDone   = "backup_done"
	EventTest         = "test"
)

// EventLabels 事件中文名。
var EventLabels = map[string]string{
	EventSyncFailed:   "同步失败",
	EventVerifyFailed: "域名验证失败",
	EventBackupDone:   "自动备份完成",
	EventTest:         "测试消息",
}

// Event 通知事件。
type Event struct {
	Type     string    `json:"type"`
	Level    string    `json:"level"` // info | warning | error | critical
	Domain   string    `json:"domain,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Record   string    `json:"record,omitempty"`
	Message  string    `json:"message,omitempty"`
	Time     time.Time `json:"time"`
}

// Channel 通知渠道。
type Channel struct {
	ID      int    `db:"id" json:"id"`
	Name    string `db:"name" json:"name"`
	Type    string `db:"type" json:"type"`
	Config  string `db:"config" json:"-"`
	Events  string `db:"events" json:"-"`
	Enabled int    `db:"enabled" json:"enabled"`
}

// EventList 渠道订阅的事件列表。
func (c *Channel) EventList() []string {
	var out []string
	_ = json.Unmarshal([]byte(c.Events), &out)
	return out
}

// Wants 该渠道是否订阅此事件。
func (c *Channel) Wants(eventType string) bool {
	for _, e := range c.EventList() {
		if e == eventType || e == "all" {
			return true
		}
	}
	return false
}

// Notifier 通知发送器。
type Notifier struct {
	db    *database.DB
	redis *redis.Client
	http  *http.Client
}

// New 构造 Notifier。
func New(db *database.DB, r *redis.Client) *Notifier {
	return &Notifier{
		db:    db,
		redis: r,
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
		},
	}
}

// Notify 异步发送通知（去重 + 多渠道），不阻塞调用方。
func (n *Notifier) Notify(ctx context.Context, ev Event) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	if ev.Level == "" {
		ev.Level = "error"
	}
	go n.deliver(context.Background(), ev)
}

func (n *Notifier) deliver(ctx context.Context, ev Event) {
	// 去重：同 type+domain+provider+message 5 分钟内只发一次（避免重试刷屏）
	if n.redis != nil {
		key := "notify:dedup:" + dedupKey(ev)
		if v, err := n.redis.Get(ctx, key); err == nil && v != "" {
			return
		}
		_ = n.redis.Set(ctx, key, "1", 5*time.Minute)
	}

	channels := make([]Channel, 0)
	if err := n.db.SelectContext(ctx, &channels,
		`SELECT id,name,type,config,events,enabled FROM notify_channels WHERE enabled=1`); err != nil {
		slog.Error("查询通知渠道失败", "err", err)
		return
	}
	for _, ch := range channels {
		if !ch.Wants(ev.Type) {
			continue
		}
		if err := n.send(ctx, ch, ev); err != nil {
			slog.Error("发送通知失败", "channel", ch.Name, "type", ch.Type, "err", err)
		}
	}
}

// Send 同步发送到指定渠道（用于「测试」按钮，不去重）。
func (n *Notifier) Send(ctx context.Context, ch Channel, ev Event) error {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	if ev.Level == "" {
		ev.Level = "info"
	}
	return n.send(ctx, ch, ev)
}

func (n *Notifier) send(ctx context.Context, ch Channel, ev Event) error {
	switch ch.Type {
	case "wecom":
		return n.sendWecom(ctx, ch, ev)
	case "feishu":
		return n.sendFeishu(ctx, ch, ev)
	case "dingtalk":
		return n.sendDingtalk(ctx, ch, ev)
	case "webhook":
		return n.sendWebhook(ctx, ch, ev)
	}
	return fmt.Errorf("未知渠道类型: %s", ch.Type)
}

// ---- 渠道适配 ----

func (n *Notifier) sendWecom(ctx context.Context, ch Channel, ev Event) error {
	var cfg struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(ch.Config), &cfg); err != nil {
		return err
	}
	if cfg.URL == "" {
		return fmt.Errorf("企业微信 Webhook URL 未配置")
	}

	body, _ := json.Marshal(map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": wecomContent(ev)},
	})
	resp, err := n.postJSON(ctx, cfg.URL, body)
	if err != nil {
		return err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return fmt.Errorf("解析企业微信响应失败: %w", err)
	}
	if r.ErrCode != 0 {
		return fmt.Errorf("企业微信返回 %d: %s", r.ErrCode, r.ErrMsg)
	}
	return nil
}

func (n *Notifier) sendFeishu(ctx context.Context, ch Channel, ev Event) error {
	var cfg struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(ch.Config), &cfg); err != nil {
		return err
	}
	if cfg.URL == "" {
		return fmt.Errorf("飞书 Webhook URL 未配置")
	}

	body, _ := json.Marshal(map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": plainContent(ev)},
	})
	resp, err := n.postJSON(ctx, cfg.URL, body)
	if err != nil {
		return err
	}
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(resp, &r)
	if r.Code != 0 {
		return fmt.Errorf("飞书返回 %d: %s", r.Code, r.Msg)
	}
	return nil
}

func (n *Notifier) sendDingtalk(ctx context.Context, ch Channel, ev Event) error {
	var cfg struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(ch.Config), &cfg); err != nil {
		return err
	}
	if cfg.URL == "" {
		return fmt.Errorf("钉钉 Webhook URL 未配置")
	}

	title := EventLabels[ev.Type]
	if title == "" {
		title = ev.Type
	}
	body, _ := json.Marshal(map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"title": title, "text": wecomContent(ev)},
	})
	resp, err := n.postJSON(ctx, cfg.URL, body)
	if err != nil {
		return err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(resp, &r)
	if r.ErrCode != 0 {
		return fmt.Errorf("钉钉返回 %d: %s", r.ErrCode, r.ErrMsg)
	}
	return nil
}

func (n *Notifier) sendWebhook(ctx context.Context, ch Channel, ev Event) error {
	var cfg struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(ch.Config), &cfg); err != nil {
		return err
	}
	if cfg.URL == "" {
		return fmt.Errorf("Webhook URL 未配置")
	}
	body, _ := json.Marshal(ev)
	_, err := n.postJSON(ctx, cfg.URL, body)
	return err
}

func (n *Notifier) postJSON(ctx context.Context, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	return data, nil
}

// ---- 消息格式化 ----

// wecomContent 企业微信 markdown（也用于钉钉）。
func wecomContent(ev Event) string {
	title := EventLabels[ev.Type]
	if title == "" {
		title = ev.Type
	}
	color := "warning"
	if ev.Level == "info" {
		color = "info"
	}
	var sb strings.Builder
	sb.WriteString("## " + title + "\n")
	if ev.Domain != "" {
		sb.WriteString("> 域名：**" + ev.Domain + "**\n")
	}
	if ev.Provider != "" {
		sb.WriteString("> 平台：" + providerLabel(ev.Provider) + "\n")
	}
	if ev.Record != "" {
		sb.WriteString("> 记录：" + ev.Record + "\n")
	}
	if ev.Message != "" {
		sb.WriteString("> 原因：<font color=\"" + color + "\">" + ev.Message + "</font>\n")
	}
	sb.WriteString("> 时间：" + ev.Time.Format("2006-01-02 15:04:05"))
	return sb.String()
}

func plainContent(ev Event) string {
	title := EventLabels[ev.Type]
	if title == "" {
		title = ev.Type
	}
	var parts []string
	parts = append(parts, "【"+title+"】")
	if ev.Domain != "" {
		parts = append(parts, "域名："+ev.Domain)
	}
	if ev.Provider != "" {
		parts = append(parts, "平台："+providerLabel(ev.Provider))
	}
	if ev.Record != "" {
		parts = append(parts, "记录："+ev.Record)
	}
	if ev.Message != "" {
		parts = append(parts, "原因："+ev.Message)
	}
	parts = append(parts, ev.Time.Format("2006-01-02 15:04:05"))
	return strings.Join(parts, "\n")
}

func providerLabel(p string) string {
	switch p {
	case "tencent":
		return "腾讯云"
	case "aliyun":
		return "阿里云"
	case "volc":
		return "火山引擎"
	case "spaceship":
		return "Spaceship"
	}
	return p
}

func dedupKey(ev Event) string {
	sum := sha1.Sum([]byte(ev.Type + "|" + ev.Domain + "|" + ev.Provider + "|" + ev.Message))
	return hex.EncodeToString(sum[:8])
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
