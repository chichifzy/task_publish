package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "session"
	sessionTTL    = 7 * 24 * time.Hour
)

const (
	RoleAdmin  = "admin"
	RoleSuper  = "super"
	RoleMember = "member"
)

type User struct {
	ID                  int64  `json:"-"`
	UID                 string `json:"uid"`
	Nickname            string `json:"nickname"`
	Role                string `json:"role"`
	Points              int64  `json:"points"`
	AcceptAdminDispatch bool   `json:"acceptAdminDispatch"`
}

func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

// CanManagePrivate：管理员和高级用户可以修改自己的私有仓库，被撤销的高级用户仓库冻结。
func (u *User) CanManagePrivate() bool { return u.Role == RoleAdmin || u.Role == RoleSuper }

type ctxKey struct{}

func currentUser(r *http.Request) *User {
	u, _ := r.Context().Value(ctxKey{}).(*User)
	return u
}

var uidPattern = regexp.MustCompile(`^[A-Za-z0-9]{3,20}$`)

// ValidateUID 校验用户 ID：3-20 位英文字母或数字。
func ValidateUID(uid string) string {
	if !uidPattern.MatchString(uid) {
		return "用户 ID 需为 3-20 位英文字母或数字"
	}
	return ""
}

func ValidateNickname(nickname string) string {
	if n := utf8.RuneCountInString(nickname); n < 1 || n > 32 {
		return "用户名需为 1-32 个字符"
	}
	return ""
}

func ValidatePassword(password string) string {
	if len(password) < 8 || len(password) > 72 { // bcrypt 最多处理 72 字节
		return "密码需为 8-72 个字符"
	}
	return ""
}

// 用户不存在时也做一次 bcrypt 比较，避免通过响应时间判断用户 ID 是否存在。
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UID      string `json:"uid"`
		Nickname string `json:"nickname"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.UID = strings.TrimSpace(in.UID)
	in.Nickname = strings.TrimSpace(in.Nickname)
	for _, msg := range []string{ValidateUID(in.UID), ValidateNickname(in.Nickname), ValidatePassword(in.Password)} {
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		internalError(w, r, err)
		return
	}
	// 注册只能创建普通用户
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO users (uid, nickname, password_hash, role) VALUES (?, ?, ?, 'member')
		 ON CONFLICT (uid) DO NOTHING`, in.UID, in.Nickname, string(hash))
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusConflict, "用户 ID 已被使用")
		return
	}
	id, _ := res.LastInsertId()
	if err := s.startSession(w, r, id); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UID      string `json:"uid"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	var id int64
	var hash string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM users WHERE uid = ?`,
		strings.TrimSpace(in.UID)).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(in.Password))
		writeError(w, http.StatusUnauthorized, "用户 ID 或密码错误")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "用户 ID 或密码错误")
		return
	}
	if err := s.startSession(w, r, id); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, err := s.db.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE token_hash = ?`, hashToken(c.Value)); err != nil {
			internalError(w, r, err)
			return
		}
	}
	s.setCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

// me 返回当前用户，附带前端决定显示哪些入口所需的信息。
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	resp := struct {
		*User
		PendingReviews  int  `json:"pendingReviews"`  // 等待我审核的提交数
		HasPrivateTasks bool `json:"hasPrivateTasks"` // 私有仓库里是否有任务（被撤销的高级用户用来查看冻结仓库）
	}{User: u}
	err := s.db.QueryRowContext(r.Context(), `
		SELECT
			(SELECT COUNT(*) FROM assignments WHERE reviewer_id = ? AND status = 'submitted'),
			EXISTS (SELECT 1 FROM tasks WHERE owner_id = ?)`,
		u.ID, u.ID).Scan(&resp.PendingReviews, &resp.HasPrivateTasks)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// updateSettings 普通用户设置是否接受管理员派发任务。
func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AcceptAdminDispatch bool `json:"acceptAdminDispatch"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE users SET accept_admin_dispatch = ? WHERE id = ?`,
		in.AcceptAdminDispatch, currentUser(r).ID); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	now := time.Now()
	// 顺手清理过期会话
	if _, err := s.db.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE expires_at < ?`, now.Unix()); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, now.Add(sessionTTL).Unix()); err != nil {
		return err
	}
	s.setCookie(w, token, int(sessionTTL.Seconds()))
	return nil
}

func (s *Server) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// requireUser 校验会话，把当前用户放进 context。
// 角色每次请求都从数据库读取，撤销高级用户后立即生效。
func (s *Server) requireUser(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		var u User
		err = s.db.QueryRowContext(r.Context(),
			`SELECT u.id, u.uid, u.nickname, u.role, u.points, u.accept_admin_dispatch
			 FROM sessions s JOIN users u ON u.id = s.user_id
			 WHERE s.token_hash = ? AND s.expires_at > ?`,
			hashToken(c.Value), time.Now().Unix()).
			Scan(&u.ID, &u.UID, &u.Nickname, &u.Role, &u.Points, &u.AcceptAdminDispatch)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "登录已过期，请重新登录")
			return
		}
		if err != nil {
			internalError(w, r, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, &u)))
	})
}

// requireRole 只允许指定角色访问。
func (s *Server) requireRole(next http.HandlerFunc, roles ...string) http.Handler {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(roles, currentUser(r).Role) {
			writeError(w, http.StatusForbidden, "没有权限执行此操作")
			return
		}
		next(w, r)
	})
}
