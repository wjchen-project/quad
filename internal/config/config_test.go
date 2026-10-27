package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{EnvConfig, EnvAddr, EnvDBPath, EnvAdminUsername, EnvAccessTTL, EnvRefreshTTL} {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	clearEnv(t)

	cfg, path, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := filepath.Join(home, "server.json"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if cfg.Addr != DefaultAddr || cfg.AdminUsername != DefaultAdminUsername {
		t.Fatalf("cfg = %+v", cfg)
	}
	if want := filepath.Join(home, "quad.db"); cfg.DBPath != want {
		t.Fatalf("db = %q, want %q", cfg.DBPath, want)
	}
	if cfg.AccessTTL() != 30*time.Minute || cfg.RefreshTTL() != 720*time.Hour {
		t.Fatalf("ttl = %v / %v", cfg.AccessTTL(), cfg.RefreshTTL())
	}
}

func TestLoadFileThenEnvOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	clearEnv(t)

	path := filepath.Join(home, "custom.json")
	fileCfg := Config{
		Addr:               "127.0.0.1:9000",
		DBPath:             "~/data/todo.db",
		AdminUsername:      "root",
		AccessTokenMinutes: 10,
		RefreshTokenHours:  5,
	}
	if err := Save(path, fileCfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvAddr, "0.0.0.0:9999")
	t.Setenv(EnvAccessTTL, "15")

	cfg, gotPath, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if gotPath != path {
		t.Fatalf("path = %q", gotPath)
	}
	if cfg.Addr != "0.0.0.0:9999" {
		t.Fatalf("addr = %q, env 应覆盖文件", cfg.Addr)
	}
	if cfg.AccessTokenMinutes != 15 {
		t.Fatalf("access ttl = %d", cfg.AccessTokenMinutes)
	}
	if cfg.AdminUsername != "root" || cfg.RefreshTokenHours != 5 {
		t.Fatalf("文件值未生效: %+v", cfg)
	}
	homeDir, _ := os.UserHomeDir()
	if want := filepath.Join(homeDir, "data", "todo.db"); cfg.DBPath != want {
		t.Fatalf("db = %q, want %q", cfg.DBPath, want)
	}
}

func TestEnsureFileAndInvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	clearEnv(t)

	path := filepath.Join(home, "server.json")
	if err := EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("写入的配置不是合法 JSON: %v", err)
	}
	if saved.Addr != DefaultAddr {
		t.Fatalf("addr = %q", saved.Addr)
	}
	// 已存在时不应覆盖。
	if err := EnsureFile(path); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestFinalizeValidation(t *testing.T) {
	valid := Config{Addr: DefaultAddr, DBPath: "db.sqlite", AdminUsername: "admin", AccessTokenMinutes: 1, RefreshTokenHours: 1}
	if err := valid.Finalize(); err != nil {
		t.Fatalf("有效配置: %v", err)
	}

	cases := []Config{
		{Addr: "", DBPath: "x", AdminUsername: "a", AccessTokenMinutes: 1, RefreshTokenHours: 1},
		{Addr: "x", DBPath: "", AdminUsername: "a", AccessTokenMinutes: 1, RefreshTokenHours: 1},
		{Addr: "x", DBPath: "x", AdminUsername: "", AccessTokenMinutes: 1, RefreshTokenHours: 1},
		{Addr: "x", DBPath: "x", AdminUsername: "a", AccessTokenMinutes: 0, RefreshTokenHours: 1},
		{Addr: "x", DBPath: "x", AdminUsername: "a", AccessTokenMinutes: 1, RefreshTokenHours: 0},
	}
	for i, cfg := range cases {
		if err := cfg.Finalize(); err == nil {
			t.Fatalf("case %d 应校验失败: %+v", i, cfg)
		}
	}
}

func TestInvalidEnvNumber(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	clearEnv(t)
	t.Setenv(EnvAccessTTL, "abc")
	if _, _, err := Load(""); err == nil {
		t.Fatal("非法环境变量应报错")
	}
}

func TestSessionPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	path, err := SessionPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "session.json"); path != want {
		t.Fatalf("session path = %q, want %q", path, want)
	}
	dir, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != home {
		t.Fatalf("data dir = %q", dir)
	}
}

func TestResolvePathPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	clearEnv(t)
	t.Setenv(EnvConfig, "~/env.json")
	got, err := ResolvePath("")
	if err != nil {
		t.Fatal(err)
	}
	homeDir, _ := os.UserHomeDir()
	if want := filepath.Join(homeDir, "env.json"); got != want {
		t.Fatalf("env path = %q, want %q", got, want)
	}
	got, err = ResolvePath("flag.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != "flag.json" {
		t.Fatalf("flag path = %q", got)
	}
}
