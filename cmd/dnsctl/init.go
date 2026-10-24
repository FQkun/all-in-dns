package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"

	"github.com/loeia/all-in-dns/internal/config"
)

// ensureInit 首次启动时自动初始化数据库（幂等，重复执行不会清数据）。
// 逻辑：库不存在 → 建库建表；管理员不存在 → 创建。已存在则跳过。
func ensureInit(cfg *config.Config) error {
	// 1. 连 server（不带库名）
	baseDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4",
		cfg.DB.User, cfg.DB.Pass, cfg.DB.Host, cfg.DB.Port)
	server, err := sql.Open("mysql", baseDSN)
	if err != nil {
		return fmt.Errorf("MySQL 连接失败: %w", err)
	}
	defer server.Close()
	if err := server.Ping(); err != nil {
		return fmt.Errorf("MySQL 连接失败: %w", err)
	}

	// 2. users 表不存在则建库建表（库可能由 compose 的 MYSQL_DATABASE 预先创建但无表）
	var tableExists int
	_ = server.QueryRow(
		`SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME='users'`,
		cfg.DB.Name).Scan(&tableExists)
	if tableExists == 0 {
		if err := runSchema(server, cfg.DB.Name, "migrations/schema.sql"); err != nil {
			return fmt.Errorf("导入 schema 失败: %w", err)
		}
	}

	// 3. 连目标库，管理员不存在则创建
	dbDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4",
		cfg.DB.User, cfg.DB.Pass, cfg.DB.Host, cfg.DB.Port, cfg.DB.Name)
	db, err := sql.Open("mysql", dbDSN)
	if err != nil {
		return fmt.Errorf("连接目标库失败: %w", err)
	}
	defer db.Close()

	// 迁移：确保 provider 枚举包含 spaceship（已有库升级）
	if err := migrateProviderEnum(db); err != nil {
		return fmt.Errorf("迁移 provider 枚举失败: %w", err)
	}

	var exists int
	_ = db.QueryRow(`SELECT COUNT(*) FROM users WHERE username=?`, cfg.App.AdminUser).Scan(&exists)
	if exists == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte(cfg.App.AdminPass), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("密码加密失败: %w", err)
		}
		if _, err := db.Exec(`INSERT INTO users (username,password_hash,role) VALUES (?,?,?)`,
			cfg.App.AdminUser, string(hash), "admin"); err != nil {
			return fmt.Errorf("创建管理员失败: %w", err)
		}
		fmt.Printf("已创建管理员: %s\n", cfg.App.AdminUser)
	}
	return nil
}

// migrateProviderEnum 已有库升级：确保 provider 枚举包含 spaceship。
func migrateProviderEnum(db *sql.DB) error {
	var colType string
	err := db.QueryRow(`SELECT COLUMN_TYPE FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='cloud_accounts' AND COLUMN_NAME='provider'`).Scan(&colType)
	if err != nil {
		return nil // 表不存在则跳过（首次安装会建新表）
	}
	if strings.Contains(colType, "spaceship") {
		return nil
	}
	for _, t := range []string{"cloud_accounts", "domain_providers", "record_provider_map"} {
		if _, err := db.Exec("ALTER TABLE " + t + " MODIFY provider ENUM('tencent','aliyun','volc','spaceship') NOT NULL"); err != nil {
			return err
		}
	}
	return nil
}

// runSchema 导入 schema.sql：替换库名后逐条执行。
func runSchema(server *sql.DB, dbName, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 schema 失败: %w", err)
	}
	sqlText := strings.ReplaceAll(string(raw), "dns_manager", dbName)
	stmts := splitSQL(sqlText)
	for _, s := range stmts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, err := server.Exec(s); err != nil {
			return fmt.Errorf("执行失败 [%s...]: %w", truncateStr(s, 60), err)
		}
	}
	return nil
}

// splitSQL 按分号拆分 SQL 语句（schema.sql 无字符串内分号，简单拆分即可）。
func splitSQL(s string) []string {
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
