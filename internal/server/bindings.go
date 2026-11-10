package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

// 绑定：普通用户申请绑定高级用户，高级用户同意后才能向其派发任务。
// 状态 pending_bind（待同意绑定）→ bound（已绑定）→ pending_unbind（申请解绑中，仍可派发）。
// 普通用户强制解绑会立即解除，并把两个账号加入黑名单（bind_blacklist），之后无论角色如何变化都不能再绑定。

const (
	bindPending       = "pending_bind"
	bindBound         = "bound"
	bindPendingUnbind = "pending_unbind"
)

// pair 返回黑名单中的有序账号对。
func pair(x, y int64) (int64, int64) {
	if x < y {
		return x, y
	}
	return y, x
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func isBlocked(ctx context.Context, q queryer, x, y int64) (bool, error) {
	a, b := pair(x, y)
	var blocked bool
	err := q.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM bind_blacklist WHERE user_a = ? AND user_b = ?)`, a, b).Scan(&blocked)
	return blocked, err
}

// userIDByUID 查找用户；role 非空时要求角色匹配。
func userIDByUID(ctx context.Context, q queryer, uid, role string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx,
		`SELECT id FROM users WHERE uid = ? AND (? = '' OR role = ?)`, strings.TrimSpace(uid), role, role).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errStatus(http.StatusNotFound, "用户不存在")
	}
	return id, err
}

func scanList[T any](rows *sql.Rows, err error, scan func(*T) []any) ([]T, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []T{}
	for rows.Next() {
		var v T
		if err := rows.Scan(scan(&v)...); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

// ---------- 普通用户 ----------

type superInfo struct {
	UID      string `json:"uid"`
	Nickname string `json:"nickname"`
	Status   string `json:"status"` // 空、pending_bind、bound、pending_unbind、blocked
}

// searchSupers 按用户 ID 或用户名模糊搜索高级用户，用户 ID 完全一致的排在最前。
func (s *Server) searchSupers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 1 || n > 32 {
		writeError(w, http.StatusBadRequest, "请输入 1-32 个字符的用户 ID 或用户名")
		return
	}
	me := currentUser(r).ID
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT u.uid, u.nickname,
		       CASE WHEN EXISTS (SELECT 1 FROM bind_blacklist k
		                         WHERE k.user_a = min(u.id, ?1) AND k.user_b = max(u.id, ?1)) THEN 'blocked'
		            ELSE coalesce((SELECT b.status FROM bindings b WHERE b.member_id = ?1 AND b.super_id = u.id), '')
		       END
		FROM users u
		WHERE u.role = 'super' AND (u.uid LIKE ?3 ESCAPE '\' OR u.nickname LIKE ?3 ESCAPE '\')
		ORDER BY (u.uid = ?2 COLLATE NOCASE) DESC, u.uid
		LIMIT 20`, me, q, "%"+escapeLike(q)+"%")
	list, err := scanList(rows, err, func(v *superInfo) []any { return []any{&v.UID, &v.Nickname, &v.Status} })
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type bindingInfo struct {
	UID      string `json:"uid"`
	Nickname string `json:"nickname"`
	Status   string `json:"status"`
	Active   bool   `json:"active"` // false 表示对方已被撤销高级用户身份，绑定保留但不能派发
}

