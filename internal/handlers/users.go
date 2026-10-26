package handlers

// 子账号与域名级权限（二次开发新增）
//
// 权限模型：
//   - admin          超级管理员，天然可见/可操作全部域名，且是唯一能管理子账号与审计日志的角色
//   - operator       员工账号（可选写）：只能看到并修改 user_domains 中授权的域名
//   - viewer         只读员工账号：在授权域名内只读
//
// scope_all=1 表示"不限域名"（历史账号迁移时的默认值，保持向后兼容），
// 新建子账号时 scope_all=0 并显式授权具体域名。

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/loeia/all-in-dns/internal/auth"
	"github.com/loeia/all-in-dns/internal/httpapi"
)

var (
	reDomainPath = regexp.MustCompile(`^/api/domains/(\d+)`)
	reRecordPath = regexp.MustCompile(`^/api/records/(\d+)`)
	reUserPath   = regexp.MustCompile(`^/api/users/(\d+)`)
	reUsername   = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)
)

// 子账号角色白名单
var userRoles = []string{"admin", "operator", "viewer"}

type userRow struct {
	ID          int     `db:"id" json:"id"`
	Username    string  `db:"username" json:"username"`
	Role        string  `db:"role" json:"role"`
	ScopeAll    int     `db:"scope_all" json:"scope_all"`
	CreatedAt   string  `db:"created_at" json:"created_at"`
	LastLoginAt *string `db:"last_login_at" json:"last_login_at"`
}

type grantRow struct {
	UserID   int    `db:"user_id"`
	DomainID int    `db:"domain_id"`
	Name     string `db:"name"`
}

// ---------- 权限判定 ----------

// canAccessDomain 判断用户是否可访问指定域名。
func (h *Handler) canAccessDomain(ctx context.Context, u *auth.User, domainID int) bool {
	if u == nil || domainID <= 0 {
		return false
	}
	if u.Role == "admin" {
		return true
	}
	var scopeAll int
	if err := h.db.GetContext(ctx, &scopeAll, `SELECT scope_all FROM users WHERE id=?`, u.ID); err != nil {
		if err == sql.ErrNoRows {
			return false
		}
		// 列不存在（未执行迁移）时按"全部可见"降级，避免把所有人锁在门外
		return true
	}
	if scopeAll == 1 {
		return true
	}
	var hit int
	err := h.db.GetContext(ctx, &hit,
		`SELECT 1 FROM user_domains WHERE user_id=? AND domain_id=?`, u.ID, domainID)
	return err == nil && hit == 1
}

// grantedDomainIDs 返回用户被授权的域名 ID（admin 或 scope_all 返回 nil 表示不受限）。
func (h *Handler) grantedDomainIDs(ctx context.Context, u *auth.User) []int {
	if u == nil {
		return []int{}
	}
	if u.Role == "admin" {
		return nil
	}
	var scopeAll int
	if err := h.db.GetContext(ctx, &scopeAll, `SELECT scope_all FROM users WHERE id=?`, u.ID); err != nil {
		return nil // 迁移未执行，降级为不受限
	}
	if scopeAll == 1 {
		return nil
	}
	ids := []int{}
	_ = h.db.SelectContext(ctx, &ids, `SELECT domain_id FROM user_domains WHERE user_id=?`, u.ID)
	return ids
}

// ScopeGuard 域名级越权拦截：
// 任何 /api/domains/{id}/... 或 /api/records/{id} 请求，非管理员必须是授权域名。
func (h *Handler) ScopeGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		var domainID int
		if m := reDomainPath.FindStringSubmatch(path); m != nil {
			domainID, _ = strconv.Atoi(m[1])
		} else if m := reRecordPath.FindStringSubmatch(path); m != nil {
			if rid, _ := strconv.Atoi(m[1]); rid > 0 {
				_ = h.db.GetContext(r.Context(), &domainID, `SELECT domain_id FROM records WHERE id=?`, rid)
			}
		}
		if domainID == 0 {
			next.ServeHTTP(w, r)
			return
		}
		u, err := h.auth.ParseToken(r.Context(), auth.TokenFromRequest(r))
		if err != nil || u == nil {
			next.ServeHTTP(w, r) // 未登录交给下游返回 401
			return
		}
		if !h.canAccessDomain(r.Context(), u, domainID) {
			httpapi.Fail(w, http.StatusForbidden, "无权访问该域名")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- 子账号管理 ----------

// listUsers 子账号列表（含授权域名），仅 admin。
func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireRole(r.Context(), w, r, "admin"); !ok {
		return
	}
	rows := []userRow{}
	if err := h.db.SelectContext(r.Context(), &rows,
		`SELECT id, username, role, scope_all, created_at, last_login_at FROM users ORDER BY id`); err != nil {
		httpapi.Error(w, err)
		return
	}
	grants := []grantRow{}
	_ = h.db.SelectContext(r.Context(), &grants,
		`SELECT ud.user_id, ud.domain_id, d.name FROM user_domains ud JOIN domains d ON d.id=ud.domain_id ORDER BY d.name`)

	byUser := map[int][]grantRow{}
	for _, g := range grants {
		byUser[g.UserID] = append(byUser[g.UserID], g)
	}

	out := make([]map[string]any, 0, len(rows))
	for _, u := range rows {
		ids := make([]int, 0, 4)
		names := make([]string, 0, 4)
		for _, g := range byUser[u.ID] {
			ids = append(ids, g.DomainID)
			names = append(names, g.Name)
		}
		out = append(out, map[string]any{
			"id": u.ID, "username": u.Username, "role": u.Role,
			"scope_all": u.ScopeAll, "created_at": u.CreatedAt, "last_login_at": u.LastLoginAt,
			"domain_ids": ids, "domain_names": names,
		})
	}
	httpapi.OK(w, out)
}

