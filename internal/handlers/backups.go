package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/loeia/all-in-dns/internal/httpapi"
)

type backupRow struct {
	ID          int     `db:"id" json:"id"`
	Name        string  `db:"name" json:"name"`
	RecordCount int     `db:"record_count" json:"record_count"`
	CreatedBy   *string `db:"created_by" json:"created_by"`
	CreatedAt   string  `db:"created_at" json:"created_at"`
}

func (h *Handler) listBackups(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireRole(r.Context(), w, r, "admin"); !ok {
		return
	}
	rows := make([]backupRow, 0)
	if err := h.db.SelectContext(r.Context(), &rows,
		`SELECT id,name,record_count,created_by,created_at FROM backups ORDER BY id DESC LIMIT 100`); err != nil {
		httpapi.Error(w, err)
		return
	}
	httpapi.OK(w, rows)
}

// snapshot 生成当前核心表的 JSON 快照（对齐备份结构 schema_version=2）。
func (h *Handler) snapshot(ctx context.Context) (map[string]any, error) {
	snap := map[string]any{
		"schema_version": 2,
		"backed_at":      time.Now().Format("2006-01-02 15:04:05"),
	}
	for _, t := range []string{"domains", "domain_providers", "records", "record_provider_map"} {
		rows, err := h.queryAllMaps(ctx, `SELECT * FROM `+t)
		if err != nil {
			return nil, err
		}
		snap[t] = rows
	}
	return snap, nil
}

// queryAllMaps 查询为 []map[string]any，[]byte 转 string 以便 JSON 输出与 PHP 一致。
func (h *Handler) queryAllMaps(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := h.db.QueryxContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := rows.MapScan(m); err != nil {
			return nil, err
		}
		for k, v := range m {
			if b, ok := v.([]byte); ok {
				m[k] = string(b)
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (h *Handler) createBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	snap, err := h.snapshot(ctx)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	dataJSON, _ := json.Marshal(snap)
	gz, err := gzipCompress(dataJSON)
	if err != nil {
		httpapi.Fail(w, http.StatusBadRequest, "备份压缩失败")
		return
	}
	var recordCount int
	_ = h.db.GetContext(ctx, &recordCount, `SELECT COUNT(*) c FROM records`)
	_, err = h.db.ExecContext(ctx,
		`INSERT INTO backups (name,data,record_count,created_by,is_gzip) VALUES (?,?,?,?,1)`,
		"手动备份 "+time.Now().Format("2006-01-02 15:04"), gz, recordCount, u.Username)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "backup.create", "", nil, httpapi.IP(r))
	httpapi.OK(w, nil)
}

func (h *Handler) deleteBackups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	in := httpapi.Input(r)
	rawIDs, _ := in["ids"].([]any)
	ids := make([]int, 0, len(rawIDs))
	for _, v := range rawIDs {
		if n := intOr(v, 0); n > 0 {
			ids = append(ids, n)
		}
	}
	if len(ids) == 0 {
		httpapi.Fail(w, http.StatusBadRequest, "未选择备份")
		return
	}
	query, args, err := sqlx.In(`DELETE FROM backups WHERE id IN (?)`, ids)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	res, err := h.db.ExecContext(ctx, h.db.Rebind(query), args...)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	n, _ := res.RowsAffected()
	idStrs := make([]string, len(ids))
	for i, v := range ids {
		idStrs[i] = strconv.Itoa(v)
	}
	h.logger.Op(ctx, &u.ID, u.Username, "backup.delete", strings.Join(idStrs, ","), nil, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"deleted": n})
}

