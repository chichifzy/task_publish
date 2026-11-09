// Package server 提供 JSON API 和前端静态文件。
package server

import (
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
)

type Server struct {
	db           *sql.DB
	secureCookie bool
}

func New(db *sql.DB, static fs.FS, secureCookie bool) http.Handler {
	s := &Server{db: db, secureCookie: secureCookie}
	m := http.NewServeMux()

	// 认证
	m.HandleFunc("POST /api/auth/register", s.register)
	m.HandleFunc("POST /api/auth/login", s.login)
	m.HandleFunc("POST /api/auth/logout", s.logout)
	m.Handle("GET /api/me", s.requireUser(s.me))

	// 普通用户
	m.Handle("GET /api/tasks", s.requireUser(s.listTasks))
	m.Handle("POST /api/tasks/{id}/claim", s.requireUser(s.claimTask))

	// 管理员
	m.Handle("POST /api/admin/tasks", s.requireAdmin(s.createTask))
	m.Handle("PUT /api/admin/tasks/{id}", s.requireAdmin(s.updateTask))
	m.Handle("DELETE /api/admin/tasks/{id}", s.requireAdmin(s.deleteTask))

	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "接口不存在")
	})
	m.Handle("/", spaHandler(static))

	return logRequests(m)
}

// spaHandler 提供前端构建产物；找不到的路径返回 index.html，交给 React Router 处理。
func spaHandler(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(dist, p); err != nil {
			index, err := fs.ReadFile(dist, "index.html")
			if err != nil {
				http.Error(w, "前端尚未构建：请先运行 make web", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(index)
			return
		}
		// Vite 产物文件名带内容哈希，可以长期缓存
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			slog.Info("request", "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "duration", time.Since(start))
		}
	})
}
