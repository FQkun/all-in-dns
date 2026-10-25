package handlers

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/loeia/all-in-dns/internal/httpapi"
	"github.com/loeia/all-in-dns/internal/notify"
)

var reDomain = regexp.MustCompile(`^([a-z0-9-]+\.)+[a-z]{2,}$`)

type domainRow struct {
	ID        int     `db:"id" json:"id"`
	Name      string  `db:"name" json:"name"`
	Status    string  `db:"status" json:"status"`
	Remark    *string `db:"remark" json:"remark"`
	CreatedAt string  `db:"created_at" json:"created_at"`
	Providers *string `db:"providers" json:"providers"`
}

func (h *Handler) listDomains(w http.ResponseWriter, r *http.Request) {
	u, ok := h.auth.RequireUser(r.Context(), w, r)
	if !ok {
		return
	}
	rows := make([]domainRow, 0)

	// 子账号隔离：非管理员只能看到 user_domains 中授权的域名
	ids := h.grantedDomainIDs(r.Context(), u)
	const baseSQL = `SELECT d.*, GROUP_CONCAT(CONCAT(dp.provider,':',IF(dp.sync_enabled=1,'on','off'))) AS providers
		FROM domains d LEFT JOIN domain_providers dp ON dp.domain_id = d.id`
	var err error
	switch {
	case ids == nil: // 不受限（admin 或 scope_all=1）
		err = h.db.SelectContext(r.Context(), &rows,
			baseSQL+` GROUP BY d.id ORDER BY d.id`)
	case len(ids) == 0: // 未授权任何域名
		httpapi.OK(w, rows)
		return
	default:
		query, args, e := sqlx.In(baseSQL+` WHERE d.id IN (?) GROUP BY d.id ORDER BY d.id`, ids)
		if e != nil {
			httpapi.Error(w, e)
			return
		}
		err = h.db.SelectContext(r.Context(), &rows, h.db.Rebind(query), args...)
	}
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	httpapi.OK(w, rows)
}