func (h *Handler) restoreBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	id := intFromPath(r, "id")

	var bk struct {
		ID     int    `db:"id"`
		Name   string `db:"name"`
		Data   []byte `db:"data"`
		IsGzip int    `db:"is_gzip"`
	}
	err := h.db.GetContext(ctx, &bk, `SELECT id,name,data,is_gzip FROM backups WHERE id=?`, id)
	if err != nil {
		httpapi.Fail(w, http.StatusNotFound, "备份不存在")
		return
	}

	raw := bk.Data
	if bk.IsGzip == 1 {
		raw, err = gzipDecompress(raw)
		if err != nil {
			httpapi.Fail(w, http.StatusBadRequest, "备份数据损坏，无法恢复")
			return
		}
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil || data == nil {
		httpapi.Fail(w, http.StatusBadRequest, "备份数据损坏，无法恢复")
		return
	}
	if sv, ok := data["schema_version"].(float64); ok && int(sv) != 2 {
		httpapi.Fail(w, http.StatusBadRequest,
			"备份结构版本不匹配（"+strconv.Itoa(int(sv))+"/2），请使用同版本备份恢复")
		return
	}

	// 恢复前快照
	before, err := h.snapshot(ctx)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	beforeJSON, _ := json.Marshal(before)
	beforeGz, _ := gzipCompress(beforeJSON)
	var recordCount int
	_ = h.db.GetContext(ctx, &recordCount, `SELECT COUNT(*) c FROM records`)
	_, _ = h.db.ExecContext(ctx,
		`INSERT INTO backups (name,data,record_count,created_by,is_gzip) VALUES (?,?,?,?,1)`,
		"恢复前快照 "+time.Now().Format("2006-01-02 15:04:05")+" (备份#"+strconv.Itoa(id)+")",
		beforeGz, recordCount, u.Username)

	now := time.Now().Format("2006-01-02 15:04:05")
	err = h.db.InTx(func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sync_queue`); err != nil {
			return err
		}
		for _, t := range []string{"record_provider_map", "records", "domain_providers", "domains"} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+t); err != nil {
				return err
			}
		}
		for _, row := range asAnySlice(data["domains"]) {
			m, _ := row.(map[string]any)
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO domains (id,name,status,remark,created_at) VALUES (?,?,?,?,?)`,
				anyInt(m["id"]), anyStr(m["name"]), anyStrOr(m["status"], "active"),
				anyNullable(m["remark"]), anyStrOr(m["created_at"], now)); err != nil {
				return err
			}
		}
		for _, row := range asAnySlice(data["domain_providers"]) {
			m, _ := row.(map[string]any)
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO domain_providers (id,domain_id,account_id,provider,provider_domain_id,sync_enabled,last_verify_at,verify_status,verify_message) VALUES (?,?,?,?,?,?,?,?,?)`,
				anyInt(m["id"]), anyInt(m["domain_id"]), anyInt(m["account_id"]), anyStr(m["provider"]),
				anyNullable(m["provider_domain_id"]), anyIntOr(m["sync_enabled"], 1),
				anyNullable(m["last_verify_at"]), anyStrOr(m["verify_status"], "unknown"),
				anyNullable(m["verify_message"])); err != nil {
				return err
			}
		}
		for _, row := range asAnySlice(data["records"]) {
			m, _ := row.(map[string]any)
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO records (id,domain_id,sub_domain,type,line,value,ttl,priority,remark,status,updated_at,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
				anyInt(m["id"]), anyInt(m["domain_id"]), anyStrOr(m["sub_domain"], "@"), anyStr(m["type"]),
				anyStrOr(m["line"], "默认"), anyStr(m["value"]), anyIntOr(m["ttl"], 600),
				anyNullable(m["priority"]), anyNullable(m["remark"]), anyStrOr(m["status"], "active"),
				anyStrOr(m["updated_at"], now), anyStrOr(m["created_at"], now)); err != nil {
				return err
			}
		}
		for _, row := range asAnySlice(data["record_provider_map"]) {
			m, _ := row.(map[string]any)
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO record_provider_map (id,record_id,account_id,provider,provider_record_id,sync_status,sync_message,synced_at,retry_count) VALUES (?,?,?,?,?,?,?,?,?)`,
				anyInt(m["id"]), anyInt(m["record_id"]), anyInt(m["account_id"]), anyStr(m["provider"]),
				anyNullable(m["provider_record_id"]), anyStrOr(m["sync_status"], "pending"),
				anyNullable(m["sync_message"]), anyNullable(m["synced_at"]), anyIntOr(m["retry_count"], 0)); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx,
			`UPDATE record_provider_map SET sync_status='pending', sync_message='恢复备份，待远端校验' WHERE sync_status='success'`)
		return err
	})
	if err != nil {
		httpapi.Fail(w, http.StatusBadRequest, "恢复失败: "+err.Error())
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "backup.restore", strconv.Itoa(id), nil, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"need_verify": true})
}

// ---- 工具 ----

func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipDecompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func asAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func anyInt(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	}
	return 0
}

func anyStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func anyStrOr(v any, def string) string {
	s := anyStr(v)
	if s == "" {
		return def
	}
	return s
}

func anyIntOr(v any, def int64) int64 {
	if v == nil {
		return def
	}
	return anyInt(v)
}

func anyNullable(v any) any {
	if v == nil {
		return nil
	}
	return v
}
