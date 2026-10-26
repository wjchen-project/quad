package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const userColumns = `id, username, role, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (User, error) {
	var u User
	var created, updated int64
	if err := row.Scan(&u.ID, &u.Username, &u.Role, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	u.CreatedAt = time.UnixMilli(created).UTC()
	u.UpdatedAt = time.UnixMilli(updated).UTC()
	return u, nil
}

// CreateUser 创建用户，用户名重复时返回 ErrConflict。
func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string) (User, error) {
	if _, ok := NormalizeRole(role); !ok {
		return User{}, errors.New("store: 非法用户角色")
	}
	now := time.Now().UTC().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(username, password_hash, role, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
		username, passwordHash, role, now, now)
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.GetUserByID(ctx, id)
}

// GetUserByID 按 ID 查询用户。
func (s *Store) GetUserByID(ctx context.Context, id int64) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetUserByUsername 按用户名查询用户（大小写不敏感）。
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, username)
	return scanUser(row)
}

// GetUserAuthByUsername 返回用户及其密码哈希，供登录校验使用。
func (s *Store) GetUserAuthByUsername(ctx context.Context, username string) (User, string, error) {
	var u User
	var hash string
	var created, updated int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, role, password_hash, created_at, updated_at FROM users WHERE username = ?`,
		username).Scan(&u.ID, &u.Username, &u.Role, &hash, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", err
	}
	u.CreatedAt = time.UnixMilli(created).UTC()
	u.UpdatedAt = time.UnixMilli(updated).UTC()
	return u, hash, nil
}

// ListUsers 按创建顺序返回全部用户。
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// SetUserPassword 覆盖用户密码哈希并刷新 updated_at。
func (s *Store) SetUserPassword(ctx context.Context, id int64, passwordHash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, time.Now().UTC().UnixMilli(), id)
	if err != nil {
		return err
	}
	return s.ensureAffected(res)
}

// SetUserRole 修改用户角色。
func (s *Store) SetUserRole(ctx context.Context, id int64, role string) error {
	if _, ok := NormalizeRole(role); !ok {
		return errors.New("store: 非法用户角色")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET role = ?, updated_at = ? WHERE id = ?`,
		role, time.Now().UTC().UnixMilli(), id)
	if err != nil {
		return err
	}
	return s.ensureAffected(res)
}

// DeleteUser 删除用户，其待办与刷新令牌随之级联删除。
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return s.ensureAffected(res)
}

// CountAdmins 返回管理员数量，用于阻止删除最后一个管理员。
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = ?`, RoleAdmin).Scan(&n)
	return n, err
}

// CountUsers 返回用户总数。
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}
