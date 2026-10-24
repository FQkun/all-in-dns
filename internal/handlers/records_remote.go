package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/loeia/all-in-dns/internal/httpapi"
	"github.com/loeia/all-in-dns/internal/sync"
)

var mxValueRe = regexp.MustCompile(`^(\d+)\s+(.+)$`)

var providerLabel = map[string]string{
	"tencent": "腾讯云", "aliyun": "阿里云", "volc": "火山引擎",
}

// importRemote 从各云厂商拉取记录导入本地（单向导入，不触发同步）。
func (h *Handler) importRemote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	id := intFromPath(r, "id")

	var domainName string
	err := h.db.GetContext(ctx, &domainName, `SELECT name FROM domains WHERE id=?`, id)
	if err != nil || domainName == "" {
		httpapi.Fail(w, http.StatusNotFound, "域名不存在")
		return
	}

	var dps []struct {
		AccountID int    `db:"account_id"`
		Provider  string `db:"provider"`
	}
	_ = h.db.SelectContext(ctx, &dps, `
		SELECT dp.account_id, ca.provider FROM domain_providers dp
		JOIN cloud_accounts ca ON ca.id=dp.account_id
		WHERE dp.domain_id=? AND dp.sync_enabled=1`, id)

	// 已有本地记录按归一化键索引
	seen := map[string]int{}
	type localRec struct {
		ID        int    `db:"id"`
		SubDomain string `db:"sub_domain"`
		Type      string `db:"type"`
		Line      string `db:"line"`
		Value     string `db:"value"`
	}
	var locals []localRec
	_ = h.db.SelectContext(ctx, &locals,
		`SELECT id, sub_domain, type, line, value FROM records WHERE domain_id=?`, id)
	for _, lr := range locals {
		seen[sync.RecKey(lr.SubDomain, lr.Type, lr.Value, lr.Line)] = lr.ID
	}

	imported := 0
	var errors []string
	for _, dp := range dps {
		prov, err := h.sync.MakeProvider(ctx, dp.AccountID)
		if err != nil {
			errors = append(errors, label(dp.Provider)+": "+err.Error())
			continue
		}
		remote, err := prov.ListRecords(ctx, domainName)
		if err != nil {
			errors = append(errors, label(dp.Provider)+": "+err.Error())
			continue
		}
		for _, rr := range remote {
			if strings.ToUpper(rr.Type) == "NS" && strings.ToLower(rr.Sub) == "@" {
				continue
			}
			line := sync.NormLine(rr.Line)
			value := rr.Value
			priority := rr.Priority
			if strings.ToUpper(rr.Type) == "MX" {
				if m := mxValueRe.FindStringSubmatch(value); m != nil {
					p := intOr(m[1], 0)
					priority = &p
					value = m[2]
				}
			}
			key := sync.RecKey(rr.Sub, rr.Type, value, line)

			recordID, exists := seen[key]
			if !exists {
				res, err := h.db.ExecContext(ctx,
					`INSERT INTO records (domain_id,sub_domain,type,line,value,ttl,priority) VALUES (?,?,?,?,?,?,?)`,
					id, rr.Sub, rr.Type, line, value, rr.TTL, priority)
				if err != nil {
					httpapi.Error(w, err)
					return
				}
				rid, _ := res.LastInsertId()
				recordID = int(rid)
				seen[key] = recordID
				imported++
			}
			_, _ = h.db.ExecContext(ctx, `
				INSERT INTO record_provider_map (record_id,account_id,provider,provider_record_id,sync_status,synced_at)
				VALUES (?,?,?,?,?,NOW())
				ON DUPLICATE KEY UPDATE provider=VALUES(provider), provider_record_id=VALUES(provider_record_id), sync_status='success', synced_at=NOW()`,
				recordID, dp.AccountID, dp.Provider, rr.ID, "success")
		}
	}

	merged, err := h.sync.MergeDuplicates(ctx, id)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "record.import_remote", domainName,
		map[string]any{"count": imported, "merged": merged}, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"imported": imported, "merged": merged, "errors": errors})
}

func label(p string) string {
	if l, ok := providerLabel[p]; ok {
		return l
	}
	return p
}
