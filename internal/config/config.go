// Package config 负责服务端配置的加载、环境变量覆盖与默认路径。
//
// 作者: wjchen
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 环境变量名，命令行参数优先级高于环境变量。
const (
	EnvHome          = "TODO_HELPER_HOME"
	EnvConfig        = "TODO_HELPER_CONFIG"
	EnvAddr          = "TODO_HELPER_ADDR"
	EnvDBPath        = "TODO_HELPER_DB"
	EnvAdminUsername = "TODO_HELPER_ADMIN_USERNAME"
	EnvAccessTTL     = "TODO_HELPER_ACCESS_TOKEN_MINUTES"
	EnvRefreshTTL    = "TODO_HELPER_REFRESH_TOKEN_HOURS"
)

// 默认值。
const (
	DefaultAddr          = "127.0.0.1:8787"
	DefaultAdminUsername = "sysadmin"
	DefaultAccessMinutes = 30
	DefaultRefreshHours  = 720
)

// Config 是服务端配置，字段可由 server.json 提供并被环境变量覆盖。
type Config struct {
	Addr               string `json:"addr"`
	DBPath             string `json:"db_path"`
	AdminUsername      string `json:"admin_username"`
	AccessTokenMinutes int    `json:"access_token_minutes"`
	RefreshTokenHours  int    `json:"refresh_token_hours"`
}

// Default 返回带路径展开的默认配置。
func Default() (Config, error) {
	dir, err := DataDir()
	if err != nil {
		return Config{}, err
	}
	return Config{
		Addr:               DefaultAddr,
		DBPath:             filepath.Join(dir, "todo_helper.db"),
		AdminUsername:      DefaultAdminUsername,
		AccessTokenMinutes: DefaultAccessMinutes,
		RefreshTokenHours:  DefaultRefreshHours,
	}, nil
}

// DataDir 返回数据目录 ~/.todo_helper（可用 TODO_HELPER_HOME 覆盖）。
func DataDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv(EnvHome)); home != "" {
		return filepath.Clean(home), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: 无法定位用户目录: %w", err)
	}
	return filepath.Join(home, ".todo_helper"), nil
}

// SessionPath 返回客户端登录态文件路径。
func SessionPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// ResolvePath 按 参数 > 环境变量 > 默认值 的顺序确定配置文件路径。
func ResolvePath(flagPath string) (string, error) {
	if p := strings.TrimSpace(flagPath); p != "" {
		return expandHome(p)
	}
	if p := strings.TrimSpace(os.Getenv(EnvConfig)); p != "" {
		return expandHome(p)
	}
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "server.json"), nil
}

// Load 读取配置文件（不存在则使用默认值），再应用环境变量。
// 返回生效配置与实际使用的配置文件路径。
func Load(flagPath string) (Config, string, error) {
	path, err := ResolvePath(flagPath)
	if err != nil {
		return Config{}, "", err
	}
	cfg, err := Default()
	if err != nil {
		return Config{}, "", err
	}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, "", fmt.Errorf("config: 解析 %s 失败: %w", path, err)
		}
	case !errors.Is(readErr, os.ErrNotExist):
		return Config{}, "", fmt.Errorf("config: 读取 %s 失败: %w", path, readErr)
	}
	if err := cfg.applyEnv(); err != nil {
		return Config{}, "", err
	}
	if err := cfg.Finalize(); err != nil {
		return Config{}, "", err
	}
	return cfg, path, nil
}

func (c *Config) applyEnv() error {
	if v := strings.TrimSpace(os.Getenv(EnvAddr)); v != "" {
		c.Addr = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvDBPath)); v != "" {
		c.DBPath = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvAdminUsername)); v != "" {
		c.AdminUsername = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvAccessTTL)); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: %s 必须是整数: %w", EnvAccessTTL, err)
		}
		c.AccessTokenMinutes = n
	}
	if v := strings.TrimSpace(os.Getenv(EnvRefreshTTL)); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: %s 必须是整数: %w", EnvRefreshTTL, err)
		}
		c.RefreshTokenHours = n
	}
	return nil
}

// Finalize 清理空白、展开路径并校验配置，供命令行覆盖参数后复用。
func (c *Config) Finalize() error {
	c.Addr = strings.TrimSpace(c.Addr)
	c.AdminUsername = strings.TrimSpace(c.AdminUsername)
	if c.Addr == "" {
		return errors.New("config: addr 不能为空")
	}
	if c.AdminUsername == "" {
		return errors.New("config: admin_username 不能为空")
	}
	if c.AccessTokenMinutes < 1 {
		return errors.New("config: access_token_minutes 必须大于 0")
	}
	if c.RefreshTokenHours < 1 {
		return errors.New("config: refresh_token_hours 必须大于 0")
	}
	dbPath, err := expandHome(c.DBPath)
	if err != nil {
		return err
	}
	c.DBPath = dbPath
	return nil
}

// EnsureFile 在配置文件不存在时写入默认配置，已存在则不动。
func EnsureFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg, err := Default()
	if err != nil {
		return err
	}
	return Save(path, cfg)
}

// Save 将配置写入指定路径。
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("config: 创建配置目录失败: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("config: 写入 %s 失败: %w", path, err)
	}
	return nil
}

// AccessTTL 返回访问令牌有效期。
func (c Config) AccessTTL() time.Duration {
	return time.Duration(c.AccessTokenMinutes) * time.Minute
}

// RefreshTTL 返回刷新令牌有效期。
func (c Config) RefreshTTL() time.Duration {
	return time.Duration(c.RefreshTokenHours) * time.Hour
}

func expandHome(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("config: 路径不能为空")
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("config: 无法定位用户目录: %w", err)
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	return filepath.Clean(p), nil
}
