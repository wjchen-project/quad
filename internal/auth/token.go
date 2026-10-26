package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidToken 表示令牌缺失、被篡改或已过期。
var ErrInvalidToken = errors.New("auth: 令牌无效或已过期")

// Claims 是访问令牌中携带的声明。
type Claims struct {
	Subject   int64
	Username  string
	Role      string
	IssuedAt  time.Time
	ExpiresAt time.Time
	ID        string
}

type tokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type tokenPayload struct {
	Sub      int64  `json:"sub"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
	Jti      string `json:"jti"`
}

// Signer 使用 HS256 签发和校验访问令牌。
type Signer struct {
	secret []byte
	now    func() time.Time
}

// NewSigner 创建签名器；secret 至少 16 字节。
func NewSigner(secret []byte) (*Signer, error) {
	if len(secret) < 16 {
		return nil, errors.New("auth: JWT 密钥长度不足 16 字节")
	}
	return &Signer{secret: append([]byte(nil), secret...), now: time.Now}, nil
}

// SetClock 仅用于测试，替换时间源。
func (s *Signer) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// Sign 签发访问令牌，claims.ExpiresAt 必须晚于当前时间。
func (s *Signer) Sign(claims Claims) (string, error) {
	now := s.now().UTC()
	if !claims.ExpiresAt.After(now) {
		return "", errors.New("auth: 令牌过期时间必须晚于当前时间")
	}
	jti := claims.ID
	if jti == "" {
		buf := make([]byte, 8)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("auth: 生成令牌 ID 失败: %w", err)
		}
		jti = base64.RawURLEncoding.EncodeToString(buf)
	}
	header, err := json.Marshal(tokenHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(tokenPayload{
		Sub:      claims.Subject,
		Username: claims.Username,
		Role:     claims.Role,
		Iat:      now.Unix(),
		Exp:      claims.ExpiresAt.UTC().Unix(),
		Jti:      jti,
	})
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	return signingInput + "." + enc.EncodeToString(s.sign(signingInput)), nil
}

// Verify 校验令牌签名与有效期，并返回声明。
func (s *Signer) Verify(token string) (Claims, error) {
	var claims Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, ErrInvalidToken
	}
	enc := base64.RawURLEncoding
	headerRaw, err := enc.DecodeString(parts[0])
	if err != nil {
		return claims, ErrInvalidToken
	}
	var header tokenHeader
	if err := json.Unmarshal(headerRaw, &header); err != nil || header.Alg != "HS256" {
		return claims, ErrInvalidToken
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return claims, ErrInvalidToken
	}
	if !hmac.Equal(sig, s.sign(parts[0]+"."+parts[1])) {
		return claims, ErrInvalidToken
	}
	payloadRaw, err := enc.DecodeString(parts[1])
	if err != nil {
		return claims, ErrInvalidToken
	}
	var payload tokenPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return claims, ErrInvalidToken
	}
	now := s.now().UTC()
	if payload.Sub <= 0 || payload.Exp == 0 || !now.Before(time.Unix(payload.Exp, 0)) {
		return claims, ErrInvalidToken
	}
	claims = Claims{
		Subject:   payload.Sub,
		Username:  payload.Username,
		Role:      payload.Role,
		IssuedAt:  time.Unix(payload.Iat, 0).UTC(),
		ExpiresAt: time.Unix(payload.Exp, 0).UTC(),
		ID:        payload.Jti,
	}
	return claims, nil
}

func (s *Signer) sign(input string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(input))
	return mac.Sum(nil)
}

// GenerateRefreshToken 生成随机刷新令牌明文。
func GenerateRefreshToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: 生成刷新令牌失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashRefreshToken 返回刷新令牌的 SHA-256 十六进制摘要，数据库只保存摘要。
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:])
}
