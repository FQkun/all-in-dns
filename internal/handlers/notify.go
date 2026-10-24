package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/loeia/all-in-dns/internal/httpapi"
	"github.com/loeia/all-in-dns/internal/notify"
)

var channelTypes = []string{"wecom", "feishu", "dingtalk", "webhook"}

type notifyChannelRow struct {
	ID        int    `db:"id"`
	Name      string `db:"name"`
	Type      string `db:"type"`
	Config    string `db:"config"`
	Events    string `db:"events"`
	Enabled   int    `db:"enabled"`
	CreatedAt string `db:"created_at"`
}

// listNotifyEvents 返回可用事件类型（供前端选择）。
func (h *Handler) listNotifyEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireRole(r.Context(), w, r, "admin"); !ok {
		return
	}
	out := make([]map[string]string, 0, len(notify.EventLabels))
	for t, label := range notify.EventLabels {
		if t == notify.EventTest {
			continue
		}
		out = append(out, map[string]string{"type": t, "label": label})
	}
	httpapi.OK(w, out)
}

func (h *Handler) listNotifyChannels(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireRole(r.Context(), w, r, "admin"); !ok {
		return
	}
	rows := make([]notifyChannelRow, 0)
	err := h.db.SelectContext(r.Context(), &rows,
		`SELECT id,name,type,config,events,enabled,created_at FROM notify_channels ORDER BY id`)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var cfg struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal([]byte(row.Config), &cfg)
		var events []string
		_ = json.Unmarshal([]byte(row.Events), &events)
		out = append(out, map[string]any{
			"id":         row.ID,
			"name":       row.Name,
			"type":       row.Type,
			"url_masked": maskURL(cfg.URL),
			"events":     events,
			"enabled":    row.Enabled,
			"created_at": row.CreatedAt,
		})
	}
	httpapi.OK(w, out)
}

func (h *Handler) createNotifyChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	in := httpapi.Input(r)
	name := str(in["name"])
	typ := str(in["type"])
	url := str(in["url"])
	if name == "" || url == "" {
		httpapi.Fail(w, http.StatusBadRequest, "名称和 Webhook URL 不能为空")
		return
	}
	if !contains(channelTypes, typ) {
		httpapi.Fail(w, http.StatusBadRequest, "无效的渠道类型")
		return
	}
	eventsJSON := normalizeEvents(in["events"])
	configJSON, _ := json.Marshal(map[string]string{"url": url})

	if _, err := h.db.ExecContext(ctx,
		`INSERT INTO notify_channels (name,type,config,events,enabled) VALUES (?,?,?,?,1)`,
		name, typ, string(configJSON), eventsJSON); err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "notify.create", name, map[string]any{"type": typ}, httpapi.IP(r))
	httpapi.OK(w, nil)
}

func (h *Handler) updateNotifyChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	in := httpapi.Input(r)

	// 只更新允许的字段
	sets := make([]string, 0)
	args := make([]any, 0)
	if v := str(in["name"]); v != "" {
		sets = append(sets, "name=?")
		args = append(args, v)
	}
	if v := str(in["url"]); v != "" {
		configJSON, _ := json.Marshal(map[string]string{"url": v})
		sets = append(sets, "config=?")
		args = append(args, string(configJSON))
	}
	if _, exists := in["events"]; exists {
		sets = append(sets, "events=?")
		args = append(args, normalizeEvents(in["events"]))
	}
	if v, exists := in["enabled"]; exists {
		sets = append(sets, "enabled=?")
		if b, ok := v.(bool); ok && b {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if len(sets) == 0 {
		httpapi.Fail(w, http.StatusBadRequest, "没有可更新的字段")
		return
	}
	args = append(args, id)
	if _, err := h.db.ExecContext(ctx, "UPDATE notify_channels SET "+joinComma(sets)+" WHERE id=?", args...); err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "notify.update", strconv.Itoa(id), in, httpapi.IP(r))
	httpapi.OK(w, nil)
}

func (h *Handler) deleteNotifyChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	if _, err := h.db.ExecContext(ctx, `DELETE FROM notify_channels WHERE id=?`, id); err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "notify.delete", strconv.Itoa(id), nil, httpapi.IP(r))
	httpapi.OK(w, nil)
}

// testNotifyChannel 用该渠道发一条测试消息。
func (h *Handler) testNotifyChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := h.auth.RequireRole(ctx, w, r, "admin"); !ok {
		return
	}
	id := intFromPath(r, "id")
	var row notifyChannelRow
	if err := h.db.GetContext(ctx, &row,
		`SELECT id,name,type,config,events,enabled,created_at FROM notify_channels WHERE id=?`, id); err != nil {
		httpapi.Fail(w, http.StatusNotFound, "渠道不存在")
		return
	}
	ch := notify.Channel{ID: row.ID, Name: row.Name, Type: row.Type, Config: row.Config, Events: row.Events, Enabled: row.Enabled}
	ev := notify.Event{
		Type:    notify.EventTest,
		Level:   "info",
		Message: "这是一条来自 DNS 统一管理平台的测试消息，收到说明通知配置正确。",
	}
	if h.notifier == nil {
		httpapi.Fail(w, http.StatusServiceUnavailable, "通知服务未初始化")
		return
	}
	if err := h.notifier.Send(ctx, ch, ev); err != nil {
		httpapi.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	httpapi.OK(w, nil)
}

// ---- 工具 ----

// normalizeEvents 把前端传来的事件数组转成 JSON（空表示订阅全部）。
func normalizeEvents(v any) string {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		b, _ := json.Marshal([]string{"all"})
		return string(b)
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		out = append(out, "all")
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// maskURL 脱敏 Webhook URL（企业微信/钉钉的 key 在 URL 里）。
func maskURL(u string) string {
	if u == "" {
		return ""
	}
	if len(u) <= 24 {
		return u[:min(8, len(u))] + "****"
	}
	return u[:len(u)-12] + "****" + u[len(u)-4:]
}

// joinComma 用逗号连接（用于 SQL SET 片段）。
func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
