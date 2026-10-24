package sync

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// VerifyReport 一致性校验报告单条。
type VerifyReport struct {
	AccountID    int           `json:"account_id,omitempty"`
	Provider     string        `json:"provider"`
	Status       string        `json:"status"` // consistent | inconsistent | error
	LocalCount   int           `json:"local_count,omitempty"`
	RemoteCount  int           `json:"remote_count,omitempty"`
	MissingLocal int           `json:"missing_local,omitempty"`
	ExtraRemote  int           `json:"extra_remote,omitempty"`
	Message      string        `json:"message,omitempty"`
	Detail       *VerifyDetail `json:"detail,omitempty"`
}

// VerifyDetail 差异明细。
type VerifyDetail struct {
	Missing []MissingItem `json:"missing"`
	Extra   []string      `json:"extra"`
}

// MissingItem 本地缺失记录。
type MissingItem struct {
	ID  int    `json:"id"`
	Key string `json:"key"`
}

// VerifyDomain 对比本地与各厂商远端记录（对齐 SyncService::verifyDomain）。
func (s *Service) VerifyDomain(ctx context.Context, domainID int) ([]VerifyReport, error) {
	var domainName string
	err := s.db.GetContext(ctx, &domainName, `SELECT name FROM domains WHERE id=?`, domainID)
	if errors.Is(err, nil) && domainName == "" {
		return nil, errors.New("域名不存在")
	}
	if err != nil {
		return nil, err
	}

	// 本地记录 key → id
	var local []struct {
		ID        int    `db:"id"`
		SubDomain string `db:"sub_domain"`
		Type      string `db:"type"`
		Line      string `db:"line"`
		Value     string `db:"value"`
	}
	err = s.db.SelectContext(ctx, &local,
		`SELECT id, sub_domain, type, line, value FROM records WHERE domain_id=? AND status != 'disabled'`, domainID)
	if err != nil {
		return nil, err
	}
	localKeys := map[string]int{}
	for _, r := range local {
		if strings.ToUpper(r.Type) == "NS" && strings.ToLower(r.SubDomain) == "@" {
			continue
		}
		localKeys[RecKey(r.SubDomain, r.Type, r.Value, r.Line)] = r.ID
	}

	var dps []struct {
		AccountID int    `db:"account_id"`
		Provider  string `db:"provider"`
	}
	err = s.db.SelectContext(ctx, &dps,
		`SELECT account_id, provider FROM domain_providers WHERE domain_id=? AND sync_enabled=1`, domainID)
	if err != nil {
		return nil, err
	}

	report := make([]VerifyReport, 0, len(dps))
	for _, dp := range dps {
		aid := dp.AccountID
		prov, err := s.MakeProvider(ctx, aid)
		if err != nil {
			report = append(report, VerifyReport{
				AccountID: aid, Provider: dp.Provider, Status: "error",
				LocalCount: len(localKeys), RemoteCount: 0, Message: err.Error(),
			})
			continue
		}
		remote, err := prov.ListRecords(ctx, domainName)
		if err != nil {
			report = append(report, VerifyReport{
				AccountID: aid, Provider: dp.Provider, Status: "error",
				LocalCount: len(localKeys), RemoteCount: 0, Message: err.Error(),
			})
			continue
		}
		remoteKeys := map[string]string{}
		for _, rr := range remote {
			if strings.ToUpper(rr.Type) == "NS" && strings.ToLower(rr.Sub) == "@" {
				continue
			}
			remoteKeys[RecKey(rr.Sub, rr.Type, rr.Value, rr.Line)] = rr.ID
		}

		missing := map[string]int{}
		for k, id := range localKeys {
			if _, ok := remoteKeys[k]; !ok {
				missing[k] = id
			}
		}
		extra := map[string]string{}
		for k, id := range remoteKeys {
			if _, ok := localKeys[k]; !ok {
				extra[k] = id
			}
		}

		status := "consistent"
		if len(missing) > 0 || len(extra) > 0 {
			status = "inconsistent"
		}
		missingList := make([]MissingItem, 0, len(missing))
		for k, id := range missing {
			missingList = append(missingList, MissingItem{ID: id, Key: k})
		}
		extraKeys := make([]string, 0, len(extra))
		for k := range extra {
			extraKeys = append(extraKeys, k)
		}

		ok := "ok"
		if status != "consistent" {
			ok = "fail"
		}
		_, _ = s.db.ExecContext(ctx,
			`UPDATE domain_providers SET last_verify_at=NOW(), verify_status=? WHERE domain_id=? AND account_id=?`,
			ok, domainID, aid)

		report = append(report, VerifyReport{
			AccountID:    aid,
			Provider:     dp.Provider,
			Status:       status,
			LocalCount:   len(localKeys),
			RemoteCount:  len(remoteKeys),
			MissingLocal: len(missing),
			ExtraRemote:  len(extra),
			Detail:       &VerifyDetail{Missing: missingList, Extra: extraKeys},
		})
	}
	if len(report) == 0 {
		report = append(report, VerifyReport{Provider: "-", Status: "error", Message: "该域名未绑定任何启用同步的云平台"})
	}
	return report, nil
}