func (s *Server) listMyBindings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT u.uid, u.nickname, b.status, u.role = 'super'
		FROM bindings b JOIN users u ON u.id = b.super_id
		WHERE b.member_id = ? ORDER BY b.created_at`, currentUser(r).ID)
	list, err := scanList(rows, err, func(v *bindingInfo) []any { return []any{&v.UID, &v.Nickname, &v.Status, &v.Active} })
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// applyBind 普通用户申请绑定一个高级用户。
func (s *Server) applyBind(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UID string `json:"uid"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	me := currentUser(r).ID
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		superID, err := userIDByUID(r.Context(), tx, in.UID, RoleSuper)
		if err != nil {
			return err
		}
		if blocked, err := isBlocked(r.Context(), tx, me, superID); err != nil {
			return err
		} else if blocked {
			return errStatus(http.StatusForbidden, "你与该用户处于不可绑定状态，可向管理员申请解除")
		}
		res, err := tx.ExecContext(r.Context(),
			`INSERT INTO bindings (member_id, super_id, status, seen) VALUES (?, ?, 'pending_bind', 0) ON CONFLICT DO NOTHING`,
			me, superID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errStatus(http.StatusConflict, "已经申请或已经绑定该用户")
		}
		return nil
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// memberBindingAction 普通用户对某个绑定执行操作：
// cancel 撤回绑定申请或解绑申请；unbind 申请解绑；force 强制解绑并加入黑名单。
func (s *Server) memberBindingAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	me := currentUser(r).ID
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		superID, err := userIDByUID(r.Context(), tx, r.PathValue("uid"), "")
		if err != nil {
			return err
		}
		var status string
		err = tx.QueryRowContext(r.Context(),
			`SELECT status FROM bindings WHERE member_id = ? AND super_id = ?`, me, superID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "没有与该用户的绑定")
		}
		if err != nil {
			return err
		}
		var q string
		switch {
		case action == "cancel" && status == bindPending:
			q = `DELETE FROM bindings WHERE member_id = ? AND super_id = ?`
		case action == "cancel" && status == bindPendingUnbind:
			q = `UPDATE bindings SET status = 'bound', seen = 1 WHERE member_id = ? AND super_id = ?`
		case action == "unbind" && status == bindBound:
			q = `UPDATE bindings SET status = 'pending_unbind', seen = 0 WHERE member_id = ? AND super_id = ?`
		case action == "force" && status != bindPending:
			a, b := pair(me, superID)
			if _, err := tx.ExecContext(r.Context(),
				`INSERT INTO bind_blacklist (user_a, user_b, blocked_by) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
				a, b, me); err != nil {
				return err
			}
			q = `DELETE FROM bindings WHERE member_id = ? AND super_id = ?`
		default:
			return errStatus(http.StatusConflict, "当前状态不能执行此操作")
		}
		_, err = tx.ExecContext(r.Context(), q, me, superID)
		return err
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 高级用户：Member 管理 ----------

type memberInfo struct {
	UID             string  `json:"uid"`
	Nickname        string  `json:"nickname"`
	Points          int64   `json:"points"`
	ExclusivePoints int64   `json:"exclusivePoints"` // 在当前高级用户处的专有积分
	CreatedAt       string  `json:"createdAt"`       // 注册时间
	Status          string  `json:"status"`
	AppliedAt       string  `json:"appliedAt"`
	BoundAt         *string `json:"boundAt"`
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT u.uid, u.nickname, u.points,
		       COALESCE((SELECT e.points FROM exclusive_points e WHERE e.user_id = u.id AND e.super_id = b.super_id), 0),
		       u.created_at, b.status, b.created_at, b.bound_at
		FROM bindings b JOIN users u ON u.id = b.member_id
		WHERE b.super_id = ?
		ORDER BY b.status = 'bound', b.created_at`, currentUser(r).ID)
	list, err := scanList(rows, err, func(v *memberInfo) []any {
		return []any{&v.UID, &v.Nickname, &v.Points, &v.ExclusivePoints, &v.CreatedAt, &v.Status, &v.AppliedAt, &v.BoundAt}
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	// 打开 Member管理 即视为已查看，红点消失
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE bindings SET seen = 1 WHERE super_id = ? AND seen = 0`, currentUser(r).ID); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// superBindingAction 高级用户处理申请或直接解绑：approve 同意、reject 拒绝、unbind 直接解绑（不进黑名单）。
func (s *Server) superBindingAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	me := currentUser(r).ID
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		memberID, err := userIDByUID(r.Context(), tx, r.PathValue("uid"), "")
		if err != nil {
			return err
		}
		var status string
		err = tx.QueryRowContext(r.Context(),
			`SELECT status FROM bindings WHERE member_id = ? AND super_id = ?`, memberID, me).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "没有与该用户的绑定")
		}
		if err != nil {
			return err
		}
		const del = `DELETE FROM bindings WHERE member_id = ? AND super_id = ?`
		var q string
		switch {
		case action == "approve" && status == bindPending:
			q = `UPDATE bindings SET status = 'bound', bound_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			     WHERE member_id = ? AND super_id = ?`
		case action == "approve" && status == bindPendingUnbind,
			action == "reject" && status == bindPending,
			action == "unbind" && status != bindPending:
			q = del
		case action == "reject" && status == bindPendingUnbind:
			q = `UPDATE bindings SET status = 'bound' WHERE member_id = ? AND super_id = ?`
		default:
			return errStatus(http.StatusConflict, "当前状态不能执行此操作")
		}
		_, err = tx.ExecContext(r.Context(), q, memberID, me)
		return err
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 不可绑定名单 ----------

type blockInfo struct {
	UID      string `json:"uid"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"`
	ByMe     bool   `json:"byMe"`
	Pending  bool   `json:"pending"` // 已有待审核的解除申请
}

// listMyBlocks 列出与我处于不可绑定状态的账号。
func (s *Server) listMyBlocks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT u.uid, u.nickname, u.role, k.blocked_by = ?1,
		       EXISTS (SELECT 1 FROM unblock_requests q
		               WHERE q.user_a = k.user_a AND q.user_b = k.user_b AND q.status = 'pending')
		FROM bind_blacklist k JOIN users u ON u.id = CASE WHEN k.user_a = ?1 THEN k.user_b ELSE k.user_a END
		WHERE ?1 IN (k.user_a, k.user_b)
		ORDER BY k.created_at DESC`, currentUser(r).ID)
	list, err := scanList(rows, err, func(v *blockInfo) []any {
		return []any{&v.UID, &v.Nickname, &v.Role, &v.ByMe, &v.Pending}
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// requestUnblock 账号对的任意一方向管理员申请解除不可绑定状态。
func (s *Server) requestUnblock(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r).ID
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		other, err := userIDByUID(r.Context(), tx, r.PathValue("uid"), "")
		if err != nil {
			return err
		}
		if blocked, err := isBlocked(r.Context(), tx, me, other); err != nil {
			return err
		} else if !blocked {
			return errStatus(http.StatusNotFound, "你与该用户不在不可绑定名单中")
		}
		a, b := pair(me, other)
		res, err := tx.ExecContext(r.Context(),
			`INSERT INTO unblock_requests (user_a, user_b, requester_id) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
			a, b, me)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errStatus(http.StatusConflict, "已有待审核的申请")
		}
		return nil
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 管理员 ----------

