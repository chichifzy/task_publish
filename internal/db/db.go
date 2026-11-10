// Package db 负责打开 SQLite 数据库并执行表结构迁移。
package db

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // 纯 Go 实现的 SQLite 驱动，无需 CGO
)

// Open 打开（不存在则创建）数据库文件，并执行未应用的迁移。
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" + // 读写并发更好
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_txlock=immediate" // 事务一开始就拿写锁，避免读后写升级失败
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 同一时刻只允许一个写入者。单连接让所有请求排队执行，
	// 避免 SQLITE_BUSY；对小规模系统足够快。
	// 注意：事务内只能使用 tx 执行语句，再用 db 会等待这唯一的连接而死锁。
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
	if version >= len(migrations) {
		return nil
	}
	// 重建表时需要先关闭外键检查（事务内无法修改），迁移完成后再逐条用 foreign_key_check 校验。
	// 连接池只有一个连接，这里的设置作用于后续所有语句。
	if _, err := d.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer d.Exec("PRAGMA foreign_keys = ON")
	for i := version; i < len(migrations); i++ {
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("迁移 #%d: %w", i+1, err)
		}
		rows, err := tx.Query("PRAGMA foreign_key_check")
		if err != nil {
			tx.Rollback()
			return err
		}
		broken := rows.Next()
		rows.Close()
		if broken {
			tx.Rollback()
			return fmt.Errorf("迁移 #%d: 外键校验失败", i+1)
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

var (
	ErrAdminExists = errors.New("管理员已存在，系统只能有一个管理员")
	ErrUIDTaken    = errors.New("用户 ID 已被使用")
)

// CreateAdmin 创建唯一的管理员账号，仅供命令行使用。
func CreateAdmin(d *sql.DB, uid, nickname, passwordHash string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrAdminExists
	}
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM users WHERE uid = ?)`, uid).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrUIDTaken
	}
	if _, err := tx.Exec(
		`INSERT INTO users (uid, nickname, password_hash, role) VALUES (?, ?, ?, 'admin')`,
		uid, nickname, passwordHash); err != nil {
		return err
	}
	return tx.Commit()
}
