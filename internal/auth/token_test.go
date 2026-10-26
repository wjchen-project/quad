package auth

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testSigner(t *testing.T) (*Signer, *time.Time) {
	t.Helper()
	signer, err := NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	signer.SetClock(func() time.Time { return now })
	return signer, &now
}

func TestSignAndVerify(t *testing.T) {
	signer, now := testSigner(t)
	token, err := signer.Sign(Claims{
		Subject:   7,
		Username:  "alice",
		Role:      "admin",
		ExpiresAt: now.Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != 7 || claims.Username != "alice" || claims.Role != "admin" {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.ID == "" {
		t.Fatal("应自动生成 jti")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	signer, now := testSigner(t)
	token, err := signer.Sign(Claims{Subject: 1, Username: "a", Role: "user", ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	if _, err := signer.Verify(token); err == nil {
		t.Fatal("过期令牌应校验失败")
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	signer, now := testSigner(t)
	token, err := signer.Sign(Claims{Subject: 1, Username: "a", Role: "user", ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	// 篡改 payload（提升角色），签名不再匹配。
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	tampered := strings.Replace(string(payload), `"role":"user"`, `"role":"admin"`, 1)
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(tampered))
	if _, err := signer.Verify(strings.Join(parts, ".")); err == nil {
		t.Fatal("被篡改的令牌应校验失败")
	}
}

func TestVerifyRejectsAlgNone(t *testing.T) {
	signer, now := testSigner(t)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":1,"exp":%d}`, now.Add(time.Hour).Unix())))
	token := header + "." + payload + "."
	if _, err := signer.Verify(token); err == nil {
		t.Fatal("alg=none 应被拒绝")
	}
}

func TestSignRequiresFutureExpiry(t *testing.T) {
	signer, now := testSigner(t)
	if _, err := signer.Sign(Claims{Subject: 1, ExpiresAt: *now}); err == nil {
		t.Fatal("过期时间不晚于当前时间应报错")
	}
}

func TestNewSignerRejectsShortSecret(t *testing.T) {
	if _, err := NewSigner([]byte("short")); err == nil {
		t.Fatal("过短密钥应报错")
	}
}

func TestRefreshTokenHashing(t *testing.T) {
	token, err := GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 32 {
		t.Fatalf("刷新令牌过短: %d", len(token))
	}
	hash := HashRefreshToken(token)
	if len(hash) != 64 {
		t.Fatalf("摘要长度 = %d, want 64", len(hash))
	}
	if hash != HashRefreshToken(token) {
		t.Fatal("同一令牌的摘要应稳定")
	}
	other, err := GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if HashRefreshToken(other) == hash {
		t.Fatal("不同令牌的摘要不应相同")
	}
}
