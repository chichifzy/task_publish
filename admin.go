package main

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"task_publish/internal/db"
	"task_publish/internal/server"
)

// createAdmin 创建系统唯一的管理员。密码从终端读取（不回显），
// 非终端时从标准输入读取一行，便于脚本使用。密码不作为命令行参数，避免留在 shell 历史里。
func createAdmin(database *sql.DB, uid, nickname string) error {
	uid = strings.TrimSpace(uid)
	nickname = strings.TrimSpace(nickname)
	if msg := server.ValidateUID(uid); msg != "" {
		return errors.New(msg)
	}
	if msg := server.ValidateNickname(nickname); msg != "" {
		return errors.New(msg)
	}

	password, err := readPassword()
	if err != nil {
		return err
	}
	if msg := server.ValidatePassword(password); msg != "" {
		return errors.New(msg)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return db.CreateAdmin(database, uid, nickname, string(hash))
}

func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("读取密码: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "密码: ")
	p1, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "再次输入密码: ")
	p2, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(p1) != string(p2) {
		return "", errors.New("两次输入的密码不一致")
	}
	return string(p1), nil
}
