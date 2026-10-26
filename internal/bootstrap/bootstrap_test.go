package bootstrap

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"todo_helper/internal/auth"
	"todo_helper/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "bootstrap.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestEnsureAdminCreatesFirstAdmin(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	res, err := EnsureAdmin(ctx, st, "sysadmin")
	if err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if !res.Created || res.Username != "sysadmin" || len(res.Password) != adminPasswordLen {
		t.Fatalf("result = %+v", res)
	}
	user, err := st.GetUserByUsername(ctx, "sysadmin")
	if err != nil {
		t.Fatal(err)
	}
	if user.Role != store.RoleAdmin {
		t.Fatalf("role = %s", user.Role)
	}
	_, hash, err := st.GetUserAuthByUsername(ctx, "sysadmin")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := auth.VerifyPassword(hash, res.Password); err != nil || !ok {
		t.Fatalf("初始密码未正确入库: ok=%v err=%v", ok, err)
	}

	again, err := EnsureAdmin(ctx, st, "sysadmin")
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Password != "" {
		t.Fatalf("重复初始化不应再创建账号: %+v", again)
	}
}

func TestEnsureAdminPromotesExistingUser(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	hash, err := auth.HashPassword("user-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, "boss", hash, store.RoleUser); err != nil {
		t.Fatal(err)
	}
	res, err := EnsureAdmin(ctx, st, "boss")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || !res.Promoted || res.Password != "" {
		t.Fatalf("result = %+v", res)
	}
	user, err := st.GetUserByUsername(ctx, "boss")
	if err != nil {
		t.Fatal(err)
	}
	if user.Role != store.RoleAdmin {
		t.Fatalf("role = %s", user.Role)
	}
}

func TestResetAdminPassword(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	first, err := EnsureAdmin(ctx, st, "sysadmin")
	if err != nil {
		t.Fatal(err)
	}
	user, err := st.GetUserByUsername(ctx, "sysadmin")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateRefreshToken(ctx, user.ID, "token-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	pw, err := ResetAdminPassword(ctx, st, "sysadmin")
	if err != nil {
		t.Fatalf("ResetAdminPassword: %v", err)
	}
	if pw == first.Password {
		t.Fatal("重置后的密码不应与初始密码相同")
	}
	_, hash, err := st.GetUserAuthByUsername(ctx, "sysadmin")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := auth.VerifyPassword(hash, pw); err != nil || !ok {
		t.Fatalf("新密码校验失败: ok=%v err=%v", ok, err)
	}
	rt, err := st.GetRefreshToken(ctx, "token-hash")
	if err != nil {
		t.Fatal(err)
	}
	if rt.RevokedAt == nil {
		t.Fatal("重置密码后应撤销刷新令牌")
	}
	if _, err := ResetAdminPassword(ctx, st, "ghost"); err == nil {
		t.Fatal("不存在的用户应报错")
	}
}

func TestEnsureJWTSecretStable(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	first, err := EnsureJWTSecret(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 {
		t.Fatalf("secret length = %d", len(first))
	}
	second, err := EnsureJWTSecret(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("同一数据库的 JWT 密钥应保持稳定")
	}
	other := newStore(t)
	third, err := EnsureJWTSecret(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, third) {
		t.Fatal("不同数据库不应共享密钥")
	}
}