type adminBlockInfo struct {
	A         userRef `json:"a"`
	B         userRef `json:"b"`
	BlockedBy string  `json:"blockedBy"`
	CreatedAt string  `json:"createdAt"`
	RequestID *int64  `json:"requestId"` // 待审核的解除申请
	Requester *string `json:"requester"`
}

func (s *Server) adminListBlocks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT ua.uid, ua.nickname, ub.uid, ub.nickname, ux.uid, k.created_at, q.id, ur.uid
		FROM bind_blacklist k
		JOIN users ua ON ua.id = k.user_a
		JOIN users ub ON ub.id = k.user_b
		JOIN users ux ON ux.id = k.blocked_by
		LEFT JOIN unblock_requests q ON q.user_a = k.user_a AND q.user_b = k.user_b AND q.status = 'pending'
		LEFT JOIN users ur ON ur.id = q.requester_id
		ORDER BY q.id IS NULL, k.created_at DESC`)
	list, err := scanList(rows, err, func(v *adminBlockInfo) []any {
		return []any{&v.A.UID, &v.A.Nickname, &v.B.UID, &v.B.Nickname, &v.BlockedBy, &v.CreatedAt, &v.RequestID, &v.Requester}
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// adminResolveBlock 管理员处理账号对：remove 解除不可绑定（同时通过待审核的申请）；reject 拒绝解除申请。
func (s *Server) adminResolveBlock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		A      string `json:"a"`
		B      string `json:"b"`
		Remove bool   `json:"remove"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		x, err := userIDByUID(r.Context(), tx, in.A, "")
		if err != nil {
			return err
		}
		y, err := userIDByUID(r.Context(), tx, in.B, "")
		if err != nil {
			return err
		}
		a, b := pair(x, y)
		newStatus := "rejected"
		if in.Remove {
			newStatus = "approved"
			res, err := tx.ExecContext(r.Context(),
				`DELETE FROM bind_blacklist WHERE user_a = ? AND user_b = ?`, a, b)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return errStatus(http.StatusNotFound, "该账号对不在不可绑定名单中")
			}
		}
		res, err := tx.ExecContext(r.Context(),
			`UPDATE unblock_requests SET status = ? WHERE user_a = ? AND user_b = ? AND status = 'pending'`,
			newStatus, a, b)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 && !in.Remove {
			return errStatus(http.StatusNotFound, "没有待审核的申请")
		}
		return nil
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
