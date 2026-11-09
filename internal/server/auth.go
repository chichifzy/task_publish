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
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "session"
	sessionTTL    = 7 * 24 * time.Hour
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (u *User) IsAdmin() bool { return u.Role == "admin" }

type ctxKey struct{}

func currentUser(r *http.Request) *User {
	u, _ := r.Context().Value(ctxKey{}).(*User)
	return u
}

// 用户不存在时也做一次 bcrypt 比较，避免通过响应时间判断用户名是否存在。
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c *credentials) validate() string {
	c.Username = strings.TrimSpace(c.Username)
	if n := utf8.RuneCountInString(c.Username); n < 3 || n > 32 {
		return "用户名需为 3-32 个字符"
	}
	if len(c.Password) < 8 || len(c.Password) > 72 { // bcrypt 最多处理 72 字节
		return "密码需为 8-72 个字符"
	}
	return ""
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decodeJSON(w, r, &c) {
		return
	}
	if msg := c.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		internalError(w, r, err)
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO users (username, password_hash) VALUES (?, ?)
		 ON CONFLICT (username) DO NOTHING`, c.Username, string(hash))
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusConflict, "用户名已存在")
		return
	}
	id, _ := res.LastInsertId()
	u := &User{ID: id, Username: c.Username, Role: "user"}
	if err := s.startSession(w, r, u.ID); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decodeJSON(w, r, &c) {
		return
	}
	var u User
	var hash string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, username, role, password_hash FROM users WHERE username = ?`,
		strings.TrimSpace(c.Username)).Scan(&u.ID, &u.Username, &u.Role, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(c.Password))
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(c.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, &u)
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

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
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
func (s *Server) requireUser(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		var u User
		err = s.db.QueryRowContext(r.Context(),
			`SELECT u.id, u.username, u.role
			 FROM sessions s JOIN users u ON u.id = s.user_id
			 WHERE s.token_hash = ? AND s.expires_at > ?`,
			hashToken(c.Value), time.Now().Unix()).Scan(&u.ID, &u.Username, &u.Role)
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

func (s *Server) requireAdmin(next http.HandlerFunc) http.Handler {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin() {
			writeError(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next(w, r)
	})
}
