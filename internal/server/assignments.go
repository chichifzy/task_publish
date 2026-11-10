package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

type newAssignment struct {
	TaskID      int64
	Public      bool
	UserID      int64
	ReviewerID  int64
	Source      string
	Description string
	Points      int64
}

// insertAssignment 创建接取记录并保存任务快照。
// 唯一索引保证同一用户在完成前不能以任何方式重复接取同一任务。
func insertAssignment(ctx context.Context, tx *sql.Tx, a newAssignment) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO assignments (task_id, task_public, user_id, reviewer_id, source, description, points, seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.TaskID, a.Public, a.UserID, a.ReviewerID, a.Source, a.Description, a.Points,
		a.Source != "dispatch") // 派发的任务标记为未查看，用于“我的任务”红点
	if isUniqueViolation(err) {
		return 0, errStatus(http.StatusConflict, "正在进行此任务，完成前不能重复接取")
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// adminID 返回管理员的内部编号。公共任务由管理员审核。
func adminID(ctx context.Context, tx *sql.Tx) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE role = 'admin'`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errStatus(http.StatusConflict, "系统尚未创建管理员")
	}
	return id, err
}

type userRef struct {
	UID      string `json:"uid"`
	Nickname string `json:"nickname"`
}

type Assignment struct {
	ID          int64   `json:"id"`
	TaskID      int64   `json:"taskId"`
	TaskPublic  bool    `json:"taskPublic"`
	Source      string  `json:"source"`
	Description string  `json:"description"`
	Points      int64   `json:"points"`
	Status      string  `json:"status"`
	Submission  string  `json:"submission"`
	ReviewNote  string  `json:"reviewNote"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	User        userRef `json:"user"`
	Reviewer    userRef `json:"reviewer"`
}

const assignmentSelect = `
	SELECT a.id, a.task_id, a.task_public, a.source, a.description, a.points, a.status,
	       a.submission, a.review_note, a.created_at, a.updated_at,
	       u.uid, u.nickname, rv.uid, rv.nickname
	FROM assignments a
	JOIN users u ON u.id = a.user_id
	JOIN users rv ON rv.id = a.reviewer_id`

