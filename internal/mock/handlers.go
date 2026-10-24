package mock

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/loeia/all-in-dns/internal/httpapi"
)

// Handler 返回注册好全部 mock 接口的 *http.ServeMux。
// 路由与真实后端（routes.php）保持一致，作为前后端接口契约。
func Handler() http.Handler {
	mux := http.NewServeMux()
	s := NewStore()

	// 模拟登录态：默认已登录（admin），方便前端直接开发主界面。
	loggedIn := true

	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		loggedIn = true
		httpapi.OK(w, map[string]any{"user": s.Users[0]})
	})
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		loggedIn = false
		httpapi.OK(w, nil)
	})
	mux.HandleFunc("GET /api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if !loggedIn {
			httpapi.Fail(w, http.StatusUnauthorized, "未登录")
			return
		}
		httpapi.OK(w, map[string]any{"user": s.Users[0]})
	})

	// ---------- 云账号 ----------
	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, s.Accounts)
	})
	mux.HandleFunc("POST /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		in := httpapi.Input(r)
		s.mu.Lock()
		s.Accounts = append(s.Accounts, Account{
			ID: s.next(), Provider: str(in["provider"]), Name: orDefault(in["name"], str(in["provider"])),
			Status: 1, CreatedAt: nowStr(), AccessKeyIDMasked: "****",
		})
		s.mu.Unlock()
		httpapi.OK(w, nil)
	})
	mux.HandleFunc("DELETE /api/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		s.mu.Lock()
		for i, a := range s.Accounts {
			if a.ID == id {
				s.Accounts = append(s.Accounts[:i], s.Accounts[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		httpapi.OK(w, nil)
	})
	mux.HandleFunc("POST /api/accounts/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, map[string]any{"ok": true, "domains": 3})
	})

	// ---------- 域名 ----------
	mux.HandleFunc("GET /api/domains", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, s.Domains)
	})
	mux.HandleFunc("POST /api/domains", func(w http.ResponseWriter, r *http.Request) {
		in := httpapi.Input(r)
		id := s.next()
		s.mu.Lock()
		s.Domains = append(s.Domains, Domain{
			ID: id, Name: str(in["name"]), Status: "active", Remark: str(in["remark"]),
			CreatedAt: nowStr(), Providers: "tencent:on,aliyun:on,volc:on",
		})
		s.mu.Unlock()
		httpapi.OK(w, map[string]any{"domain_id": id})
	})
	mux.HandleFunc("POST /api/domains/{id}/verify", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, []map[string]any{
			{"account_id": 1, "provider": "tencent", "status": "consistent", "local_count": 4, "remote_count": 4, "missing_local": 0, "extra_remote": 0},
			{"account_id": 2, "provider": "aliyun", "status": "inconsistent", "local_count": 4, "remote_count": 3, "missing_local": 1, "extra_remote": 0,
				"detail": map[string]any{"missing": []map[string]any{{"id": 2, "key": "www|CNAME|example.com|默认"}}, "extra": []string{}}},
			{"account_id": 3, "provider": "volc", "status": "consistent", "local_count": 4, "remote_count": 4, "missing_local": 0, "extra_remote": 0},
		})
	})
	mux.HandleFunc("POST /api/domains/{id}/repair", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, map[string]any{"sync": []map[string]any{{"queue_id": 1, "provider": "aliyun", "status": "success"}}})
	})
	mux.HandleFunc("POST /api/domains/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, nil)
	})
	mux.HandleFunc("DELETE /api/domains/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		s.mu.Lock()
		for i, d := range s.Domains {
			if d.ID == id {
				s.Domains = append(s.Domains[:i], s.Domains[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		httpapi.OK(w, nil)
	})

	// ---------- 解析记录 ----------
	mux.HandleFunc("GET /api/domains/{id}/records", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size < 1 {
			size = 100
		}
		var items []Record
		s.mu.RLock()
		for _, rec := range s.Records {
			if rec.DomainID == id {
				items = append(items, rec)
			}
		}
		s.mu.RUnlock()
		total := len(items)
		httpapi.OK(w, map[string]any{"items": items, "total": total, "page": page, "size": size})
	})
	mux.HandleFunc("POST /api/domains/{id}/records", func(w http.ResponseWriter, r *http.Request) {
		in := httpapi.Input(r)
		domainID, _ := strconv.Atoi(r.PathValue("id"))
		rid := s.next()
		s.mu.Lock()
		s.Records = append(s.Records, Record{
			ID: rid, DomainID: domainID, SubDomain: orDefault(in["sub"], "@"), Type: str(in["type"]),
			Line: orDefault(in["line"], "默认"), Value: str(in["value"]), TTL: intOr(in["ttl"], 600),
			Status: "active", UpdatedAt: nowStr(), SyncStates: "1|tencent|success,2|aliyun|success,3|volc|success",
		})
		s.mu.Unlock()
		httpapi.OK(w, map[string]any{"record_id": rid, "sync": []map[string]any{{"provider": "tencent", "status": "success"}, {"provider": "aliyun", "status": "success"}, {"provider": "volc", "status": "success"}}})
	})
	mux.HandleFunc("POST /api/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		in := httpapi.Input(r)
		s.mu.Lock()
		for i := range s.Records {
			if s.Records[i].ID == id {
				r2 := &s.Records[i]
				r2.SubDomain = orDefault(in["sub"], r2.SubDomain)
				r2.Type = str(in["type"])
				r2.Line = orDefault(in["line"], r2.Line)
				r2.Value = str(in["value"])
				r2.TTL = intOr(in["ttl"], r2.TTL)
				r2.UpdatedAt = nowStr()
				r2.Status = "active"
			}
		}
		s.mu.Unlock()
		httpapi.OK(w, map[string]any{"sync": []map[string]any{{"provider": "tencent", "status": "success"}, {"provider": "aliyun", "status": "success"}, {"provider": "volc", "status": "success"}}})
	})
	mux.HandleFunc("DELETE /api/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		s.mu.Lock()
		for i, rec := range s.Records {
			if rec.ID == id {
				s.Records = append(s.Records[:i], s.Records[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		httpapi.OK(w, map[string]any{"sync": []map[string]any{{"provider": "tencent", "status": "success"}, {"provider": "aliyun", "status": "success"}, {"provider": "volc", "status": "success"}}, "pending": false})
	})
	mux.HandleFunc("POST /api/records/batch-delete", func(w http.ResponseWriter, r *http.Request) {
		in := httpapi.Input(r)
		ids, _ := in["ids"].([]any)
		s.mu.Lock()
		del := map[int]bool{}
		for _, v := range ids {
			if f, ok := v.(float64); ok {
				del[int(f)] = true
			}
		}
		kept := s.Records[:0]
		for _, rec := range s.Records {
			if !del[rec.ID] {
				kept = append(kept, rec)
			}
		}
		s.Records = kept
		s.mu.Unlock()
		httpapi.OK(w, map[string]any{"pending": []int{}})
	})
	mux.HandleFunc("POST /api/domains/{id}/import-remote", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, map[string]any{"imported": 2, "merged": 0, "errors": []string{}})
	})
	mux.HandleFunc("GET /api/domains/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		var sb strings.Builder
		sb.WriteString("sub_domain,type,line,value,ttl,priority\n")
		s.mu.RLock()
		for _, rec := range s.Records {
			if rec.DomainID != id {
				continue
			}
			pri := ""
			if rec.Priority != nil {
				pri = strconv.Itoa(*rec.Priority)
			}
			fmt.Fprintf(&sb, "%s,%s,%s,%s,%d,%s\n", rec.SubDomain, rec.Type, rec.Line, rec.Value, rec.TTL, pri)
		}
		s.mu.RUnlock()
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"records_%d.csv\"", id))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("\xEF\xBB\xBF" + sb.String()))
	})
	mux.HandleFunc("POST /api/domains/{id}/import", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, map[string]any{"imported": 0, "skipped": 0, "failed": 0, "sync": []map[string]any{}})
	})

	// ---------- 同步 ----------
	mux.HandleFunc("GET /api/sync/logs", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, s.SyncLogs)
	})
	mux.HandleFunc("DELETE /api/sync/logs", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.SyncLogs = nil
		s.mu.Unlock()
		httpapi.OK(w, nil)
	})
	mux.HandleFunc("GET /api/sync/queue", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, s.Queue)
	})
	mux.HandleFunc("POST /api/sync/retry-now", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		for i := range s.Queue {
			s.Queue[i].Status = "success"
			s.Queue[i].RetryCount = 0
		}
		s.Queue = nil
		s.mu.Unlock()
		httpapi.OK(w, map[string]any{"count": 0, "items": []map[string]any{}})
	})
	mux.HandleFunc("DELETE /api/sync/queue", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		n := len(s.Queue)
		s.Queue = nil
		s.mu.Unlock()
		httpapi.OK(w, map[string]any{"count": n})
	})

	// ---------- 操作日志 ----------
	mux.HandleFunc("GET /api/operation-logs", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, s.OpLogs)
	})
	mux.HandleFunc("DELETE /api/operation-logs", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.OpLogs = nil
		s.mu.Unlock()
		httpapi.OK(w, nil)
	})

	// ---------- 备份与恢复 ----------
	mux.HandleFunc("GET /api/backups", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, s.Backups)
	})
	mux.HandleFunc("POST /api/backups", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.Backups = append(s.Backups, Backup{ID: s.next(), Name: "手动备份 " + nowStr()[:10], RecordCount: len(s.Records), CreatedBy: "admin", CreatedAt: nowStr()})
		s.mu.Unlock()
		httpapi.OK(w, nil)
	})
	mux.HandleFunc("POST /api/backups/delete", func(w http.ResponseWriter, r *http.Request) {
		in := httpapi.Input(r)
		ids, _ := in["ids"].([]any)
		del := map[int]bool{}
		for _, v := range ids {
			if f, ok := v.(float64); ok {
				del[int(f)] = true
			}
		}
		n := 0
		s.mu.Lock()
		kept := s.Backups[:0]
		for _, b := range s.Backups {
			if del[b.ID] {
				n++
				continue
			}
			kept = append(kept, b)
		}
		s.Backups = kept
		s.mu.Unlock()
		httpapi.OK(w, map[string]any{"deleted": n})
	})
	mux.HandleFunc("POST /api/backups/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		httpapi.OK(w, map[string]any{"need_verify": true})
	})

	// ---------- 监控 ----------
	mux.HandleFunc("GET /api/monitor/summary", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		pending, failed := 0, 0
		for _, q := range s.Queue {
			switch q.Status {
			case "pending":
				pending++
			case "failed":
				failed++
			}
		}
		httpapi.OK(w, map[string]any{
			"domains": len(s.Domains), "records": len(s.Records),
			"pending_queue": pending, "failed_queue": failed,
			"failed_sync_24h": 1, "success_sync_24h": 4,
		})
	})

	return httpapi.Chain(mux, httpapi.Recover)
}

// ---- 小工具 ----

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func orDefault(v any, def string) string {
	s := strings.TrimSpace(str(v))
	if s == "" {
		return def
	}
	return s
}

func intOr(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, err := strconv.Atoi(t)
		if err != nil {
			return def
		}
		return n
	}
	return def
}
