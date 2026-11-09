// Package db 负责打开 SQLite 数据库并执行表结构迁移。
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // 纯 Go 实现的 SQLite 驱动，无需 CGO
)

// Open 打开（不存在则创建）数据库文件，并执行未应用的迁移。
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" + // 读写并发更好
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 同一时刻只允许一个写入者。单连接让所有请求排队执行，
	// 避免 SQLITE_BUSY；对小规模系统足够快。
	d.SetMaxOpenConns(1)

	if err := migrate(d); err != nil {
		d.Close()
		return nil, fmt.Errorf("迁移数据库: %w", err)
	}
	return d, nil
}

// migrate 用 SQLite 的 user_version 记录已应用的迁移数量，
// 只执行 migrations 中尚未执行的部分。
func migrate(d *sql.DB) error {
	var version int
	if err := d.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("迁移 #%d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SetRole 修改用户角色，用于命令行提升管理员。
func SetRole(d *sql.DB, username, role string) error {
	res, err := d.Exec("UPDATE users SET role = ? WHERE username = ?", role, username)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("用户 %q 不存在", username)
	}
	return nil
}