func (s *Server) queryAssignments(w http.ResponseWriter, r *http.Request, where string, args ...any) {
	rows, err := s.db.QueryContext(r.Context(), assignmentSelect+" "+where, args...)
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer rows.Close()
	list := []Assignment{}
	for rows.Next() {
		var a Assignment
		if err := rows.Scan(&a.ID, &a.TaskID, &a.TaskPublic, &a.Source, &a.Description, &a.Points,
			&a.Status, &a.Submission, &a.ReviewNote, &a.CreatedAt, &a.UpdatedAt,
			&a.User.UID, &a.User.Nickname, &a.Reviewer.UID, &a.Reviewer.Nickname); err != nil {
			internalError(w, r, err)
			return
		}
		list = append(list, a)
	}
	if err := rows.Err(); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// listMyAssignments 当前用户的接取记录，未完成的排在前面。查看后清除新任务红点。
func (s *Server) listMyAssignments(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE assignments SET seen = 1 WHERE user_id = ? AND seen = 0`, u.ID); err != nil {
		internalError(w, r, err)
		return
	}
	s.queryAssignments(w, r, `
		WHERE a.user_id = ? AND a.user_hidden = 0
		ORDER BY CASE a.status WHEN 'in_progress' THEN 0 WHEN 'submitted' THEN 1 ELSE 2 END, a.updated_at DESC`,
		u.ID)
}

// listRecords 任务记录。普通用户：自己已完成或被取消的任务；高级用户和管理员：自己派发的任务。
func (s *Server) listRecords(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.Role == RoleMember {
		s.queryAssignments(w, r, `
			WHERE a.user_id = ? AND a.user_hidden = 0 AND a.status IN ('completed', 'cancelled')
			ORDER BY a.updated_at DESC`, u.ID)
		return
	}
	s.queryAssignments(w, r, `
		WHERE a.reviewer_id = ? AND a.source = 'dispatch' AND a.reviewer_hidden = 0
		ORDER BY CASE WHEN a.status IN `+activeStatuses+` THEN 0 ELSE 1 END, a.updated_at DESC`, u.ID)
}

// cancelAssignment 派发者取消尚未完成的派发任务，不扣积分。
func (s *Server) cancelAssignment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE assignments SET status = 'cancelled', seen = 1,
		       updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ? AND reviewer_id = ? AND source = 'dispatch' AND status IN `+activeStatuses,
		id, u.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists bool
		if err := s.db.QueryRowContext(r.Context(), `
			SELECT EXISTS (SELECT 1 FROM assignments WHERE id = ? AND reviewer_id = ? AND source = 'dispatch')`,
			id, u.ID).Scan(&exists); err != nil {
			internalError(w, r, err)
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "任务记录不存在")
		} else {
			writeError(w, http.StatusConflict, "只有未完成的任务可以取消")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteRecord 从自己的任务记录中删除已完成或已取消的任务。
// 执行人和派发者各自删除，互不影响；积分流水仍引用该记录，所以只做隐藏。
func (s *Server) deleteRecord(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	q := `UPDATE assignments SET user_hidden = 1 WHERE id = ? AND user_id = ? AND status IN ('completed', 'cancelled')`
	if u.Role != RoleMember {
		q = `UPDATE assignments SET reviewer_hidden = 1
			WHERE id = ? AND reviewer_id = ? AND source = 'dispatch' AND status IN ('completed', 'cancelled')`
	}
	res, err := s.db.ExecContext(r.Context(), q, id, u.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusConflict, "只有已完成或已取消的任务记录可以删除")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listReviews 等待当前用户审核的提交。
func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	s.queryAssignments(w, r, `
		WHERE a.reviewer_id = ? AND a.status = 'submitted'
		ORDER BY a.updated_at`, currentUser(r).ID)
}

func (s *Server) submitAssignment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Submission string `json:"submission"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Submission = strings.TrimSpace(in.Submission)
	if n := utf8.RuneCountInString(in.Submission); n < 1 || n > maxDescriptionLen {
		writeError(w, http.StatusBadRequest, "完成情况需为 1-5000 个字符")
		return
	}
	u := currentUser(r)
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE assignments SET status = 'submitted', submission = ?,
		       updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ? AND user_id = ? AND status = 'in_progress'`, in.Submission, id, u.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists bool
		if err := s.db.QueryRowContext(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM assignments WHERE id = ? AND user_id = ?)`, id, u.ID).Scan(&exists); err != nil {
			internalError(w, r, err)
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "任务记录不存在")
		} else {
			writeError(w, http.StatusConflict, "只有进行中的任务可以提交")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// abandonAssignment 放弃任务，扣除任务积分的 50%（向下取整）。积分为负数时不能放弃。
func (s *Server) abandonAssignment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	var penalty, balance int64
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		var points int64
		var status string
		err := tx.QueryRowContext(r.Context(),
			`SELECT points, status FROM assignments WHERE id = ? AND user_id = ?`, id, u.ID).Scan(&points, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "任务记录不存在")
		}
		if err != nil {
			return err
		}
		if status != "in_progress" && status != "submitted" {
			return errStatus(http.StatusConflict, "只有未完成的任务可以放弃")
		}
		if err := tx.QueryRowContext(r.Context(),
			`SELECT points FROM users WHERE id = ?`, u.ID).Scan(&balance); err != nil {
			return err
		}
		if balance < 0 {
			return errStatus(http.StatusConflict, "积分为负数时不能放弃任务")
		}
		penalty = points / 2 // 积分为正整数，整数除法即向下取整
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE assignments SET status = 'abandoned',
			       updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE id = ?`, id); err != nil {
			return err
		}
		if penalty == 0 {
			return nil
		}
		balance -= penalty
		return changePoints(r.Context(), tx, u.ID, -penalty, "abandon", id)
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"penalty": penalty, "points": balance})
}

// reviewAssignment 审核人通过或退回提交。通过后发放积分；退回后任务回到进行中。
func (s *Server) reviewAssignment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > 1000 {
		writeError(w, http.StatusBadRequest, "审核意见不能超过 1000 个字符")
		return
	}
	if !in.Approve && in.Note == "" {
		writeError(w, http.StatusBadRequest, "请填写不通过的原因")
		return
	}
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		var userID, points int64
		var status string
		err := tx.QueryRowContext(r.Context(),
			`SELECT user_id, points, status FROM assignments WHERE id = ? AND reviewer_id = ?`,
			id, currentUser(r).ID).Scan(&userID, &points, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "任务记录不存在")
		}
		if err != nil {
			return err
		}
		if status != "submitted" {
			return errStatus(http.StatusConflict, "只有已提交的任务可以审核")
		}
		newStatus := "in_progress"
		if in.Approve {
			newStatus = "completed"
		}
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE assignments SET status = ?, review_note = ?,
			       updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE id = ?`, newStatus, in.Note, id); err != nil {
			return err
		}
		if !in.Approve {
			return nil
		}
		return changePoints(r.Context(), tx, userID, points, "complete", id)
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// changePoints 修改用户积分并写一条积分流水。
func changePoints(ctx context.Context, tx *sql.Tx, userID, delta int64, reason string, assignmentID int64) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET points = points + ? WHERE id = ?`, delta, userID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO points_log (user_id, delta, reason, assignment_id) VALUES (?, ?, ?, ?)`,
		userID, delta, reason, assignmentID)
	return err
}
