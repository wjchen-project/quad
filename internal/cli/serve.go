package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"quad/internal/api"
	"quad/internal/bootstrap"
	"quad/internal/config"
	"quad/internal/store"
)

const serveUsage = `用法: quad serve [参数]

启动 API 服务端。首次启动会创建管理员账号并打印随机密码。
配置优先级：命令行参数 > 环境变量 > 配置文件 > 默认值。

参数:
`

func runServe(args []string) int {
	fs := newFlagSet("serve")
	addr := fs.String("addr", "", "监听地址（默认 127.0.0.1:8787）")
	dbPath := fs.String("db", "", "SQLite 数据库路径（默认 ~/.quad/quad.db）")
	configFlag := fs.String("config", "", "配置文件路径（默认 ~/.quad/server.json）")
	adminUsername := fs.String("admin-username", "", "管理员用户名（默认 sysadmin）")
	resetPassword := fs.Bool("reset-admin-password", false, "启动时重置管理员密码并打印新密码")
	fs.Usage = func() { printUsage(fs, serveUsage) }
	if ok, code := parseFlags(fs, args, false); !ok {
		return code
	}

	path, err := config.ResolvePath(*configFlag)
	if err != nil {
		return fail("%v", err)
	}
	if err := config.EnsureFile(path); err != nil {
		return fail("%v", err)
	}
	cfg, path, err := config.Load(*configFlag)
	if err != nil {
		return fail("%v", err)
	}
	if strings.TrimSpace(*addr) != "" {
		cfg.Addr = *addr
	}
	if strings.TrimSpace(*dbPath) != "" {
		cfg.DBPath = *dbPath
	}
	if strings.TrimSpace(*adminUsername) != "" {
		cfg.AdminUsername = *adminUsername
	}
	if err := cfg.Finalize(); err != nil {
		return fail("%v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fail("%v", err)
	}
	defer st.Close()

	ctx := context.Background()
	admin, err := bootstrap.EnsureAdmin(ctx, st, cfg.AdminUsername)
	if err != nil {
		return fail("初始化管理员失败: %v", err)
	}
	if admin.Created {
		fmt.Printf("\n首次启动：已创建管理员账号\n  用户名: %s\n  密码:   %s\n\n请立即保存该密码，它不会再次显示。\n\n",
			admin.Username, admin.Password)
	}
	if admin.Promoted {
		fmt.Printf("已把已有用户 %s 提升为管理员。\n\n", admin.Username)
	}
	if *resetPassword {
		pw, err := bootstrap.ResetAdminPassword(ctx, st, cfg.AdminUsername)
		if err != nil {
			return fail("重置管理员密码失败: %v", err)
		}
		fmt.Printf("已重置管理员 %s 的密码\n  新密码: %s\n\n", cfg.AdminUsername, pw)
	}
	_ = st.DeleteExpiredRefreshTokens(ctx, time.Now())

	apiServer, err := api.New(ctx, api.Options{
		Store:      st,
		AccessTTL:  cfg.AccessTTL(),
		RefreshTTL: cfg.RefreshTTL(),
		Logger:     logger,
	})
	if err != nil {
		return fail("%v", err)
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fail("监听 %s 失败: %v", cfg.Addr, err)
	}
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Printf("服务端已启动\n  地址:     %s\n  数据库:   %s\n  配置文件: %s\n  管理员:   %s\n\n",
		displayURL(cfg.Addr), cfg.DBPath, path, cfg.AdminUsername)

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.Serve(listener)
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fail("服务端异常退出: %v", err)
		}
		return 0
	case <-sigCtx.Done():
		fmt.Println("\n正在关闭服务端…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fail("关闭服务端失败: %v", err)
		}
		return 0
	}
}

func displayURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	return "http://" + addr
}