func (h *Handler) createDomain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// 新建域名涉及云账号与平台绑定，仅管理员可操作（子账号只能在授权域名内维护记录）
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	in := httpapi.Input(r)
	domain := strings.ToLower(strings.TrimSpace(str(in["name"])))
	if !reDomain.MatchString(domain) {
		httpapi.Fail(w, http.StatusBadRequest, "域名格式不正确")
		return
	}

	var exists int
	_ = h.db.GetContext(ctx, &exists, `SELECT id FROM domains WHERE name=?`, domain)
	if exists > 0 {
		httpapi.Fail(w, http.StatusBadRequest, "域名已存在")
		return
	}

	var remark any
	if v, ok := in["remark"]; ok {
		remark = str(v)
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO domains (name,remark) VALUES (?,?)`, domain, remark)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	domainID64, _ := res.LastInsertId()
	domainID := int(domainID64)

	// 绑定到各厂商账号并验证域名
	var accounts []struct {
		ID       int    `db:"id"`
		Provider string `db:"provider"`
	}
	_ = h.db.SelectContext(ctx, &accounts, `SELECT id,provider FROM cloud_accounts WHERE status=1`)
	for _, acc := range accounts {
		okStatus := "fail"
		msg := ""
		var pid string

		prov, err := h.sync.MakeProvider(ctx, acc.ID)
		if err != nil {
			msg = truncate(err.Error(), 250)
		} else {
			res, err := prov.CheckDomain(ctx, domain)
			if err != nil {
				msg = truncate(err.Error(), 250)
			} else {
				if res.OK {
					okStatus = "ok"
				}
				msg = res.Message
				pid = res.DomainID
			}
		}
		_, _ = h.db.ExecContext(ctx,
			`INSERT INTO domain_providers (domain_id,account_id,provider,verify_status,verify_message,last_verify_at) VALUES (?,?,?,?,?,NOW())`,
			domainID, acc.ID, acc.Provider, okStatus, msg)
		if pid != "" {
			_, _ = h.db.ExecContext(ctx,
				`UPDATE domain_providers SET provider_domain_id=? WHERE domain_id=? AND account_id=?`,
				pid, domainID, acc.ID)
		}
		if okStatus != "ok" && h.notifier != nil {
			h.notifier.Notify(ctx, notify.Event{
				Type: notify.EventVerifyFailed, Level: "warning",
				Domain: domain, Provider: acc.Provider, Message: msg,
			})
		}
	}

	h.logger.Op(ctx, &u.ID, u.Username, "domain.create", domain, nil, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"domain_id": domainID})
}

func (h *Handler) verifyDomain(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireRole(r.Context(), w, r, "admin", "operator"); !ok {
		return
	}
	id := intFromPath(r, "id")
	report, err := h.sync.VerifyDomain(r.Context(), id)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	httpapi.OK(w, report)
}

func (h *Handler) repairDomain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	in := httpapi.Input(r)
	accountID := intOr(in["account_id"], 0)
	rawIDs, _ := in["record_ids"].([]any)
	if accountID == 0 || len(rawIDs) == 0 {
		httpapi.Fail(w, http.StatusBadRequest, "参数错误")
		return
	}

	var bound int
	_ = h.db.GetContext(ctx, &bound,
		`SELECT 1 FROM domain_providers WHERE domain_id=? AND account_id=?`, id, accountID)
	if bound == 0 {
		httpapi.Fail(w, http.StatusBadRequest, "该平台未绑定到此域名")
		return
	}

	recordIDs := make([]int, 0, len(rawIDs))
	for _, v := range rawIDs {
		recordIDs = append(recordIDs, intOr(v, 0))
	}
	results, err := h.sync.PushToAccount(ctx, id, accountID, recordIDs)
	if err != nil {
		httpapi.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "record.repair", strconv.Itoa(id),
		map[string]any{"account": accountID, "count": len(recordIDs)}, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"sync": results})
}

func (h *Handler) toggleDomain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	in := httpapi.Input(r)
	enabled := 0
	if b, ok := in["enabled"].(bool); ok && b {
		enabled = 1
	}
	_, err := h.db.ExecContext(ctx,
		`UPDATE domain_providers SET sync_enabled=? WHERE domain_id=? AND account_id=?`,
		enabled, id, intOr(in["account_id"], 0))
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "domain.toggle", strconv.Itoa(id), in, httpapi.IP(r))
	httpapi.OK(w, nil)
}

func (h *Handler) deleteDomain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	err := h.db.InTx(func(tx *sqlx.Tx) error {
		stmts := []string{
			`DELETE q FROM sync_queue q JOIN records r ON r.id=q.record_id WHERE r.domain_id=?`,
			`DELETE rm FROM record_provider_map rm JOIN records r ON r.id=rm.record_id WHERE r.domain_id=?`,
			`DELETE FROM records WHERE domain_id=?`,
			`DELETE FROM domain_providers WHERE domain_id=?`,
			`DELETE FROM domains WHERE id=?`,
		}
		for _, sql := range stmts {
			if _, err := tx.ExecContext(ctx, sql, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "domain.delete", strconv.Itoa(id), nil, httpapi.IP(r))
	httpapi.OK(w, nil)
}

// bindDomain 给已有域名绑定一个云账号（补齐后新增账号无法绑定旧域名的问题）。
func (h *Handler) bindDomain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	in := httpapi.Input(r)
	accountID := intOr(in["account_id"], 0)
	if accountID == 0 {
		httpapi.Fail(w, http.StatusBadRequest, "参数错误")
		return
	}

	var domainName string
	err := h.db.GetContext(ctx, &domainName, `SELECT name FROM domains WHERE id=?`, id)
	if err != nil || domainName == "" {
		httpapi.Fail(w, http.StatusNotFound, "域名不存在")
		return
	}

	var acc struct {
		ID       int    `db:"id"`
		Provider string `db:"provider"`
	}
	err = h.db.GetContext(ctx, &acc, `SELECT id, provider FROM cloud_accounts WHERE id=?`, accountID)
	if err != nil {
		httpapi.Fail(w, http.StatusNotFound, "账号不存在")
		return
	}

	var bound int
	_ = h.db.GetContext(ctx, &bound, `SELECT 1 FROM domain_providers WHERE domain_id=? AND account_id=?`, id, accountID)
	if bound > 0 {
		httpapi.Fail(w, http.StatusBadRequest, "该账号已绑定到此域名")
		return
	}

	// 验证域名归属（失败也允许绑定，和创建域名时的逻辑一致）
	okStatus := "fail"
	msg := ""
	var pid string
	prov, err := h.sync.MakeProvider(ctx, accountID)
	if err != nil {
		msg = truncate(err.Error(), 250)
	} else {
		res, err := prov.CheckDomain(ctx, domainName)
		if err != nil {
			msg = truncate(err.Error(), 250)
		} else {
			if res.OK {
				okStatus = "ok"
			}
			msg = res.Message
			pid = res.DomainID
		}
	}

	_, err = h.db.ExecContext(ctx,
		`INSERT INTO domain_providers (domain_id,account_id,provider,verify_status,verify_message,last_verify_at) VALUES (?,?,?,?,?,NOW())`,
		id, accountID, acc.Provider, okStatus, msg)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			httpapi.Fail(w, http.StatusBadRequest, "该账号已绑定到此域名")
			return
		}
		httpapi.Error(w, err)
		return
	}
	if pid != "" {
		_, _ = h.db.ExecContext(ctx,
			`UPDATE domain_providers SET provider_domain_id=? WHERE domain_id=? AND account_id=?`,
			pid, id, accountID)
	}

	if okStatus != "ok" && h.notifier != nil {
		h.notifier.Notify(ctx, notify.Event{
			Type: notify.EventVerifyFailed, Level: "warning",
			Domain: domainName, Provider: acc.Provider, Message: msg,
		})
	}

	h.logger.Op(ctx, &u.ID, u.Username, "domain.bind", strconv.Itoa(id),
		map[string]any{"account": accountID, "provider": acc.Provider}, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"verify_status": okStatus, "verify_message": msg})
}

// domainAccounts 返回域名已绑定的账号列表（含验证状态）。
func (h *Handler) domainAccounts(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireUser(r.Context(), w, r); !ok {
		return
	}
	id := intFromPath(r, "id")
	type row struct {
		AccountID    int    `db:"account_id" json:"account_id"`
		AccountName  string `db:"account_name" json:"account_name"`
		Provider     string `db:"provider" json:"provider"`
		VerifyStatus string `db:"verify_status" json:"verify_status"`
		SyncEnabled  int    `db:"sync_enabled" json:"sync_enabled"`
	}
	rows := make([]row, 0)
	err := h.db.SelectContext(r.Context(), &rows, `
		SELECT dp.account_id, ca.name AS account_name, dp.provider, dp.verify_status, dp.sync_enabled
		FROM domain_providers dp JOIN cloud_accounts ca ON ca.id = dp.account_id
		WHERE dp.domain_id=? ORDER BY dp.id`, id)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	httpapi.OK(w, rows)
}

// unbindDomain 解除域名与云账号的绑定（清理该账号下此域名的队列与映射）。
func (h *Handler) unbindDomain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	in := httpapi.Input(r)
	accountID := intOr(in["account_id"], 0)
	if accountID == 0 {
		httpapi.Fail(w, http.StatusBadRequest, "参数错误")
		return
	}

	var bound int
	_ = h.db.GetContext(ctx, &bound, `SELECT 1 FROM domain_providers WHERE domain_id=? AND account_id=?`, id, accountID)
	if bound == 0 {
		httpapi.Fail(w, http.StatusBadRequest, "该账号未绑定到此域名")
		return
	}

	err := h.db.InTx(func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE q FROM sync_queue q JOIN records r ON r.id=q.record_id WHERE r.domain_id=? AND q.account_id=?`,
			id, accountID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE rm FROM record_provider_map rm JOIN records r ON r.id=rm.record_id WHERE r.domain_id=? AND rm.account_id=?`,
			id, accountID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM domain_providers WHERE domain_id=? AND account_id=?`, id, accountID)
		return err
	})
	if err != nil {
		httpapi.Error(w, err)
		return
	}

	h.logger.Op(ctx, &u.ID, u.Username, "domain.unbind", strconv.Itoa(id),
		map[string]any{"account": accountID}, httpapi.IP(r))
	httpapi.OK(w, nil)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
