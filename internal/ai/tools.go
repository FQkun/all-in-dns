package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/loeia/all-in-dns/internal/database"
)

// Executor 只读诊断工具执行器（不做任何写操作）。
type Executor struct {
	db *database.DB
}

// NewExecutor 构造执行器。
func NewExecutor(db *database.DB) *Executor {
	return &Executor{db: db}
}

// Tools 返回暴露给模型的工具定义。
func (e *Executor) Tools() []Tool {
	return []Tool{
		{
			Type: "function",
			Function: FunctionDef{
				Name:        "list_domains",
				Description: "列出当前平台管理的所有域名，以及每个域名绑定的云平台和验证状态",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		{
			Type: "function",
			Function: FunctionDef{
				Name:        "get_domain_status",
				Description: "查询某个域名的详细状态：绑定的云平台、验证状态、待重试的同步任务、最近失败的同步日志",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"domain": map[string]any{"type": "string", "description": "域名，如 example.com"},
					},
					"required": []string{"domain"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionDef{
				Name:        "list_sync_logs",
				Description: "查询最近的同步日志，可只看失败的，用于诊断同步失败原因",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"failed_only": map[string]any{"type": "boolean", "description": "是否只看失败的"},
						"limit":       map[string]any{"type": "integer", "description": "返回条数，默认 10"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionDef{
				Name:        "list_records",
				Description: "查询某个域名下的本地解析记录",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"domain": map[string]any{"type": "string", "description": "域名，如 example.com"},
					},
					"required": []string{"domain"},
				},
			},
		},
	}
}

// Execute 执行工具，返回 JSON 字符串结果。
func (e *Executor) Execute(ctx context.Context, name, argsJSON string) (string, error) {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	if args == nil {
		args = map[string]any{}
	}

	switch name {
	case "list_domains":
		return e.listDomains(ctx)
	case "get_domain_status":
		return e.getDomainStatus(ctx, str(args["domain"]))
	case "list_sync_logs":
		return e.listSyncLogs(ctx, boolArg(args["failed_only"]), intArg(args["limit"], 10))
	case "list_records":
		return e.listRecords(ctx, str(args["domain"]))
	default:
		return "", fmt.Errorf("未知工具: %s", name)
	}
}

func (e *Executor) listDomains(ctx context.Context) (string, error) {
	rows, err := e.queryMaps(ctx, `
		SELECT d.name, d.status,
		       GROUP_CONCAT(CONCAT(dp.provider, ':', dp.verify_status) SEPARATOR ',') AS providers,
		       COUNT(DISTINCT r.id) AS record_count
		FROM domains d
		LEFT JOIN domain_providers dp ON dp.domain_id = d.id
		LEFT JOIN records r ON r.domain_id = d.id
		GROUP BY d.id ORDER BY d.id`)
	if err != nil {
		return "", err
	}
	return jsonString(rows), nil
}

func (e *Executor) getDomainStatus(ctx context.Context, domain string) (string, error) {
	var domainID int
	err := e.db.GetContext(ctx, &domainID, `SELECT id FROM domains WHERE name=?`, domain)
	if err != nil {
		return jsonString(map[string]any{"error": "域名不存在: " + domain}), nil
	}

	providers, _ := e.queryMaps(ctx, `
		SELECT dp.provider, ca.name AS account, dp.verify_status, dp.verify_message, dp.sync_enabled
		FROM domain_providers dp JOIN cloud_accounts ca ON ca.id = dp.account_id
		WHERE dp.domain_id=? ORDER BY dp.id`, domainID)

	queue, _ := e.queryMaps(ctx, `
		SELECT q.action, q.status, q.retry_count, q.last_error, ca.provider
		FROM sync_queue q
		JOIN records r ON r.id = q.record_id
		JOIN cloud_accounts ca ON ca.id = q.account_id
		WHERE r.domain_id=? AND q.status IN ('pending','failed')
		ORDER BY q.id DESC LIMIT 20`, domainID)

	logs, _ := e.queryMaps(ctx, `
		SELECT l.provider, l.action, l.status, l.message, l.created_at
		FROM sync_logs l JOIN records r ON r.id = l.record_id
		WHERE r.domain_id=? AND l.status='failed'
		ORDER BY l.id DESC LIMIT 10`, domainID)

	return jsonString(map[string]any{
		"domain":         domain,
		"providers":      providers,
		"pending_queue":  queue,
		"recent_failed":  logs,
	}), nil
}

func (e *Executor) listSyncLogs(ctx context.Context, failedOnly bool, limit int) (string, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	query := `SELECT provider, action, status, message, created_at FROM sync_logs`
	if failedOnly {
		query += ` WHERE status='failed'`
	}
	query += ` ORDER BY id DESC LIMIT ?`

	rows, err := e.queryMaps(ctx, query, limit)
	if err != nil {
		return "", err
	}
	return jsonString(rows), nil
}

func (e *Executor) listRecords(ctx context.Context, domain string) (string, error) {
	var domainID int
	err := e.db.GetContext(ctx, &domainID, `SELECT id FROM domains WHERE name=?`, domain)
	if err != nil {
		return jsonString(map[string]any{"error": "域名不存在: " + domain}), nil
	}
	rows, err := e.queryMaps(ctx, `
		SELECT sub_domain, type, line, value, ttl, priority, status
		FROM records WHERE domain_id=? ORDER BY type, sub_domain LIMIT 200`, domainID)
	if err != nil {
		return "", err
	}
	return jsonString(rows), nil
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolArg(v any) bool {
	b, _ := v.(bool)
	return b
}

func intArg(v any, def int) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return def
}

// queryMaps 查询为 []map[string]any（sqlx Select 不支持 []map，需 MapScan）。
func (e *Executor) queryMaps(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := e.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
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
