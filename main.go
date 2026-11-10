package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"task_publish/internal/db"
	"task_publish/internal/server"
	"task_publish/web"
)

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	dbPath := flag.String("db", "data.db", "SQLite 数据库文件路径")
	secureCookie := flag.Bool("secure-cookie", false, "为会话 Cookie 设置 Secure 标记（HTTPS 部署时开启）")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `用法:
  %[1]s [flags]                                启动服务
  %[1]s [flags] create-admin <用户ID> <用户名>  创建唯一的管理员（随后输入密码）

flags:
`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	database, err := db.Open(*dbPath)
	if err != nil {
		slog.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	switch flag.Arg(0) {
	case "":
		// 启动服务
	case "create-admin":
		if flag.NArg() != 3 {
			flag.Usage()
			os.Exit(2)
		}
		if err := createAdmin(database, flag.Arg(1), flag.Arg(2)); err != nil {
			fmt.Fprintln(os.Stderr, "创建管理员失败:", err)
			os.Exit(1)
		}
		fmt.Printf("管理员 %s 已创建\n", flag.Arg(1))
		return
	default:
		flag.Usage()
		os.Exit(2)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(database, web.Dist(), *secureCookie),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("服务已启动", "addr", *addr, "db", *dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("服务异常退出", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("关闭服务失败", "err", err)
	}
}
