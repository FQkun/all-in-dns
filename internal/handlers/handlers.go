// Package handlers 实现 /api/* 路由，对齐原 PHP 版 routes.php。
package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"

	"github.com/loeia/all-in-dns/internal/auth"
	"github.com/loeia/all-in-dns/internal/config"
	"github.com/loeia/all-in-dns/internal/database"
	"github.com/loeia/all-in-dns/internal/httpapi"
	"github.com/loeia/all-in-dns/internal/logger"
	"github.com/loeia/all-in-dns/internal/redis"
	"github.com/loeia/all-in-dns/internal/sync"
)

// Handler 聚合各依赖。
type Handler struct {
	db     *database.DB
	redis  *redis.Client
	auth   *auth.Service
	sync   *sync.Service
	logger *logger.Logger
	cfg    *config.Config
}

// New 构造 Handler。
func New(db *database.DB, r *redis.Client, a *auth.Service, s *sync.Service, lg *logger.Logger, cfg *config.Config) *Handler {
	return &Handler{db: db, redis: r, auth: a, sync: s, logger: lg, cfg: cfg}
}

// apiError 业务错误（带 HTTP 状态码）。
type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

func failAPI(status int, msg string) error { return &apiError{Status: status, Msg: msg} }

// fail 处理错误响应：apiError 用指定状态码，其余按 500。
func (h *Handler) fail(w http.ResponseWriter, err error) {
	if ae, ok := err.(*apiError); ok {
		httpapi.Fail(w, ae.Status, ae.Msg)
		return
	}
	httpapi.Error(w, err)
}

// Routes 注册全部 API 路由。
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", h.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", h.handleLogout)
	mux.HandleFunc("GET /api/auth/me", h.handleMe)

	mux.HandleFunc("GET /api/accounts", h.listAccounts)
	mux.HandleFunc("POST /api/accounts", h.createAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", h.deleteAccount)
	mux.HandleFunc("POST /api/accounts/{id}/test", h.testAccount)

	mux.HandleFunc("GET /api/domains", h.listDomains)
	mux.HandleFunc("POST /api/domains", h.createDomain)
	mux.HandleFunc("POST /api/domains/{id}/verify", h.verifyDomain)
	mux.HandleFunc("POST /api/domains/{id}/repair", h.repairDomain)
	mux.HandleFunc("POST /api/domains/{id}/toggle", h.toggleDomain)
	mux.HandleFunc("GET /api/domains/{id}/probe", h.probeDomain)
	mux.HandleFunc("GET /api/domains/{id}/accounts", h.domainAccounts)
	mux.HandleFunc("POST /api/domains/{id}/bind", h.bindDomain)
	mux.HandleFunc("POST /api/domains/{id}/unbind", h.unbindDomain)
	mux.HandleFunc("DELETE /api/domains/{id}", h.deleteDomain)

	mux.HandleFunc("GET /api/domains/{id}/records", h.listRecords)
	mux.HandleFunc("POST /api/domains/{id}/records", h.createRecord)
	mux.HandleFunc("POST /api/domains/{id}/import-remote", h.importRemote)
	mux.HandleFunc("GET /api/domains/{id}/export", h.exportRecords)
	mux.HandleFunc("POST /api/domains/{id}/import", h.importRecords)
	mux.HandleFunc("POST /api/records/{id}", h.updateRecord)
	mux.HandleFunc("DELETE /api/records/{id}", h.deleteRecord)
	mux.HandleFunc("POST /api/records/batch-delete", h.batchDeleteRecords)

	mux.HandleFunc("GET /api/sync/logs", h.listSyncLogs)
	mux.HandleFunc("DELETE /api/sync/logs", h.clearSyncLogs)
	mux.HandleFunc("GET /api/sync/queue", h.listSyncQueue)
	mux.HandleFunc("POST /api/sync/retry-now", h.retryNow)
	mux.HandleFunc("DELETE /api/sync/queue", h.clearSyncQueue)

	mux.HandleFunc("GET /api/operation-logs", h.listOpLogs)
	mux.HandleFunc("DELETE /api/operation-logs", h.clearOpLogs)

	mux.HandleFunc("GET /api/backups", h.listBackups)
	mux.HandleFunc("POST /api/backups", h.createBackup)
	mux.HandleFunc("POST /api/backups/delete", h.deleteBackups)
	mux.HandleFunc("POST /api/backups/{id}/restore", h.restoreBackup)

	mux.HandleFunc("GET /api/monitor/summary", h.monitorSummary)

	// 定时任务 HTTP 入口（对齐原 cron.php）
	mux.HandleFunc("GET /cron", h.handleCron)

	return httpapi.Chain(mux, httpapi.Recover, httpapi.NoCache)
}

// ---- 记录输入校验（对齐 routes.php 闭包） ----

type recordInput struct {
	Sub      string
	Type     string
	Line     string
	Value    string
	TTL      int
	Priority *int
}

