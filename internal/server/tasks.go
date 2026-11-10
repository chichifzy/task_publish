package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

const (
	maxPoints         = 1_000_000_000 // 管理员任务积分上限，防止累加溢出
	maxSuperPoints    = 9             // 高级用户私有任务积分上限
	maxDescriptionLen = 5000
)

// activeStatuses：处于这两个状态的接取记录视为“未完成”，同一用户不能重复接取同一任务。
const activeStatuses = `('in_progress', 'submitted')`

type Task struct {
	ID          int64  `json:"id"`
	Public      bool   `json:"public"`
	Description string `json:"description"`
	Points      int64  `json:"points"`
	Visibility  string `json:"visibility"` // 仅公共任务有意义
	Enabled     bool   `json:"enabled"`    // 仅公共任务有意义
	CopiedFrom  *int64 `json:"copiedFrom,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	ActiveCount *int   `json:"activeCount,omitempty"` // 进行中的人数，只返回给管理员
	MyActive    bool   `json:"myActive"`              // 当前用户是否正在进行此任务
}

func validateDescription(d *string) error {
	*d = strings.TrimSpace(*d)
	if n := utf8.RuneCountInString(*d); n < 1 || n > maxDescriptionLen {
		return errStatus(http.StatusBadRequest, "任务描述需为 1-5000 个字符")
	}
	return nil
}

func validatePoints(p int64, max int64) error {
	if p < 1 || p > max {
		if max == maxSuperPoints {
			return errStatus(http.StatusBadRequest, "高级用户的任务积分需为 1-9 的整数")
		}
		return errStatus(http.StatusBadRequest, "积分需为不小于 1 的整数")
	}
	return nil
}

// ---------- 公共任务仓库 ----------

func (s *Server) listPublicTasks(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var filter string
	switch u.Role {
	case RoleAdmin:
		filter = "1"
	case RoleSuper:
		filter = "t.enabled = 1"
	default:
		filter = "t.enabled = 1 AND t.visibility = 'all'"
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT t.id, t.description, t.points, t.visibility, t.enabled, t.created_at, t.updated_at,
		       (SELECT COUNT(*) FROM assignments a
		        WHERE a.task_id = t.id AND a.status IN `+activeStatuses+`),
		       EXISTS (SELECT 1 FROM assignments a
		        WHERE a.task_id = t.id AND a.user_id = ? AND a.status IN `+activeStatuses+`)
		FROM tasks t
		WHERE t.owner_id IS NULL AND `+filter+`
		ORDER BY t.id DESC`, u.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer rows.Close()

	tasks := []Task{}
	for rows.Next() {
		t := Task{Public: true}
		var active int
		if err := rows.Scan(&t.ID, &t.Description, &t.Points, &t.Visibility, &t.Enabled,
			&t.CreatedAt, &t.UpdatedAt, &active, &t.MyActive); err != nil {
			internalError(w, r, err)
			return
		}
		if u.IsAdmin() {
			t.ActiveCount = &active
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

type publicTaskInput struct {
	Description string `json:"description"`
	Points      int64  `json:"points"`
	Visibility  string `json:"visibility"`
	Enabled     *bool  `json:"enabled"`
}

func (in *publicTaskInput) validate() error {
	if err := validateDescription(&in.Description); err != nil {
		return err
	}
	if err := validatePoints(in.Points, maxPoints); err != nil {
		return err
	}
	if in.Visibility == "" {
		in.Visibility = "all"
	}
	if in.Visibility != "all" && in.Visibility != "privileged" {
		return errStatus(http.StatusBadRequest, "可见范围只能是 all 或 privileged")
	}
	if in.Enabled == nil {
		t := true
		in.Enabled = &t
	}
	return nil
}

func (s *Server) createPublicTask(w http.ResponseWriter, r *http.Request) {
	var in publicTaskInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.validate(); err != nil {
		fail(w, r, err)
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO tasks (owner_id, description, points, visibility, enabled) VALUES (NULL, ?, ?, ?, ?)`,
		in.Description, in.Points, in.Visibility, *in.Enabled)
	if err != nil {
		internalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) updatePublicTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in publicTaskInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.validate(); err != nil {
		fail(w, r, err)
		return
	}
	// 已接取的任务保存了快照，修改原任务不影响它们
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE tasks SET description = ?, points = ?, visibility = ?, enabled = ?,
		       updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ? AND owner_id IS NULL`,
		in.Description, in.Points, in.Visibility, *in.Enabled, id)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deletePublicTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM tasks WHERE id = ? AND owner_id IS NULL`, id)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// claimPublicTask 普通用户主动接取启用且全体可见的公共任务，由管理员审核。
func (s *Server) claimPublicTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var assignmentID int64
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		var desc string
		var points int64
		err := tx.QueryRowContext(r.Context(), `
			SELECT description, points FROM tasks
			WHERE id = ? AND owner_id IS NULL AND enabled = 1 AND visibility = 'all'`, id).Scan(&desc, &points)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "任务不存在或不可接取")
		}
		if err != nil {
			return err
		}
		reviewer, err := adminID(r.Context(), tx)
		if err != nil {
			return err
		}
		assignmentID, err = insertAssignment(r.Context(), tx, newAssignment{
			TaskID: id, Public: true, UserID: currentUser(r).ID, ReviewerID: reviewer,
			Source: "claim", Description: desc, Points: points,
		})
		return err
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"assignmentId": assignmentID})
}

// copyPublicTask 把自己能看到的公共任务复制为私有仓库中的独立副本，保留原积分。
func (s *Server) copyPublicTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	res, err := s.db.ExecContext(r.Context(), `
		INSERT INTO tasks (owner_id, description, points, copied_from)
		SELECT ?, description, points, id FROM tasks
		WHERE id = ? AND owner_id IS NULL AND (? OR enabled = 1)`,
		u.ID, id, u.IsAdmin())
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "任务不存在或不可复制")
		return
	}
	newID, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]int64{"id": newID})
}

// ---------- 随机抽取 ----------
// 抽到的任务记录在服务端，接口只返回积分值；接取时按服务端记录创建接取记录。

func (s *Server) currentDraw(w http.ResponseWriter, r *http.Request) {
	var points int64
	err := s.db.QueryRowContext(r.Context(), `
		SELECT t.points FROM draws d JOIN tasks t ON t.id = d.task_id
		WHERE d.user_id = ?`, currentUser(r).ID).Scan(&points)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"points": points})
}

func (s *Server) drawTask(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var points int64
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		var taskID int64
		// 跳过用户正在进行的任务
		err := tx.QueryRowContext(r.Context(), `
			SELECT t.id, t.points FROM tasks t
			WHERE t.owner_id IS NULL AND t.enabled = 1 AND t.visibility = 'privileged'
			  AND NOT EXISTS (SELECT 1 FROM assignments a
			      WHERE a.task_id = t.id AND a.user_id = ? AND a.status IN `+activeStatuses+`)
			ORDER BY random() LIMIT 1`, u.ID).Scan(&taskID, &points)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "暂时没有可抽取的任务")
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO draws (user_id, task_id) VALUES (?, ?)
			ON CONFLICT (user_id) DO UPDATE SET task_id = excluded.task_id,
			    created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`, u.ID, taskID)
		return err
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"points": points})
}