// createUser 新建子账号，仅 admin。
func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	in := httpapi.Input(r)
	username := strings.TrimSpace(str(in["username"]))
	password := str(in["password"])
	role := "operator"
	if v := strings.TrimSpace(str(in["role"])); v != "" {
		role = v
	}
	if !reUsername.MatchString(username) {
		httpapi.Fail(w, http.StatusBadRequest, "用户名需为 3~32 位字母、数字、下划线、点或短横线")
		return
	}
	if len([]rune(password)) < 6 {
		httpapi.Fail(w, http.StatusBadRequest, "密码至少 6 位")
		return
	}
	if !contains(userRoles, role) {
		httpapi.Fail(w, http.StatusBadRequest, "无效的角色")
		return
	}
	var exists int
	_ = h.db.GetContext(ctx, &exists, `SELECT id FROM users WHERE username=?`, username)
	if exists > 0 {
		httpapi.Fail(w, http.StatusBadRequest, "用户名已被占用")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	scopeAll := 0
	if b, ok := in["scope_all"].(bool); ok && b {
		scopeAll = 1
	}
	res, err := h.db.ExecContext(ctx,
		`INSERT INTO users (username,password_hash,role,scope_all) VALUES (?,?,?,?)`,
		username, string(hash), role, scopeAll)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	newID64, _ := res.LastInsertId()
	newID := int(newID64)

	if scopeAll == 0 {
		h.replaceGrants(ctx, newID, in["domain_ids"])
	}
	h.logger.Op(ctx, &u.ID, u.Username, "user.create", username,
		map[string]any{"role": role, "scope_all": scopeAll, "domains": in["domain_ids"]}, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"user_id": newID})
}

// updateUser 修改子账号（角色 / 密码 / 域名授权），仅 admin。
func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	op, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	in := httpapi.Input(r)

	var target userRow
	if err := h.db.GetContext(ctx, &target,
		`SELECT id, username, role, scope_all, created_at, last_login_at FROM users WHERE id=?`, id); err != nil {
		httpapi.Fail(w, http.StatusNotFound, "账号不存在")
		return
	}

	if v := strings.TrimSpace(str(in["role"])); v != "" {
		if !contains(userRoles, v) {
			httpapi.Fail(w, http.StatusBadRequest, "无效的角色")
			return
		}
		// 不允许把最后一个管理员降级
		if target.Role == "admin" && v != "admin" {
			var admins int
			_ = h.db.GetContext(ctx, &admins, `SELECT COUNT(*) FROM users WHERE role='admin'`)
			if admins <= 1 {
				httpapi.Fail(w, http.StatusBadRequest, "至少保留一个管理员账号")
				return
			}
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE users SET role=? WHERE id=?`, v, id); err != nil {
			httpapi.Error(w, err)
			return
		}
	}

	if pw := str(in["password"]); pw != "" {
		if len([]rune(pw)) < 6 {
			httpapi.Fail(w, http.StatusBadRequest, "密码至少 6 位")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, string(hash), id); err != nil {
			httpapi.Error(w, err)
			return
		}
	}

	if v, ok := in["scope_all"].(bool); ok {
		scope := 0
		if v {
			scope = 1
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE users SET scope_all=? WHERE id=?`, scope, id); err != nil {
			httpapi.Error(w, err)
			return
		}
	}
	if ids, ok := in["domain_ids"]; ok {
		h.replaceGrants(ctx, id, ids)
	}

	h.logger.Op(ctx, &op.ID, op.Username, "user.update", target.Username, in, httpapi.IP(r))
	httpapi.OK(w, nil)
}

