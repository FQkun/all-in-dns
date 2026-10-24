package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/loeia/all-in-dns/internal/httpapi"
)

type recordRow struct {
	ID         int     `db:"id" json:"id"`
	DomainID   int     `db:"domain_id" json:"domain_id"`
	SubDomain  string  `db:"sub_domain" json:"sub_domain"`
	Type       string  `db:"type" json:"type"`
	Line       string  `db:"line" json:"line"`
	Value      string  `db:"value" json:"value"`
	TTL        int     `db:"ttl" json:"ttl"`
	Priority   *int    `db:"priority" json:"priority"`
	Remark     *string `db:"remark" json:"remark"`
	Status     string  `db:"status" json:"status"`
	UpdatedAt  string  `db:"updated_at" json:"updated_at"`
	SyncStates *string `db:"sync_states" json:"sync_states"`
}

func (h *Handler) listRecords(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireUser(r.Context(), w, r); !ok {
		return
	}
	id := intFromPath(r, "id")
	page := intOr(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	size := intOr(r.URL.Query().Get("size"), 100)
	if size < 20 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	off := (page - 1) * size

	var total int
	_ = h.db.GetContext(r.Context(), &total, `SELECT COUNT(*) c FROM records WHERE domain_id=?`, id)

	rows := make([]recordRow, 0)
	err := h.db.SelectContext(r.Context(), &rows, `
		SELECT r.id, r.domain_id, r.sub_domain, r.type, r.line, r.value, r.ttl, r.priority, r.remark, r.status, r.updated_at,
		       GROUP_CONCAT(DISTINCT CONCAT(m.account_id, '|', COALESCE(m.provider, ca.provider), '|', m.sync_status)) AS sync_states
		FROM records r
		LEFT JOIN record_provider_map m ON m.record_id = r.id
		LEFT JOIN cloud_accounts ca ON ca.id = m.account_id
		WHERE r.domain_id=? GROUP BY r.id ORDER BY r.type, r.sub_domain, r.id LIMIT ? OFFSET ?`,
		id, size, off)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	httpapi.OK(w, map[string]any{"items": rows, "total": total, "page": page, "size": size})
}

func (h *Handler) createRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	in := httpapi.Input(r)
	ri, err := validateRecordInput(in)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.validateRecordConflict(ctx, id, ri.Sub, ri.Type, ri.Line, ri.Value, nil); err != nil {
		h.fail(w, err)
		return
	}

	var remark any
	if v, ok := in["remark"]; ok {
		remark = str(v)
	}
	res, err := h.db.ExecContext(ctx,
		`INSERT INTO records (domain_id,sub_domain,type,line,value,ttl,priority,remark) VALUES (?,?,?,?,?,?,?,?)`,
		id, ri.Sub, ri.Type, ri.Line, ri.Value, ri.TTL, ri.Priority, remark)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	recordID, _ := res.LastInsertId()

	results, err := h.sync.Dispatch(ctx, int(recordID), "create")
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "record.create", strconv.Itoa(int(recordID)), in, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"record_id": recordID, "sync": results})
}

func (h *Handler) updateRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	in := httpapi.Input(r)

	var domainID int
	err := h.db.GetContext(ctx, &domainID, `SELECT domain_id FROM records WHERE id=?`, id)
	if err != nil || domainID == 0 {
		httpapi.Fail(w, http.StatusNotFound, "记录不存在")
		return
	}
	ri, err := validateRecordInput(in)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.validateRecordConflict(ctx, domainID, ri.Sub, ri.Type, ri.Line, ri.Value, &id); err != nil {
		h.fail(w, err)
		return
	}

	var remark any
	if v, ok := in["remark"]; ok {
		remark = str(v)
	}
	_, err = h.db.ExecContext(ctx,
		`UPDATE records SET sub_domain=?,type=?,line=?,value=?,ttl=?,priority=?,remark=?,status='active' WHERE id=?`,
		ri.Sub, ri.Type, ri.Line, ri.Value, ri.TTL, ri.Priority, remark, id)
	if err != nil {
		httpapi.Error(w, err)
		return
	}

	results, err := h.sync.Dispatch(ctx, id, "update")
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "record.update", strconv.Itoa(id), in, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"sync": results})
}

func (h *Handler) deleteRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	id := intFromPath(r, "id")

	results, err := h.sync.Dispatch(ctx, id, "delete")
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	failed := false
	for _, res := range results {
		if res.Status != "success" {
			failed = true
			break
		}
	}
	if !failed {
		_, _ = h.db.ExecContext(ctx, `DELETE FROM record_provider_map WHERE record_id=?`, id)
		_, _ = h.db.ExecContext(ctx, `DELETE FROM records WHERE id=?`, id)
	} else {
		_, _ = h.db.ExecContext(ctx, `UPDATE records SET status='disabled' WHERE id=?`, id)
	}
	h.logger.Op(ctx, &u.ID, u.Username, "record.delete", strconv.Itoa(id), nil, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"sync": results, "pending": failed})
}

func (h *Handler) batchDeleteRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	in := httpapi.Input(r)
	rawIDs, _ := in["ids"].([]any)
	if len(rawIDs) == 0 {
		httpapi.Fail(w, http.StatusBadRequest, "ids 不能为空")
		return
	}
	ids := make([]int, 0, len(rawIDs))
	for _, v := range rawIDs {
		ids = append(ids, intOr(v, 0))
	}

	var pending []int
	for _, id := range ids {
		results, err := h.sync.Dispatch(ctx, id, "delete")
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		hasFailed := false
		for _, res := range results {
			if res.Status != "success" {
				hasFailed = true
				break
			}
		}
		if hasFailed {
			_, _ = h.db.ExecContext(ctx, `UPDATE records SET status='disabled' WHERE id=?`, id)
			pending = append(pending, id)
			continue
		}
		_, _ = h.db.ExecContext(ctx, `DELETE FROM record_provider_map WHERE record_id=?`, id)
		_, _ = h.db.ExecContext(ctx, `DELETE FROM records WHERE id=?`, id)
	}
	idStrs := make([]string, len(ids))
	for i, v := range ids {
		idStrs[i] = strconv.Itoa(v)
	}
	h.logger.Op(ctx, &u.ID, u.Username, "record.batch_delete", strings.Join(idStrs, ","), nil, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"pending": pending})
}