func (s *Server) acceptDraw(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var assignmentID int64
	var unavailable bool
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		var taskID, points int64
		var desc, visibility string
		var enabled bool
		err := tx.QueryRowContext(r.Context(), `
			SELECT t.id, t.description, t.points, t.visibility, t.enabled
			FROM draws d JOIN tasks t ON t.id = d.task_id
			WHERE d.user_id = ?`, u.ID).Scan(&taskID, &desc, &points, &visibility, &enabled)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "没有待接取的抽取结果，请先抽取")
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM draws WHERE user_id = ?`, u.ID); err != nil {
			return err
		}
		// 抽到之后任务被禁用或改为全体可见：清掉抽取结果并提示重新抽取
		if !enabled || visibility != "privileged" {
			unavailable = true
			return nil
		}
		reviewer, err := adminID(r.Context(), tx)
		if err != nil {
			return err
		}
		assignmentID, err = insertAssignment(r.Context(), tx, newAssignment{
			TaskID: taskID, Public: true, UserID: u.ID, ReviewerID: reviewer,
			Source: "draw", Description: desc, Points: points,
		})
		return err
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	if unavailable {
		writeError(w, http.StatusConflict, "该任务已不可接取，请重新抽取")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"assignmentId": assignmentID})
}

func (s *Server) discardDraw(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.ExecContext(r.Context(),
		`DELETE FROM draws WHERE user_id = ?`, currentUser(r).ID); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 私有任务仓库 ----------

func (s *Server) listPrivateTasks(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, description, points, copied_from, created_at, updated_at
		FROM tasks WHERE owner_id = ? ORDER BY id DESC`, u.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer rows.Close()

	tasks := []Task{}
	for rows.Next() {
		var t Task
		var copied sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Description, &t.Points, &copied, &t.CreatedAt, &t.UpdatedAt); err != nil {
			internalError(w, r, err)
			return
		}
		if copied.Valid {
			t.CopiedFrom = &copied.Int64
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"frozen": !u.CanManagePrivate(), // 被撤销的高级用户：只能查看
		"tasks":  tasks,
	})
}