// PushToAccount 把指定记录推送到指定云账号（对齐 SyncService::pushToAccount）。
func (s *Service) PushToAccount(ctx context.Context, domainID, accountID int, recordIDs []int) ([]Result, error) {
	var domainName string
	err := s.db.GetContext(ctx, &domainName, `SELECT name FROM domains WHERE id=?`, domainID)
	if err != nil || domainName == "" {
		return nil, errors.New("域名不存在")
	}

	var queueIDs []int
	for _, rid := range recordIDs {
		rec, err := s.getRecord(ctx, rid)
		if err != nil {
			return nil, err
		}
		if rec == nil || rec.DomainID != domainID {
			continue
		}
		_, _ = s.db.ExecContext(ctx,
			`DELETE FROM record_provider_map WHERE record_id=? AND account_id=?`, rid, accountID)

		payloadJSON, _ := json.Marshal(map[string]any{
			"domain": domainName,
			"record": normalizeRecord(rec),
		})
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO sync_queue (record_id,account_id,action,payload) VALUES (?,?,?,?)`,
			rid, accountID, "create", string(payloadJSON))
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		queueIDs = append(queueIDs, int(id))
	}
	return s.executeBatch(ctx, queueIDs)
}

// MergeDuplicates 合并同域名重复记录（对齐 SyncService::mergeDuplicates）。
func (s *Service) MergeDuplicates(ctx context.Context, domainID int) (int, error) {
	var rows []struct {
		ID        int    `db:"id"`
		SubDomain string `db:"sub_domain"`
		Type      string `db:"type"`
		Line      string `db:"line"`
		Value     string `db:"value"`
	}
	err := s.db.SelectContext(ctx, &rows,
		`SELECT id, sub_domain, type, line, value FROM records WHERE domain_id=? ORDER BY id`, domainID)
	if err != nil {
		return 0, err
	}
	groups := map[string][]int{}
	for _, r := range rows {
		k := RecKey(r.SubDomain, r.Type, r.Value, r.Line)
		groups[k] = append(groups[k], r.ID)
	}

	removed := 0
	for _, ids := range groups {
		if len(ids) < 2 {
			continue
		}
		keep := ids[0]
		for _, dup := range ids[1:] {
			_, _ = s.db.ExecContext(ctx, `
				DELETE m FROM record_provider_map m
				JOIN record_provider_map k ON k.record_id=? AND k.account_id=m.account_id
				WHERE m.record_id=?`, keep, dup)
			_, _ = s.db.ExecContext(ctx,
				`UPDATE record_provider_map SET record_id=? WHERE record_id=?`, keep, dup)
			_, _ = s.db.ExecContext(ctx, `DELETE FROM records WHERE id=?`, dup)
			removed++
		}
	}
	return removed, nil
}

var mxPriorityRe = regexp.MustCompile(`^\d+\s+`)

// NormLine 归一化线路名（对齐 SyncService::normLine）。
func NormLine(line string) string {
	l := strings.TrimSpace(line)
	if l == "" || l == "默认" || strings.EqualFold(l, "default") {
		return "默认"
	}
	return l
}

// RecKey 归一化记录键（对齐 SyncService::recKey）。
func RecKey(sub, typ, value, line string) string {
	t := strings.ToUpper(strings.TrimSpace(typ))
	v := strings.TrimSpace(value)
	if t == "MX" {
		v = mxPriorityRe.ReplaceAllString(v, "")
	}
	switch t {
	case "CNAME", "MX", "NS", "SRV":
		v = strings.TrimRight(v, ".")
	}
	switch t {
	case "CNAME", "MX", "NS", "SRV", "A", "AAAA":
		v = strings.ToLower(v)
	}
	return strings.ToLower(strings.TrimSpace(sub)) + "|" + t + "|" + v + "|" + NormLine(line)
}