// deleteUser 删除子账号，仅 admin；不能删自己，也不能删掉最后一个管理员。
func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	op, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	if id == op.ID {
		httpapi.Fail(w, http.StatusBadRequest, "不能删除当前登录的账号")
		return
	}
	var target userRow
	if err := h.db.GetContext(ctx, &target,
		`SELECT id, username, role, scope_all, created_at, last_login_at FROM users WHERE id=?`, id); err != nil {
		httpapi.Fail(w, http.StatusNotFound, "账号不存在")
		return
	}
	if target.Role == "admin" {
		var admins int
		_ = h.db.GetContext(ctx, &admins, `SELECT COUNT(*) FROM users WHERE role='admin'`)
		if admins <= 1 {
			httpapi.Fail(w, http.StatusBadRequest, "至少保留一个管理员账号")
			return
		}
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM user_domains WHERE user_id=?`, id); err != nil {
		httpapi.Error(w, err)
		return
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id); err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &op.ID, op.Username, "user.delete", target.Username, nil, httpapi.IP(r))
	httpapi.OK(w, nil)
}

// replaceGrants 覆盖式写入用户的域名授权。
func (h *Handler) replaceGrants(ctx context.Context, userID int, raw any) {
	if _, err := h.db.ExecContext(ctx, `DELETE FROM user_domains WHERE user_id=?`, userID); err != nil {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	for _, v := range list {
		did := intOr(v, 0)
		if did <= 0 {
			continue
		}
		_, _ = h.db.ExecContext(ctx,
			`INSERT IGNORE INTO user_domains (user_id,domain_id) VALUES (?,?)`, userID, did)
	}
}

// ---------- 审计日志 ----------

// auditLogs 操作日志查询（按用户 / 动作 / 关键字 / 时间筛选），仅 admin。
func (h *Handler) auditLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireRole(r.Context(), w, r, "admin"); !ok {
		return
	}
	q := r.URL.Query()
	page := intOr(q.Get("page"), 1)
	if page < 1 {
		page = 1
	}
	size := intOr(q.Get("size"), 50)
	if size < 10 {
		size = 10
	}
	if size > 200 {
		size = 200
	}

	where := []string{"1=1"}
	args := []any{}
	if v := strings.TrimSpace(q.Get("username")); v != "" {
		where = append(where, "username=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.Get("action")); v != "" {
		where = append(where, "action LIKE ?")
		args = append(args, v+"%")
	}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		where = append(where, "(target LIKE ? OR detail LIKE ?)")
		args = append(args, "%"+v+"%", "%"+v+"%")
	}
	if v := strings.TrimSpace(q.Get("from")); v != "" {
		where = append(where, "created_at >= ?")
		args = append(args, v+" 00:00:00")
	}
	if v := strings.TrimSpace(q.Get("to")); v != "" {
		where = append(where, "created_at <= ?")
		args = append(args, v+" 23:59:59")
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := h.db.GetContext(r.Context(), &total,
		`SELECT COUNT(*) FROM operation_logs WHERE `+cond, args...); err != nil {
		httpapi.Error(w, err)
		return
	}

	type auditRow struct {
		ID        int     `db:"id" json:"id"`
		UserID    *int    `db:"user_id" json:"user_id"`
		Username  *string `db:"username" json:"username"`
		Action    string  `db:"action" json:"action"`
		Target    *string `db:"target" json:"target"`
		Detail    *string `db:"detail" json:"detail"`
		IP        *string `db:"ip" json:"ip"`
		CreatedAt string  `db:"created_at" json:"created_at"`
	}
	items := []auditRow{}
	args = append(args, size, (page-1)*size)
	err := h.db.SelectContext(r.Context(), &items,
		`SELECT id,user_id,username,action,target,detail,ip,created_at
		   FROM operation_logs WHERE `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		httpapi.Error(w, err)
		return
	}

	// 顺带返回可选的动作类型（供前端下拉）
	var actions []string
	_ = h.db.SelectContext(r.Context(), &actions,
		`SELECT DISTINCT action FROM operation_logs ORDER BY action`)

	httpapi.OK(w, map[string]any{
		"items": items, "total": total, "page": page, "size": size, "actions": actions,
	})
}

// 编译期兜底：确保 reUserPath 被使用（保留给后续 /users/{id} 精确匹配）
var _ = reUserPath
