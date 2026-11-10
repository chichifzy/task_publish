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

	const (
		admin  = RoleAdmin
		super  = RoleSuper
		member = RoleMember
	)

	// 认证与个人信息
	m.HandleFunc("POST /api/auth/register", s.register)
	m.HandleFunc("POST /api/auth/login", s.login)
	m.HandleFunc("POST /api/auth/logout", s.logout)
	m.Handle("GET /api/me", s.requireUser(s.me))
	m.Handle("PUT /api/me/settings", s.requireRole(s.updateSettings, member))
	m.Handle("PUT /api/me/profile", s.requireUser(s.updateProfile))
	m.Handle("PUT /api/me/password", s.requireUser(s.changePassword))
	m.Handle("POST /api/me/identity", s.requireUser(s.switchIdentity)) // 处理函数内校验是否属于管理员

	// 公共任务仓库
	m.Handle("GET /api/public/tasks", s.requireUser(s.listPublicTasks))
	m.Handle("POST /api/public/tasks", s.requireRole(s.createPublicTask, admin))
	m.Handle("PUT /api/public/tasks/{id}", s.requireRole(s.updatePublicTask, admin))
	m.Handle("DELETE /api/public/tasks/{id}", s.requireRole(s.deletePublicTask, admin))
	m.Handle("POST /api/public/tasks/{id}/claim", s.requireRole(s.claimPublicTask, member))
	m.Handle("POST /api/public/tasks/{id}/copy", s.requireRole(s.copyPublicTask, admin, super))

	// 随机抽取（仅管理员和高级用户可见的任务）
	m.Handle("GET /api/draw", s.requireRole(s.currentDraw, member))
	m.Handle("POST /api/draw", s.requireRole(s.drawTask, member))
	m.Handle("POST /api/draw/accept", s.requireRole(s.acceptDraw, member))
	m.Handle("DELETE /api/draw", s.requireRole(s.discardDraw, member))

	// 私有任务仓库：被撤销的高级用户仍可查看（冻结），但不能修改和派发
	m.Handle("GET /api/private/tasks", s.requireUser(s.listPrivateTasks))
	m.Handle("POST /api/private/tasks", s.requireRole(s.createPrivateTask, admin, super))
	m.Handle("PUT /api/private/tasks/{id}", s.requireRole(s.updatePrivateTask, admin, super))
	m.Handle("DELETE /api/private/tasks/{id}", s.requireRole(s.deletePrivateTask, admin, super))
	m.Handle("POST /api/private/tasks/{id}/dispatch", s.requireRole(s.dispatchTask, admin, super))
	m.Handle("GET /api/dispatch-targets", s.requireRole(s.listDispatchTargets, admin, super))

	// 接取记录：自己的任务、提交、放弃；审核（谁是审核人谁能看）
	m.Handle("GET /api/assignments/mine", s.requireUser(s.listMyAssignments))
	m.Handle("POST /api/assignments/{id}/submit", s.requireUser(s.submitAssignment))
	m.Handle("POST /api/assignments/{id}/abandon", s.requireUser(s.abandonAssignment))
	m.Handle("GET /api/reviews", s.requireUser(s.listReviews))
	m.Handle("POST /api/assignments/{id}/review", s.requireUser(s.reviewAssignment))
	m.Handle("POST /api/assignments/{id}/cancel", s.requireRole(s.cancelAssignment, admin, super))
	// 任务记录：普通用户看自己已结束的任务，高级用户和管理员看自己派发的任务
	m.Handle("GET /api/records", s.requireUser(s.listRecords))
	m.Handle("DELETE /api/records/{id}", s.requireUser(s.deleteRecord))

	// 绑定：普通用户申请绑定高级用户，高级用户同意后可以向其派发任务
	m.Handle("GET /api/supers", s.requireRole(s.searchSupers, member))
	m.Handle("GET /api/bindings", s.requireRole(s.listMyBindings, member))
	m.Handle("POST /api/bindings", s.requireRole(s.applyBind, member))
	m.Handle("POST /api/bindings/{uid}/{action}", s.requireRole(s.memberBindingAction, member)) // cancel / unbind / force
	m.Handle("GET /api/members", s.requireRole(s.listMembers, super))
	m.Handle("POST /api/members/{uid}/{action}", s.requireRole(s.superBindingAction, super)) // approve / reject / unbind
	// 不可绑定名单：任意一方可以向管理员申请解除
	m.Handle("GET /api/blocks", s.requireRole(s.listMyBlocks, super, member))
	m.Handle("POST /api/blocks/{uid}/request", s.requireRole(s.requestUnblock, super, member))

	// 管理员：用户管理（授予/撤销高级用户）、不可绑定名单
	m.Handle("GET /api/admin/users", s.requireRole(s.listUsers, admin))
	m.Handle("PUT /api/admin/users/{uid}/role", s.requireRole(s.setUserRole, admin))
	m.Handle("GET /api/admin/blocks", s.requireRole(s.adminListBlocks, admin))
	m.Handle("POST /api/admin/blocks/resolve", s.requireRole(s.adminResolveBlock, admin))

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
