package server

import (
	"net/http"
	"strings"
)

// escapeLike 转义 LIKE 通配符，配合 ESCAPE '\' 使用。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
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
		WHERE role != 'admin' AND parent_id IS NULL -- 管理员的身份账号不参与授予/撤销
		  AND (? = '' OR uid LIKE ? ESCAPE '\' OR nickname LIKE ? ESCAPE '\')
		ORDER BY role = 'super' DESC, id DESC
		LIMIT 200`, q, pattern, pattern)
	list, err := scanList(rows, err, func(v *userInfo) []any {
		return []any{&v.UID, &v.Nickname, &v.Role, &v.Points, &v.CreatedAt}
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// setUserRole 授予或撤销高级用户身份。
// 撤销后：私有仓库冻结（数据保留）、已派发的任务仍有效、普通用户的绑定保留但不能再派发。
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
		`UPDATE users SET role = ? WHERE uid = ? AND role IN ('super', 'member') AND parent_id IS NULL`,
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
