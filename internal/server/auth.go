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
	ID                  int64         `json:"-"`
	UID                 string        `json:"uid"`
	Nickname            string        `json:"nickname"`
	Role                string        `json:"role"`
	Points              int64         `json:"points"`
	AcceptAdminDispatch bool          `json:"acceptAdminDispatch"`
	ParentID            sql.NullInt64 `json:"-"` // 非空表示这是管理员切换出的身份账号
}

func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

// RootID 返回真实账号的 id：身份账号返回所属管理员，其他返回自身。
func (u *User) RootID() int64 {
	if u.ParentID.Valid {
		return u.ParentID.Int64
	}
	return u.ID
}

// identitySuffix 身份账号的用户 ID 后缀。注册的用户 ID 不允许下划线，不会冲突。
var identitySuffix = map[string]string{
	RoleSuper:  "_super_user",
	RoleMember: "_member_user",
}

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
		PendingReviews    int  `json:"pendingReviews"`    // 等待我审核的提交数
		HasPrivateTasks   bool `json:"hasPrivateTasks"`   // 私有仓库里是否有任务（被撤销的高级用户用来查看冻结仓库）
		CanSwitchIdentity bool `json:"canSwitchIdentity"` // 管理员及其身份账号可以切换身份
		NewBindRequests   bool `json:"newBindRequests"`   // 有未查看的绑定/解绑申请（高级用户）
		NewTasks          bool `json:"newTasks"`          // 有未查看的派发任务（普通用户）
	}{User: u, CanSwitchIdentity: u.IsAdmin() || u.ParentID.Valid}
	err := s.db.QueryRowContext(r.Context(), `
		SELECT
			(SELECT COUNT(*) FROM assignments WHERE reviewer_id = ? AND status = 'submitted'),
			EXISTS (SELECT 1 FROM tasks WHERE owner_id = ?),
			EXISTS (SELECT 1 FROM bindings WHERE super_id = ? AND seen = 0),
			EXISTS (SELECT 1 FROM assignments WHERE user_id = ? AND seen = 0)`,
		u.ID, u.ID, u.ID, u.ID).Scan(&resp.PendingReviews, &resp.HasPrivateTasks, &resp.NewBindRequests, &resp.NewTasks)
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

// switchIdentity 管理员在管理员、高级用户、普通用户三个身份间切换。
// 高级用户和普通用户身份是独立账号（首次切换时创建），用户名与管理员一致，
// 切换只是把当前会话指向目标账号，之后的权限、数据都按目标账号处理。
func (s *Server) switchIdentity(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Role != RoleAdmin && in.Role != RoleSuper && in.Role != RoleMember {
		writeError(w, http.StatusBadRequest, "身份只能是 admin、super 或 member")
		return
	}
	u := currentUser(r)
	rootID := u.RootID()
	var rootUID, nickname, rootRole string
	if err := s.db.QueryRowContext(r.Context(),
		`SELECT uid, nickname, role FROM users WHERE id = ?`, rootID).Scan(&rootUID, &nickname, &rootRole); err != nil {
		internalError(w, r, err)
		return
	}
	if rootRole != RoleAdmin {
		writeError(w, http.StatusForbidden, "只有管理员可以切换身份")
		return
	}
	c, _ := r.Cookie(sessionCookie) // requireUser 已保证存在
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		targetID := rootID
		if in.Role != RoleAdmin {
			if _, err := tx.ExecContext(r.Context(),
				`INSERT INTO users (uid, nickname, password_hash, role, parent_id) VALUES (?, ?, '!', ?, ?)
				 ON CONFLICT DO NOTHING`,
				rootUID+identitySuffix[in.Role], nickname, in.Role, rootID); err != nil {
				return err
			}
			if err := tx.QueryRowContext(r.Context(),
				`SELECT id FROM users WHERE parent_id = ? AND role = ?`, rootID, in.Role).Scan(&targetID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(r.Context(),
			`UPDATE sessions SET user_id = ? WHERE token_hash = ?`, targetID, hashToken(c.Value))
		return err
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateProfile 修改自己的用户名，校验规则与注册相同。
// 管理员和它的身份账号用户名保持一致，一起修改。
func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Nickname string `json:"nickname"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Nickname = strings.TrimSpace(in.Nickname)
	if msg := ValidateNickname(in.Nickname); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE users SET nickname = ?1 WHERE id = ?2 OR parent_id = ?2`, in.Nickname, currentUser(r).RootID()); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// changePassword 校验原密码后修改密码。当前会话保持有效，其他设备上的会话全部失效。
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if msg := ValidatePassword(in.NewPassword); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	u := currentUser(r)
	// 身份账号没有自己的密码，修改的是所属管理员的密码
	rootID := u.RootID()
	var hash string
	if err := s.db.QueryRowContext(r.Context(),
		`SELECT password_hash FROM users WHERE id = ?`, rootID).Scan(&hash); err != nil {
		internalError(w, r, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.OldPassword)) != nil {
		writeError(w, http.StatusBadRequest, "原密码错误")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		internalError(w, r, err)
		return
	}
	c, _ := r.Cookie(sessionCookie) // requireUser 已保证存在
	err = s.withTx(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE users SET password_hash = ? WHERE id = ?`, string(newHash), rootID); err != nil {
			return err
		}
		_, err := tx.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE token_hash <> ?1
			   AND user_id IN (SELECT id FROM users WHERE id = ?2 OR parent_id = ?2)`, hashToken(c.Value), rootID)
		return err
	})
	if err != nil {
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
			`SELECT u.id, u.uid, u.nickname, u.role, u.points, u.accept_admin_dispatch, u.parent_id
			 FROM sessions s JOIN users u ON u.id = s.user_id
			 WHERE s.token_hash = ? AND s.expires_at > ?`,
			hashToken(c.Value), time.Now().Unix()).
			Scan(&u.ID, &u.UID, &u.Nickname, &u.Role, &u.Points, &u.AcceptAdminDispatch, &u.ParentID)
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
