package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CreateRefreshToken 保存刷新令牌摘要。
func (s *Store) CreateRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens(user_id, token_hash, expires_at, created_at) VALUES(?, ?, ?, ?)`,
		userID, tokenHash, expiresAt.UTC().UnixMilli(), time.Now().UTC().UnixMilli())
	return err
}

// GetRefreshToken 按摘要查询刷新令牌记录（包含已撤销 / 已过期的记录，由调用方判断）。
func (s *Store) GetRefreshToken(ctx context.Context, tokenHash string) (RefreshToken, error) {
	var rt RefreshToken
	var expires, created int64
	var revoked sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, revoked_at, created_at FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &expires, &revoked, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return RefreshToken{}, ErrNotFound
	}
	if err != nil {
		return RefreshToken{}, err
	}
	rt.ExpiresAt = time.UnixMilli(expires).UTC()
	rt.CreatedAt = time.UnixMilli(created).UTC()
	if revoked.Valid {
		ts := time.UnixMilli(revoked.Int64).UTC()
		rt.RevokedAt = &ts
	}
	return rt, nil
}

// RevokeRefreshToken 撤销单个刷新令牌。
func (s *Store) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`,
		time.Now().UTC().UnixMilli(), tokenHash)
	if err != nil {
		return err
	}
	return s.ensureAffected(res)
}

// RevokeUserRefreshTokens 撤销用户全部刷新令牌，用于改密或重置密码。
func (s *Store) RevokeUserRefreshTokens(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		time.Now().UTC().UnixMilli(), userID)
	return err
}

// DeleteExpiredRefreshTokens 清理过期或已撤销的刷新令牌。
func (s *Store) DeleteExpiredRefreshTokens(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at < ? OR revoked_at IS NOT NULL`,
		before.UTC().UnixMilli())
	return err
}
