package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"todo_helper/internal/client"
	"todo_helper/internal/config"
)

const loginUsage = `用法: todo_helper login [参数]

登录成功后，access_token / refresh_token 会保存到用户目录（默认 ~/.todo_helper/session.json），
访问令牌过期时客户端会自动用 refresh_token 续期。

参数:
`

const logoutUsage = `用法: todo_helper logout

撤销服务端刷新令牌并删除本地登录态。
`

const whoamiUsage = `用法: todo_helper whoami

显示当前登录用户与服务端地址。
`

func runLogin(args []string) int {
	fs := newFlagSet("login")
	server := fs.String("server", "", "服务端地址，如 http://127.0.0.1:8787")
	fs.StringVar(server, "s", "", "同 --server")
	username := fs.String("username", "", "用户名（默认取上次登录用户，否则 sysadmin）")
	fs.StringVar(username, "u", "", "同 --username")
	password := fs.String("password", "", "密码（留空则交互式输入）")
	fs.StringVar(password, "p", "", "同 --password")
	fs.Usage = func() { printUsage(fs, loginUsage) }
	if ok, code := parseFlags(fs, args, false); !ok {
		return code
	}

	c, err := client.New(*server)
	if err != nil {
		return fail("%v", err)
	}
	user := strings.TrimSpace(*username)
	if user == "" {
		user = c.Username()
	}
	if user == "" {
		user = strings.TrimSpace(os.Getenv("TODO_HELPER_USERNAME"))
	}
	if user == "" {
		user = config.DefaultAdminUsername
	}
	pw := *password
	if pw == "" {
		pw, err = readPassword("密码: ")
		if err != nil {
			return fail("%v", err)
		}
	}
	if pw == "" {
		return fail("密码不能为空")
	}

	logged, err := c.Login(context.Background(), user, pw)
	if err != nil {
		return fail("登录失败: %v", err)
	}
	fmt.Printf("已登录: %s (%s)\n服务器: %s\n登录态: %s\n",
		logged.Username, logged.Role, c.Server(), c.SessionPath())
	return 0
}

func runLogout(args []string) int {
	fs := newFlagSet("logout")
	fs.Usage = func() { fmt.Print(logoutUsage) }
	if ok, code := parseFlags(fs, args, false); !ok {
		return code
	}
	c, err := client.New("")
	if err != nil {
		return fail("%v", err)
	}
	if !c.LoggedIn() {
		fmt.Println("当前未登录")
		return 0
	}
	if err := c.Logout(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 服务端注销失败: %v（本地登录态已清除）\n", err)
	}
	fmt.Println("已退出登录")
	return 0
}

func runWhoami(args []string) int {
	fs := newFlagSet("whoami")
	fs.Usage = func() { fmt.Print(whoamiUsage) }
	if ok, code := parseFlags(fs, args, false); !ok {
		return code
	}
	c, code := requireLogin()
	if c == nil {
		return code
	}
	user, err := c.Me(context.Background())
	if err != nil {
		if errors.Is(err, client.ErrLoginRequired) {
			return fail("%v", err)
		}
		return fail("查询当前用户失败: %v", err)
	}
	fmt.Printf("%s (%s)\n服务器: %s\n", user.Username, user.Role, c.Server())
	return 0
}

func requireLogin() (*client.Client, int) {
	c, err := client.New("")
	if err != nil {
		return nil, fail("%v", err)
	}
	if !c.LoggedIn() {
		return nil, fail("%v", client.ErrLoginRequired)
	}
	return c, 0
}
