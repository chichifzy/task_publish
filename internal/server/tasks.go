package server

import (
	"database/sql"
	"net/http"
	"strings"
	"unicode/utf8"
)

type Task struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	Claimed     bool    `json:"claimed"`
	ClaimedByMe bool    `json:"claimedByMe"`
	ClaimedBy   *string `json:"claimedBy,omitempty"` // 只对管理员返回
}

// listTasks：管理员看到全部任务和接取人；普通用户看到开放中的任务和自己接取的任务。
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT t.id, t.title, t.description, t.status, t.created_at, t.updated_at,
		       c.user_id, cu.username
		FROM tasks t
		LEFT JOIN task_claims c ON c.task_id = t.id
		LEFT JOIN users cu ON cu.id = c.user_id
		WHERE ? OR t.status = 'open' OR c.user_id = ?
		ORDER BY t.id DESC`, u.IsAdmin(), u.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer rows.Close()

	tasks := []Task{}
	for rows.Next() {
		var t Task
		var claimerID sql.NullInt64
		var claimerName sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Status,
			&t.CreatedAt, &t.UpdatedAt, &claimerID, &claimerName); err != nil {
			internalError(w, r, err)
			return
		}
		t.Claimed = claimerID.Valid
		t.ClaimedByMe = claimerID.Valid && claimerID.Int64 == u.ID
		if u.IsAdmin() && claimerName.Valid {
			t.ClaimedBy = &claimerName.String
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// claimTask 用一条带条件的 INSERT 完成接取，并发抢同一任务时只有一人成功。
func (s *Server) claimTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		INSERT INTO task_claims (task_id, user_id)
		SELECT id, ? FROM tasks WHERE id = ? AND status = 'open'
		ON CONFLICT (task_id) DO NOTHING`, currentUser(r).ID, id)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists bool
		if err := s.db.QueryRowContext(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM tasks WHERE id = ?)`, id).Scan(&exists); err != nil {
			internalError(w, r, err)
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "任务不存在")
		} else {
			writeError(w, http.StatusConflict, "任务已被接取或已关闭")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type taskInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

func (in *taskInput) validate() string {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 {
		return "标题需为 1-200 个字符"
	}
	if utf8.RuneCountInString(in.Description) > 5000 {
		return "描述不能超过 5000 个字符"
	}
	if in.Status == "" {
		in.Status = "open"
	}
	if in.Status != "open" && in.Status != "closed" {
		return "状态只能是 open 或 closed"
	}
	return ""
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in taskInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if msg := in.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO tasks (title, description, status, created_by) VALUES (?, ?, ?, ?)`,
		in.Title, in.Description, in.Status, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in taskInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if msg := in.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE tasks SET title = ?, description = ?, status = ?,
		       updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, in.Title, in.Description, in.Status, id)
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

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM tasks WHERE id = ?`, id)
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