// ---- 导出 ----

func (h *Handler) exportRecords(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireUser(r.Context(), w, r); !ok {
		return
	}
	id := intFromPath(r, "id")
	type row struct {
		SubDomain string `db:"sub_domain"`
		Type      string `db:"type"`
		Line      string `db:"line"`
		Value     string `db:"value"`
		TTL       int    `db:"ttl"`
		Priority  *int   `db:"priority"`
	}
	rows := make([]row, 0)
	if err := h.db.SelectContext(r.Context(), &rows,
		`SELECT sub_domain,type,line,value,ttl,priority FROM records WHERE domain_id=?`, id); err != nil {
		httpapi.Error(w, err)
		return
	}

	var sb strings.Builder
	sb.WriteString("sub_domain,type,line,value,ttl,priority\n")
	for _, rec := range rows {
		pri := ""
		if rec.Priority != nil {
			pri = strconv.Itoa(*rec.Priority)
		}
		fields := []string{rec.SubDomain, rec.Type, rec.Line, rec.Value, strconv.Itoa(rec.TTL), pri}
		var quoted []string
		for _, f := range fields {
			quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
		}
		sb.WriteString(strings.Join(quoted, ",") + "\n")
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"records_%d.csv\"", id))
	_, _ = w.Write([]byte("\xEF\xBB\xBF" + sb.String()))
}

// ---- 导入 ----

func (h *Handler) importRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	in := httpapi.Input(r)
	csvText := str(in["csv"])
	if csvText == "" {
		httpapi.Fail(w, http.StatusBadRequest, "csv 内容为空")
		return
	}
	if len(csvText) > 512*1024 {
		httpapi.Fail(w, http.StatusBadRequest, "单次导入不得超过 512KB，请分批")
		return
	}
	if !utf8.ValidString(csvText) {
		httpapi.Fail(w, http.StatusBadRequest, "文件编码需为 UTF-8")
		return
	}
	csvText = strings.TrimPrefix(csvText, "\xEF\xBB\xBF")

	reader := csv.NewReader(strings.NewReader(csvText))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		httpapi.Fail(w, http.StatusBadRequest, "缺少表头")
		return
	}
	idx := map[string]int{}
	for i, hcol := range header {
		idx[strings.TrimSpace(hcol)] = i
	}
	if _, ok1 := idx["sub_domain"]; !ok1 {
		httpapi.Fail(w, http.StatusBadRequest, "表头需包含 sub_domain,type,value")
		return
	}
	if _, ok2 := idx["type"]; !ok2 {
		httpapi.Fail(w, http.StatusBadRequest, "表头需包含 sub_domain,type,value")
		return
	}
	if _, ok3 := idx["value"]; !ok3 {
		httpapi.Fail(w, http.StatusBadRequest, "表头需包含 sub_domain,type,value")
		return
	}

	count, skipped := 0, 0
	var queue []int
	for {
		cols, err := reader.Read()
		if err != nil {
			break // EOF 或错误均结束
		}
		// 空行跳过
		hasValue := false
		for _, c := range cols {
			if strings.TrimSpace(c) != "" {
				hasValue = true
				break
			}
		}
		if !hasValue {
			continue
		}

		get := func(name, def string) string {
			if i, ok := idx[name]; ok && i < len(cols) {
				return cols[i]
			}
			return def
		}
		row := map[string]any{
			"sub":      get("sub_domain", "@"),
			"type":     get("type", ""),
			"line":     get("line", "默认"),
			"value":    get("value", ""),
			"ttl":      get("ttl", "600"),
			"priority": get("priority", ""),
		}
		ri, rowErr := checkRecordInput(row)
		if rowErr != "" {
			skipped++
			continue
		}
		if ri.Type == "NS" && strings.ToLower(ri.Sub) == "@" {
			skipped++
			continue
		}
		var dupID int
		_ = h.db.GetContext(ctx, &dupID,
			`SELECT id FROM records WHERE domain_id=? AND LOWER(sub_domain)=? AND type=? AND line=? AND value=?`,
			id, strings.ToLower(ri.Sub), ri.Type, ri.Line, ri.Value)
		if dupID > 0 {
			skipped++
			continue
		}
		if err := h.validateRecordConflict(ctx, id, ri.Sub, ri.Type, ri.Line, ri.Value, nil); err != nil {
			skipped++
			continue
		}
		res, err := h.db.ExecContext(ctx,
			`INSERT INTO records (domain_id,sub_domain,type,line,value,ttl,priority) VALUES (?,?,?,?,?,?,?)`,
			id, ri.Sub, ri.Type, ri.Line, ri.Value, ri.TTL, ri.Priority)
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		rid, _ := res.LastInsertId()
		queue = append(queue, int(rid))
		count++
	}

	results, err := h.sync.DispatchMany(ctx, queue, "create")
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	failed := 0
	for _, res := range results {
		if res.Status != "success" {
			failed++
		}
	}
	h.logger.Op(ctx, &u.ID, u.Username, "record.import", strconv.Itoa(id),
		map[string]any{"count": count, "skipped": skipped, "failed": failed}, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"imported": count, "skipped": skipped, "failed": failed, "sync": results})
}
