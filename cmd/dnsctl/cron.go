package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/loeia/all-in-dns/internal/auth"
	"github.com/loeia/all-in-dns/internal/config"
	"github.com/loeia/all-in-dns/internal/database"
	"github.com/loeia/all-in-dns/internal/logger"
	"github.com/loeia/all-in-dns/internal/redis"
	"github.com/loeia/all-in-dns/internal/sync"
)

// runCron 定时任务：重试失败队列 + 清理孤儿数据 + 每日自动备份。
// 对齐原 cron.php / cron/retry.php / cron/backup.php。
func runCron(args []string) int {
	fs := flag.NewFlagSet("cron-run", flag.ExitOnError)
	_ = fs.Parse(args)

	cfg := config.FromEnv()
	setTimezone(cfg.App.Timezone)

	db, err := database.Open(cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "数据库连接失败:", err)
		return 1
	}
	defer db.Close()
	rdb, err := redis.Open(cfg.Redis)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Redis 连接失败:", err)
		return 1
	}
	defer rdb.Close()

	_ = auth.New(db, rdb, cfg.App.SecretKey, cfg.SessionTTLDuration())
	lg := logger.New(db)
	svc := sync.New(db, lg, cfg.App.SecretKey, cfg.App.MaxRetry, cfg.App.RetryBaseDelay)

	ctx := osContext()
	now := time.Now().Format("2006-01-02 15:04:05")

	// 1. 重试失败队列
	done, err := svc.ProcessPendingQueue(ctx, 100)
	if err != nil {
		fmt.Fprintln(os.Stderr, "重试队列失败:", err)
		return 1
	}
	ok := 0
	for _, d := range done {
		if d.Status == "success" {
			ok++
		}
	}
	fmt.Printf("[%s] 重试: 共 %d 成功 %d 失败 %d\n", now, len(done), ok, len(done)-ok)

	var failCount int
	_ = db.GetContext(ctx, &failCount, `SELECT COUNT(*) c FROM sync_queue WHERE status='failed'`)
	if failCount >= 10 {
		fmt.Printf("[ALERT] %d 个同步任务最终失败，请检查云账号配置\n", failCount)
	}

	// 2. 清理孤儿数据
	res, _ := db.ExecContext(ctx, `DELETE q FROM sync_queue q LEFT JOIN records r ON r.id=q.record_id WHERE r.id IS NULL`)
	orphanQueue, _ := res.RowsAffected()
	res2, _ := db.ExecContext(ctx, `DELETE m FROM record_provider_map m LEFT JOIN records r ON r.id=m.record_id WHERE r.id IS NULL`)
	orphanMap, _ := res2.RowsAffected()
	if orphanQueue > 0 || orphanMap > 0 {
		fmt.Printf("清理孤儿数据: 队列 %d 条 / 映射 %d 条\n", orphanQueue, orphanMap)
	}

	// 3. 每日自动备份（保留最近 30 份）
	today := time.Now().Format("2006-01-02")
	var backupCount int
	_ = db.GetContext(ctx, &backupCount,
		`SELECT COUNT(*) c FROM backups WHERE created_by='system' AND name=?`, "自动备份 "+today)
	if backupCount == 0 {
		if err := autoBackup(db, today); err != nil {
			fmt.Fprintln(os.Stderr, "自动备份失败:", err)
			return 1
		}
		fmt.Println("自动备份完成")
	}
	return 0
}

// autoBackup 生成当日自动备份并清理旧备份（保留 30 份）。
func autoBackup(db *database.DB, today string) error {
	snap := map[string]any{
		"schema_version": 2,
		"created_at":     time.Now().Format("2006-01-02 15:04:05"),
	}
	for _, t := range []string{"domains", "domain_providers", "records", "record_provider_map"} {
		rows, err := queryMaps(db, `SELECT * FROM `+t)
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
	ctx := context.Background()
	var recordCount int
	_ = db.GetContext(ctx, &recordCount, `SELECT COUNT(*) c FROM records`)
	_, err = db.ExecContext(ctx,
		`INSERT INTO backups (name,data,record_count,created_by,is_gzip) VALUES (?,?,?,?,1)`,
		"自动备份 "+today, gz, recordCount, "system")
	if err != nil {
		return err
	}
	_, _ = db.ExecContext(ctx, `
		DELETE FROM backups WHERE created_by='system' AND id NOT IN
		(SELECT id FROM (SELECT id FROM backups WHERE created_by='system' ORDER BY id DESC LIMIT 30) t)`)
	return nil
}

func queryMaps(db *database.DB, query string) ([]map[string]any, error) {
	rows, err := db.QueryxContext(context.Background(), query)
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
