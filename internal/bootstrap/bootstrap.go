// Package bootstrap 负责首次启动时的数据初始化（管理员账号、JWT 密钥）。
//
// 作者: wjchen
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"quad/internal/auth"
	"quad/internal/store"
)

const (
	metaJWTSecret    = "jwt_secret"
	adminPasswordLen = 20
)

// AdminResult 描述管理员初始化结果。
type AdminResult struct {
	Username string
	Password string // 仅在新建管理员或重置密码时非空
	Created  bool
	Promoted bool
}

// EnsureAdmin 确保至少存在一个管理员：
//   - 没有任何用户时，按配置的用户名创建管理员，返回随机密码；
//   - 有用户但没有管理员时，把配置用户名对应的用户提升为管理员。
func EnsureAdmin(ctx context.Context, st *store.Store, username string) (AdminResult, error) {
	res := AdminResult{Username: username}
	count, err := st.CountUsers(ctx)
	if err != nil {
		return res, err
	}
	if count == 0 {
		pw, err := createAdmin(ctx, st, username)
		if err != nil {
			return res, err
		}
		res.Password = pw
		res.Created = true
		return res, nil
	}
	admins, err := st.CountAdmins(ctx)
	if err != nil {
		return res, err
	}
	if admins > 0 {
		return res, nil
	}
	u, err := st.GetUserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		pw, err := createAdmin(ctx, st, username)
		if err != nil {
			return res, err
		}
		res.Password = pw
		res.Created = true
		return res, nil
	}
	if err != nil {
		return res, err
	}
	if err := st.SetUserRole(ctx, u.ID, store.RoleAdmin); err != nil {
		return res, err
	}
	res.Promoted = true
	return res, nil
}

// ResetAdminPassword 为指定用户重置密码并撤销其全部刷新令牌。
func ResetAdminPassword(ctx context.Context, st *store.Store, username string) (string, error) {
	u, err := st.GetUserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("bootstrap: 用户 %q 不存在", username)
	}
	if err != nil {
		return "", err
	}
	pw, err := auth.GeneratePassword(adminPasswordLen)
	if err != nil {
		return "", err
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return "", err
	}
	if err := st.SetUserPassword(ctx, u.ID, hash); err != nil {
		return "", err
	}
	if u.Role != store.RoleAdmin {
		if err := st.SetUserRole(ctx, u.ID, store.RoleAdmin); err != nil {
			return "", err
		}
	}
	if err := st.RevokeUserRefreshTokens(ctx, u.ID); err != nil {
		return "", err
	}
	return pw, nil
}

// EnsureJWTSecret 读取或生成 JWT 密钥；密钥只存数据库，不写配置文件。
func EnsureJWTSecret(ctx context.Context, st *store.Store) ([]byte, error) {
	encoded, err := st.GetMeta(ctx, metaJWTSecret)
	if err == nil {
		secret, decErr := base64.RawStdEncoding.DecodeString(encoded)
		if decErr == nil && len(secret) >= 16 {
			return secret, nil
		}
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("bootstrap: 生成 JWT 密钥失败: %w", err)
	}
	if err := st.SetMeta(ctx, metaJWTSecret, base64.RawStdEncoding.EncodeToString(secret)); err != nil {
		return nil, err
	}
	return secret, nil
}

func createAdmin(ctx context.Context, st *store.Store, username string) (string, error) {
	pw, err := auth.GeneratePassword(adminPasswordLen)
	if err != nil {
		return "", err
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return "", err
	}
	if _, err := st.CreateUser(ctx, username, hash, store.RoleAdmin); err != nil {
		return "", err
	}
	return pw, nil
}
