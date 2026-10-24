// Package logger 提供操作日志与同步日志写入，对齐原 PHP 版 Logger。
package logger

import (
	"context"
	"encoding/json"

	"github.com/loeia/all-in-dns/internal/database"
)

// Logger 日志写入器。
type Logger struct {
	db *database.DB
}

// New 构造 Logger。
func New(db *database.DB) *Logger { return &Logger{db: db} }

// Op 写操作日志。
func (l *Logger) Op(ctx context.Context, userID *int, username, action, target string, detail any, ip string) {
	var detailStr string
	switch d := detail.(type) {
	case nil:
		detailStr = ""
	case string:
		detailStr = d
	default:
		if b, err := json.Marshal(d); err == nil {
			detailStr = string(b)
		}
	}
	_, _ = l.db.ExecContext(ctx,
		`INSERT INTO operation_logs (user_id,username,action,target,detail,ip) VALUES (?,?,?,?,?,?)`,
		userID, username, action, target, detailStr, ip)
}

// Sync 写同步日志；message 按 500 字符截断（对齐 PHP mb_substr）。
func (l *Logger) Sync(ctx context.Context, recordID *int, accountID int, provider, action string, success bool, message string, costMs *int) {
	status := "failed"
	if success {
		status = "success"
	}
	_, _ = l.db.ExecContext(ctx,
		`INSERT INTO sync_logs (record_id,account_id,provider,action,status,message,request_cost_ms) VALUES (?,?,?,?,?,?,?)`,
		recordID, accountID, provider, action, status, truncateRunes(message, 500), costMs)
}

// truncateRunes 按字符（rune）截断，等价 PHP mb_substr(s, 0, n)。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
