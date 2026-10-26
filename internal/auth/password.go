// Package auth 提供密码哈希、随机密码、HMAC-SHA256 访问令牌与刷新令牌工具。
//
// 作者: wjchen
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// 去掉容易混淆的 0/O/1/l/I，方便用户抄写随机生成的初始密码。
	passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	pbkdf2Iterations = 210_000
	pbkdf2KeyLength  = 32
	pbkdf2SaltLength = 16
)

// ErrInvalidHash 表示存储的密码哈希格式无法解析。
var ErrInvalidHash = errors.New("auth: 密码哈希格式无效")

// GeneratePassword 生成指定长度的随机密码。
func GeneratePassword(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("auth: 密码长度必须为正数")
	}
	out := make([]byte, length)
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: 生成随机密码失败: %w", err)
	}
	for i, b := range buf {
		out[i] = passwordAlphabet[int(b)%len(passwordAlphabet)]
	}
	return string(out), nil
}

// HashPassword 使用 PBKDF2-HMAC-SHA256 派生出可直接入库的密码哈希。
// 格式: pbkdf2_sha256$<iterations>$<salt-base64>$<hash-base64>
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("auth: 密码不能为空")
	}
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: 生成盐失败: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLength)
	if err != nil {
		return "", fmt.Errorf("auth: 派生密码哈希失败: %w", err)
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s",
		pbkdf2Iterations,
		enc.EncodeToString(salt),
		enc.EncodeToString(key),
	), nil
}

// VerifyPassword 校验明文密码与存储的哈希是否匹配。
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false, ErrInvalidHash
	}
	iterations, err := strconv.Atoi(parts[1])
	// 限制迭代次数范围，避免被恶意哈希拖慢校验。
	if err != nil || iterations < 1_000 || iterations > 10_000_000 {
		return false, ErrInvalidHash
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false, ErrInvalidHash
	}
	want, err := enc.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false, ErrInvalidHash
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false, fmt.Errorf("auth: 派生密码哈希失败: %w", err)
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
