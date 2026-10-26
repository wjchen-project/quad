package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const todoColumns = `id, user_id, title, notes, quadrant, status, progress, created_at, updated_at, completed_at`

func scanTodo(row rowScanner) (Todo, error) {
	var t Todo
	var created, updated int64
	var completed sql.NullInt64
	err := row.Scan(&t.ID, &t.UserID, &t.Title, &t.Notes, &t.Quadrant, &t.Status,
		&t.Progress, &created, &updated, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return Todo{}, ErrNotFound
	}
	if err != nil {
		return Todo{}, err
	}
	t.CreatedAt = time.UnixMilli(created).UTC()
	t.UpdatedAt = time.UnixMilli(updated).UTC()
	if completed.Valid {
		ts := time.UnixMilli(completed.Int64).UTC()
		t.CompletedAt = &ts
	}
	return t, nil
}

// CreateTodo 插入待办并回填 ID 与时间戳。
func (s *Store) CreateTodo(ctx context.Context, todo Todo) (Todo, error) {
	now := time.Now().UTC()
	var completed any
	if todo.CompletedAt != nil {
		completed = todo.CompletedAt.UTC().UnixMilli()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO todos(user_id, title, notes, quadrant, status, progress, created_at, updated_at, completed_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		todo.UserID, todo.Title, todo.Notes, todo.Quadrant, todo.Status, todo.Progress,
		now.UnixMilli(), now.UnixMilli(), completed)
	if err != nil {
		return Todo{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Todo{}, err
	}
	return s.GetTodo(ctx, todo.UserID, id)
}

// GetTodo 读取属于指定用户的待办。
func (s *Store) GetTodo(ctx context.Context, userID, id int64) (Todo, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+todoColumns+` FROM todos WHERE id = ? AND user_id = ?`, id, userID)
	return scanTodo(row)
}

// ListTodos 按过滤条件返回用户的待办，按创建时间倒序。
func (s *Store) ListTodos(ctx context.Context, userID int64, filter TodoFilter) ([]Todo, error) {
	var sb strings.Builder
	sb.WriteString(`SELECT ` + todoColumns + ` FROM todos WHERE user_id = ?`)
	args := []any{userID}
	if filter.Status != "" {
		sb.WriteString(` AND status = ?`)
		args = append(args, filter.Status)
	}
	if filter.Quadrant != "" {
		sb.WriteString(` AND quadrant = ?`)
		args = append(args, filter.Quadrant)
	}
	sb.WriteString(` ORDER BY created_at DESC, id DESC`)
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	} else if limit > 500 {
		limit = 500
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	sb.WriteString(` LIMIT ? OFFSET ?`)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	todos := make([]Todo, 0)
	for rows.Next() {
		t, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		todos = append(todos, t)
	}
	return todos, rows.Err()
}

// UpdateTodo 全量更新可变字段，仅允许更新自己的待办。
func (s *Store) UpdateTodo(ctx context.Context, todo Todo) error {
	var completed any
	if todo.CompletedAt != nil {
		completed = todo.CompletedAt.UTC().UnixMilli()
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE todos
		 SET title = ?, notes = ?, quadrant = ?, status = ?, progress = ?, updated_at = ?, completed_at = ?
		 WHERE id = ? AND user_id = ?`,
		todo.Title, todo.Notes, todo.Quadrant, todo.Status, todo.Progress,
		time.Now().UTC().UnixMilli(), completed, todo.ID, todo.UserID)
	if err != nil {
		return err
	}
	return s.ensureAffected(res)
}

// DeleteTodo 删除属于指定用户的待办。
func (s *Store) DeleteTodo(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM todos WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	return s.ensureAffected(res)
}
