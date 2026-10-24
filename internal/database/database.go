// Package database 封装 MySQL 连接（sqlx），对齐原 PHP 版 Database::i()。
package database

import (
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql" // mysql driver
	"github.com/jmoiron/sqlx"

	"github.com/loeia/all-in-dns/internal/config"
)

// DB 包装 *sqlx.DB。
type DB struct {
	*sqlx.DB
}

// Open 建立连接池并 Ping 验证。
//
// 注意：DSN 不启用 parseTime，DATETIME 列直接读为 "2006-01-02 15:04:05"
// 字符串，保证 JSON 输出与 PHP 版完全一致（前端零改动）。
func Open(cfg config.DB) (*DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s",
		cfg.User, cfg.Pass, cfg.Host, cfg.Port, cfg.Name, cfg.Charset)

	db, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{DB: db}, nil
}

// InTx 在事务中执行 fn，出错自动回滚。
func (d *DB) InTx(fn func(tx *sqlx.Tx) error) error {
	tx, err := d.Beginx()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
