package server

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

// escapeLike 转义 LIKE 通配符，配合 ESCAPE '\' 使用。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ---------- 普通用户：搜索并授权高级用户 ----------

type superInfo struct {
	UID        string `json:"uid"`
	Nickname   string `json:"nickname"`
	Authorized bool   `json:"authorized"`
}

// searchSupers 按用户 ID 或用户名搜索高级用户。用户 ID 精确匹配（不区分大小写），用户名模糊匹配。
func (s *Server) searchSupers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 1 || n > 32 {
		writeError(w, http.StatusBadRequest, "请输入 1-32 个字符的用户 ID 或用户名")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT u.uid, u.nickname,
		       EXISTS (SELECT 1 FROM authorizations a WHERE a.member_id = ? AND a.super_id = u.id)
		FROM users u
		WHERE u.role = 'super' AND (u.uid = ? OR u.nickname LIKE ? ESCAPE '\')
		ORDER BY (u.uid = ?) DESC, u.uid
		LIMIT 20`, currentUser(r).ID, q, "%"+escapeLike(q)+"%", q)
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer rows.Close()
	list := []superInfo{}
	for rows.Next() {
		var si superInfo
		if err := rows.Scan(&si.UID, &si.Nickname, &si.Authorized); err != nil {
			internalError(w, r, err)
			return
		}
		list = append(list, si)
	}
	if err := rows.Err(); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type authorizationInfo struct {
	UID      string `json:"uid"`
	Nickname string `json:"nickname"`
	Active   bool   `json:"active"` // false 表示对方已被撤销高级用户身份，授权保留但不能派发
}

func (s *Server) listAuthorizations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT u.uid, u.nickname, u.role = 'super'
		FROM authorizations a JOIN users u ON u.id = a.super_id
		WHERE a.member_id = ? ORDER BY a.created_at`, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer rows.Close()
	list := []authorizationInfo{}
	for rows.Next() {
		var ai authorizationInfo
		if err := rows.Scan(&ai.UID, &ai.Nickname, &ai.Active); err != nil {
			internalError(w, r, err)
			return
		}
		list = append(list, ai)
	}
	if err := rows.Err(); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// addAuthorization 按用户 ID 授权一个高级用户。一次只能授权一个。
func (s *Server) addAuthorization(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UID string `json:"uid"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		INSERT INTO authorizations (member_id, super_id)
		SELECT ?, id FROM users WHERE uid = ? AND role = 'super'
		ON CONFLICT DO NOTHING`, currentUser(r).ID, strings.TrimSpace(in.UID))
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists bool
		if err := s.db.QueryRowContext(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM users WHERE uid = ? AND role = 'super')`,
			strings.TrimSpace(in.UID)).Scan(&exists); err != nil {
			internalError(w, r, err)
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "未找到该高级用户")
			return
		}
		// 已经授权过，视为成功
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeAuthorization(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.ExecContext(r.Context(), `
		DELETE FROM authorizations
		WHERE member_id = ? AND super_id = (SELECT id FROM users WHERE uid = ?)`,
		currentUser(r).ID, r.PathValue("uid"))
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "没有对该用户的授权")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 管理员：用户管理 ----------

type userInfo struct {
	UID       string `json:"uid"`
	Nickname  string `json:"nickname"`
	Role      string `json:"role"`
	Points    int64  `json:"points"`
	CreatedAt string `json:"createdAt"`
}

// listUsers 列出普通用户和高级用户，可按用户 ID 或用户名搜索。
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	pattern := "%" + escapeLike(q) + "%"
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT uid, nickname, role, points, created_at FROM users
		WHERE role != 'admin'
		  AND (? = '' OR uid LIKE ? ESCAPE '\' OR nickname LIKE ? ESCAPE '\')
		ORDER BY role = 'super' DESC, id DESC
		LIMIT 200`, q, pattern, pattern)
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer rows.Close()
	list := []userInfo{}
	for rows.Next() {
		var ui userInfo
		if err := rows.Scan(&ui.UID, &ui.Nickname, &ui.Role, &ui.Points, &ui.CreatedAt); err != nil {
			internalError(w, r, err)
			return
		}
		list = append(list, ui)
	}
	if err := rows.Err(); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// setUserRole 授予或撤销高级用户身份。
// 撤销后：私有仓库冻结（数据保留）、已派发的任务仍有效、普通用户的授权保留但不能再派发。
// 重新授予后私有仓库自动恢复。
func (s *Server) setUserRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Role != RoleSuper && in.Role != RoleMember {
		writeError(w, http.StatusBadRequest, "角色只能是 super 或 member")
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE users SET role = ? WHERE uid = ? AND role IN ('super', 'member')`,
		in.Role, r.PathValue("uid"))
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