var (
	recordTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA"}
	reA         = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	reAAAA      = regexp.MustCompile(`^[0-9a-fA-F:]{2,39}$`)
	reCNAME     = regexp.MustCompile(`(?i)^([a-z0-9_*.-]+\.)+[a-z]{2,}\.?$`)
)

// checkRecordInput 校验记录输入（对齐 $checkRecordInput）。
func checkRecordInput(in map[string]any) (recordInput, string) {
	typ := strings.ToUpper(strings.TrimSpace(str(in["type"])))
	sub := strings.TrimSpace(str(in["sub"]))
	if sub == "" {
		sub = "@"
	}
	line := strings.TrimSpace(str(in["line"]))
	if line == "" {
		line = "默认"
	}
	value := strings.TrimSpace(str(in["value"]))
	ttl := intOr(in["ttl"], 600)
	err := ""

	switch {
	case !contains(recordTypes, typ):
		err = "不支持的记录类型"
	case value == "":
		err = "记录值不能为空"
	case utf8.RuneCountInString(value) > 1024:
		err = "记录值过长"
	case utf8.RuneCountInString(sub) > 253 || utf8.RuneCountInString(line) > 64:
		err = "主机记录或线路过长"
	case ttl < 60 || ttl > 86400:
		err = "TTL 需在 60~86400 秒之间"
	}

	if err == "" {
		switch typ {
		case "A":
			if !reA.MatchString(value) {
				err = "记录值格式不正确"
			}
		case "AAAA":
			if !reAAAA.MatchString(value) {
				err = "记录值格式不正确"
			}
		case "CNAME":
			if !reCNAME.MatchString(value) {
				err = "记录值格式不正确"
			}
		}
	}

	var priority *int
	if raw := in["priority"]; raw != nil && str(raw) != "" {
		p := intOr(raw, 0)
		priority = &p
	}
	if err == "" && typ == "MX" && (priority == nil || *priority < 0 || *priority > 65535) {
		err = "MX 优先级不合法"
	}
	if typ != "MX" {
		priority = nil
	}

	return recordInput{Sub: sub, Type: typ, Line: line, Value: value, TTL: ttl, Priority: priority}, err
}

// validateRecordInput 校验并返回输入，失败返回 apiError。
func validateRecordInput(in map[string]any) (recordInput, error) {
	ri, err := checkRecordInput(in)
	if err != "" {
		return ri, failAPI(http.StatusBadRequest, err)
	}
	return ri, nil
}

// validateRecordConflict 写入前冲突校验（对齐 $validateRecordConflict）。
func (h *Handler) validateRecordConflict(ctx context.Context, domainID int, sub, typ, line, value string, excludeID *int) error {
	typ = strings.ToUpper(strings.TrimSpace(typ))
	sub = strings.ToLower(strings.TrimSpace(sub))
	if sub == "" {
		sub = "@"
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = "默认"
	}

	if value != "" {
		var status string
		var err error
		if excludeID != nil {
			err = h.db.GetContext(ctx, &status,
				`SELECT status FROM records WHERE domain_id=? AND LOWER(sub_domain)=? AND type=? AND line=? AND value=? AND id<>?`,
				domainID, sub, typ, line, value, *excludeID)
		} else {
			err = h.db.GetContext(ctx, &status,
				`SELECT status FROM records WHERE domain_id=? AND LOWER(sub_domain)=? AND type=? AND line=? AND value=?`,
				domainID, sub, typ, line, value)
		}
		if err == nil && status != "" {
			if status == "disabled" {
				return failAPI(http.StatusConflict, "记录已存在（该记录待云端清理，请先在列表重试删除）")
			}
			return failAPI(http.StatusConflict, "记录已存在：同一主机 / 线路 / 类型下已有相同记录值")
		}
	}

	var conflictTypes []string
	switch typ {
	case "A", "AAAA":
		conflictTypes = []string{"CNAME"}
	case "CNAME":
		conflictTypes = []string{"A", "AAAA"}
	default:
		return nil
	}

	args := []any{domainID, sub, line}
	query, qargs, err := sqlx.In(`SELECT type FROM records WHERE domain_id=? AND LOWER(sub_domain)=? AND line=? AND status='active' AND type IN (?)`, domainID, sub, line, conflictTypes)
	_ = args
	if err != nil {
		return err
	}
	if excludeID != nil {
		query += " AND id<>?"
		qargs = append(qargs, *excludeID)
	}
	query = h.db.Rebind(query)
	var found string
	err = h.db.GetContext(ctx, &found, query, qargs...)
	if err == nil && found != "" {
		return failAPI(http.StatusConflict,
			"记录冲突："+sub+" / "+line+" 已存在 "+found+" 记录，不能与 "+typ+" 共存")
	}
	return nil
}

// ---- 工具函数 ----

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return ""
}

func intOr(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return def
		}
		return n
	}
	return def
}

func intFromPath(r *http.Request, name string) int {
	return intOr(r.PathValue(name), 0)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
