package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("s3cret-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "pbkdf2_sha256$210000$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	ok, err := VerifyPassword(hash, "s3cret-password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("正确密码应校验通过")
	}
	ok, err = VerifyPassword(hash, "wrong-password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Fatal("错误密码不应校验通过")
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	first, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("相同密码的哈希应因随机盐而不同")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	cases := []string{
		"",
		"plain-text",
		"pbkdf2_sha256$not-a-number$c2FsdA$aGFzaA",
		"pbkdf2_sha256$1000$!!!$aGFzaA",
		"pbkdf2_sha256$210000$c2FsdA$!!!",
		"md5$1$c2FsdA$aGFzaA",
	}
	for _, hash := range cases {
		if _, err := VerifyPassword(hash, "whatever"); err == nil {
			t.Fatalf("应拒绝非法哈希 %q", hash)
		}
	}
}

func TestGeneratePassword(t *testing.T) {
	pw, err := GeneratePassword(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != 20 {
		t.Fatalf("长度 = %d, want 20", len(pw))
	}
	for _, r := range pw {
		if !strings.ContainsRune(passwordAlphabet, r) {
			t.Fatalf("密码包含意外字符 %q", r)
		}
	}
	other, err := GeneratePassword(20)
	if err != nil {
		t.Fatal(err)
	}
	if pw == other {
		t.Fatal("两次生成的密码不应相同")
	}
	if _, err := GeneratePassword(0); err == nil {
		t.Fatal("长度为 0 应报错")
	}
}