type privateTaskInput struct {
	Description string `json:"description"`
	Points      int64  `json:"points"`
}

func pointsLimit(u *User) int64 {
	if u.IsAdmin() {
		return maxPoints
	}
	return maxSuperPoints
}

func (s *Server) createPrivateTask(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in privateTaskInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := validateDescription(&in.Description); err != nil {
		fail(w, r, err)
		return
	}
	if err := validatePoints(in.Points, pointsLimit(u)); err != nil {
		fail(w, r, err)
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO tasks (owner_id, description, points) VALUES (?, ?, ?)`, u.ID, in.Description, in.Points)
	if err != nil {
		internalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) updatePrivateTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	var in privateTaskInput
	if !decodeJSON(w, r, &in) {
		return
	}
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		if err := validateDescription(&in.Description); err != nil {
			return err
		}
		var current int64
		err := tx.QueryRowContext(r.Context(),
			`SELECT points FROM tasks WHERE id = ? AND owner_id = ?`, id, u.ID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "任务不存在")
		}
		if err != nil {
			return err
		}
		// 从公共仓库复制来的任务可以保留超过 9 的原积分；一旦修改积分，高级用户必须改为 1-9
		if in.Points != current {
			if err := validatePoints(in.Points, pointsLimit(u)); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(r.Context(), `
			UPDATE tasks SET description = ?, points = ?,
			       updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE id = ?`, in.Description, in.Points, id)
		return err
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deletePrivateTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`DELETE FROM tasks WHERE id = ? AND owner_id = ?`, id, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 派发 ----------

type dispatchResult struct {
	UID   string `json:"uid"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// dispatchTask 把私有仓库中的任务派发给一个或多个普通用户，派发后直接生效，由派发者审核。
func (s *Server) dispatchTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	var in struct {
		UIDs []string `json:"uids"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	// 去重（用户 ID 不区分大小写）
	seen := map[string]bool{}
	var uids []string
	for _, uid := range in.UIDs {
		uid = strings.TrimSpace(uid)
		if key := strings.ToLower(uid); uid != "" && !seen[key] {
			seen[key] = true
			uids = append(uids, uid)
		}
	}
	if len(uids) == 0 || len(uids) > 100 {
		writeError(w, http.StatusBadRequest, "请选择 1-100 个派发对象")
		return
	}

	results := make([]dispatchResult, 0, len(uids))
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		var desc string
		var points int64
		err := tx.QueryRowContext(r.Context(),
			`SELECT description, points FROM tasks WHERE id = ? AND owner_id = ?`, id, u.ID).Scan(&desc, &points)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "任务不存在")
		}
		if err != nil {
			return err
		}
		for _, uid := range uids {
			msg, err := dispatchOne(r.Context(), tx, u, uid, newAssignment{
				TaskID: id, Public: false, ReviewerID: u.ID,
				Source: "dispatch", Description: desc, Points: points,
				// 高级用户派发的任务完成后获得专有积分，管理员派发的为通用积分
				ExclusiveSuperID: sql.NullInt64{Int64: u.ID, Valid: u.Role == RoleSuper},
			})
			if err != nil {
				return err
			}
			results = append(results, dispatchResult{UID: uid, OK: msg == "", Error: msg})
		}
		return nil
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// dispatchOne 检查派发权限并创建接取记录。返回的 msg 非空表示该用户派发失败的原因。
func dispatchOne(ctx context.Context, tx *sql.Tx, dispatcher *User, uid string, a newAssignment) (msg string, err error) {
	var targetID int64
	var role string
	var acceptAdmin, authorized, unredeemed bool
	err = tx.QueryRowContext(ctx, `
		SELECT u.id, u.role, u.accept_admin_dispatch,
		       EXISTS (SELECT 1 FROM bindings b WHERE b.member_id = u.id AND b.super_id = ? AND b.status != 'pending_bind'),
		       EXISTS (SELECT 1 FROM purchases p WHERE p.user_id = u.id AND p.super_id = ? AND p.redeemed_at IS NULL)
		FROM users u WHERE u.uid = ?`, dispatcher.ID, dispatcher.ID, uid).Scan(&targetID, &role, &acceptAdmin, &authorized, &unredeemed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "用户不存在", nil
	case err != nil:
		return "", err
	case role != RoleMember:
		return "只能派发给普通用户", nil
	case dispatcher.IsAdmin() && !acceptAdmin:
		return "该用户未开启“接受管理员派发任务”", nil
	case !dispatcher.IsAdmin() && !authorized:
		return "该用户未与你绑定", nil
	case !dispatcher.IsAdmin() && unredeemed:
		return "该用户有未核销的商品，核销前不能派发新任务", nil
	}
	a.UserID = targetID
	if _, err := insertAssignment(ctx, tx, a); err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			return ae.msg, nil
		}
		return "", err
	}
	return "", nil
}

type dispatchTarget struct {
	UID      string `json:"uid"`
	Nickname string `json:"nickname"`
	// 有未核销的、从当前高级用户商城购买的商品，核销前不能派发
	Unredeemed bool `json:"unredeemed"`
}

// listDispatchTargets 列出允许当前用户派发任务的普通用户。
func (s *Server) listDispatchTargets(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var rows *sql.Rows
	var err error
	if u.IsAdmin() {
		rows, err = s.db.QueryContext(r.Context(), `
			SELECT uid, nickname, 0 FROM users
			WHERE role = 'member' AND accept_admin_dispatch = 1 ORDER BY uid`)
	} else {
		rows, err = s.db.QueryContext(r.Context(), `
			SELECT u.uid, u.nickname,
			       EXISTS (SELECT 1 FROM purchases p WHERE p.user_id = u.id AND p.super_id = b.super_id AND p.redeemed_at IS NULL)
			FROM bindings b JOIN users u ON u.id = b.member_id
			WHERE b.super_id = ? AND b.status != 'pending_bind' AND u.role = 'member' ORDER BY u.uid`, u.ID)
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer rows.Close()
	targets := []dispatchTarget{}
	for rows.Next() {
		var t dispatchTarget
		if err := rows.Scan(&t.UID, &t.Nickname, &t.Unredeemed); err != nil {
			internalError(w, r, err)
			return
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, targets)
}
