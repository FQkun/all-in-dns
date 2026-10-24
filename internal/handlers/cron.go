package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/loeia/all-in-dns/internal/notify"
)

// handleCron HTTP 定时任务入口（对齐原 cron.php）。
// 触发方式：GET /cron 带 X-Cron-Key 头（或 ?key=）。
func (h *Handler) handleCron(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	key := r.Header.Get("X-Cron-Key")
	if key == "" {
		key = r.URL.Query().Get("key")
	}
	if key == "" || h.cfg.App.CronKey == "" || key != h.cfg.App.CronKey {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	now := time.Now().Format("2006-01-02 15:04:05")

	// 1. 重试失败队列
	done, err := h.sync.ProcessPendingQueue(ctx, 100)
	if err != nil {
		_, _ = w.Write([]byte("重试队列失败: " + err.Error() + "\n"))
		return
	}
	ok := 0
	for _, d := range done {
		if d.Status == "success" {
			ok++
		}
	}
	_, _ = w.Write([]byte("[" + now + "] 重试: 共 " + itoa(len(done)) + " 成功 " + itoa(ok) + " 失败 " + itoa(len(done)-ok) + "\n"))

	var failCount int
	_ = h.db.GetContext(ctx, &failCount, `SELECT COUNT(*) c FROM sync_queue WHERE status='failed'`)
	if failCount >= 10 {
		_, _ = w.Write([]byte("[ALERT] " + itoa(failCount) + " 个同步任务最终失败，请检查云账号配置\n"))
	}

	// 2. 清理孤儿数据
	res, _ := h.db.ExecContext(ctx, `DELETE q FROM sync_queue q LEFT JOIN records r ON r.id=q.record_id WHERE r.id IS NULL`)
	orphanQueue, _ := res.RowsAffected()
	res2, _ := h.db.ExecContext(ctx, `DELETE m FROM record_provider_map m LEFT JOIN records r ON r.id=m.record_id WHERE r.id IS NULL`)
	orphanMap, _ := res2.RowsAffected()
	if orphanQueue > 0 || orphanMap > 0 {
		_, _ = w.Write([]byte("清理孤儿数据: 队列 " + itoa(int(orphanQueue)) + " 条 / 映射 " + itoa(int(orphanMap)) + " 条\n"))
	}

	// 3. 每日自动备份（保留 30 份）
	today := time.Now().Format("2006-01-02")
	var backupCount int
	_ = h.db.GetContext(ctx, &backupCount,
		`SELECT COUNT(*) c FROM backups WHERE created_by='system' AND name=?`, "自动备份 "+today)
	if backupCount == 0 {
		if err := h.systemBackup(ctx, today); err != nil {
			_, _ = w.Write([]byte("自动备份失败: " + err.Error() + "\n"))
			return
		}
		_, _ = w.Write([]byte("自动备份完成\n"))
	}
}

// systemBackup 生成当日系统自动备份并清理旧备份（保留 30 份）。
func (h *Handler) systemBackup(ctx context.Context, today string) error {
	snap := map[string]any{
		"schema_version": 2,
		"created_at":     time.Now().Format("2006-01-02 15:04:05"),
	}
	for _, t := range []string{"domains", "domain_providers", "records", "record_provider_map"} {
		rows, err := h.queryAllMaps(ctx, `SELECT * FROM `+t)
		if err != nil {
			return err
		}
		snap[t] = rows
	}
	dataJSON, _ := json.Marshal(snap)
	gz, err := gzipCompress(dataJSON)
	if err != nil {
		return err
	}
	var recordCount int
	_ = h.db.GetContext(ctx, &recordCount, `SELECT COUNT(*) c FROM records`)
	_, err = h.db.ExecContext(ctx,
		`INSERT INTO backups (name,data,record_count,created_by,is_gzip) VALUES (?,?,?,?,1)`,
		"自动备份 "+today, gz, recordCount, "system")
	if err != nil {
		return err
	}
	_, _ = h.db.ExecContext(ctx, `
		DELETE FROM backups WHERE created_by='system' AND id NOT IN
		(SELECT id FROM (SELECT id FROM backups WHERE created_by='system' ORDER BY id DESC LIMIT 30) t)`)

	if h.notifier != nil {
		h.notifier.Notify(ctx, notify.Event{
			Type: notify.EventBackupDone, Level: "info",
			Message: "每日自动备份完成，共 " + itoa(recordCount) + " 条记录",
		})
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
