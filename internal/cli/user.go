package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"quad/internal/auth"
	"quad/internal/client"
	"quad/internal/store"
)

const userUsage = `用法: quad user <子命令> [参数]

以下命令均需要管理员权限:
  list                      列出全部用户
  add                       创建用户
  passwd <用户名|ID>        重置密码
  role <用户名|ID> <角色>   修改角色（admin / user）
  rm <用户名|ID>            删除用户
`

func runUser(args []string) int {
	if len(args) == 0 {
		fmt.Print(userUsage)
		return 2
	}
	command, rest := args[0], args[1:]
	switch command {
	case "list", "ls":
		return runUserList(rest)
	case "add", "create":
		return runUserAdd(rest)
	case "passwd", "password", "reset-password":
		return runUserPasswd(rest)
	case "role":
		return runUserRole(rest)
	case "rm", "remove", "delete":
		return runUserRemove(rest)
	case "help", "-h", "--help":
		fmt.Print(userUsage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "错误: 未知 user 子命令 %q\n\n", command)
		fmt.Print(userUsage)
		return 2
	}
}

func runUserList(args []string) int {
	fs := newFlagSet("user list")
	fs.Usage = func() { fmt.Print("用法: quad user list\n") }
	if ok, code := parseFlags(fs, args, false); !ok {
		return code
	}
	c, code := requireLogin()
	if c == nil {
		return code
	}
	users, err := c.ListUsers(context.Background())
	if err != nil {
		return fail("查询用户列表失败: %v", err)
	}
	printUserTable(users)
	return 0
}

func runUserAdd(args []string) int {
	fs := newFlagSet("user add")
	username := fs.String("username", "", "用户名（3-32 位字母、数字或 _ . -）")
	fs.StringVar(username, "u", "", "同 --username")
	password := fs.String("password", "", "密码（留空则随机生成并打印）")
	fs.StringVar(password, "p", "", "同 --password")
	role := fs.String("role", store.RoleUser, "角色: user 或 admin")
	fs.StringVar(role, "r", store.RoleUser, "同 --role")
	fs.Usage = func() {
		printUsage(fs, `用法: quad user add --username <用户名> [参数]

参数:
`)
	}
	if ok, code := parseFlags(fs, args, false); !ok {
		return code
	}
	if strings.TrimSpace(*username) == "" {
		return fail("缺少 --username 参数")
	}
	normalizedRole, ok := store.NormalizeRole(*role)
	if !ok {
		return fail("--role 取值无效: admin 或 user")
	}
	pw := *password
	generated := false
	if pw == "" {
		var err error
		pw, err = auth.GeneratePassword(16)
		if err != nil {
			return fail("生成随机密码失败: %v", err)
		}
		generated = true
	}
	c, code := requireLogin()
	if c == nil {
		return code
	}
	user, err := c.CreateUser(context.Background(), *username, pw, normalizedRole)
	if err != nil {
		return fail("创建用户失败: %v", err)
	}
	fmt.Printf("已创建用户 %s (#%d, %s)\n", user.Username, user.ID, user.Role)
	if generated {
		fmt.Printf("初始密码: %s\n请转告用户尽快修改密码。\n", pw)
	}
	return 0
}

func runUserPasswd(args []string) int {
	fs := newFlagSet("user passwd")
	password := fs.String("password", "", "新密码（留空则随机生成并打印）")
	fs.StringVar(password, "p", "", "同 --password")
	fs.Usage = func() {
		printUsage(fs, `用法: quad user passwd <用户名|ID> [参数]

参数:
`)
	}
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	ref, code := singleArg(fs, "用法: quad user passwd <用户名|ID> [参数]")
	if code != 0 {
		return code
	}
	c, code := requireLogin()
	if c == nil {
		return code
	}
	ctx := context.Background()
	target, err := resolveUser(ctx, c, ref)
	if err != nil {
		return fail("%v", err)
	}
	pw := *password
	generated := false
	if pw == "" {
		pw, err = auth.GeneratePassword(16)
		if err != nil {
			return fail("生成随机密码失败: %v", err)
		}
		generated = true
	}
	if _, err := c.UpdateUser(ctx, target.ID, &pw, nil); err != nil {
		return fail("重置密码失败: %v", err)
	}
	fmt.Printf("已重置用户 %s 的密码\n", target.Username)
	if generated {
		fmt.Printf("新密码: %s\n", pw)
	}
	if strings.EqualFold(target.Username, c.Username()) {
		fmt.Println("提示: 当前账号的刷新令牌已失效，访问令牌过期后需要重新登录。")
	}
	return 0
}

func runUserRole(args []string) int {
	fs := newFlagSet("user role")
	fs.Usage = func() { fmt.Print("用法: quad user role <用户名|ID> <admin|user>\n") }
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	if fs.NArg() != 2 {
		fmt.Fprint(os.Stderr, "用法: quad user role <用户名|ID> <admin|user>\n")
		return 2
	}
	role, ok := store.NormalizeRole(fs.Arg(1))
	if !ok {
		return fail("角色只能是 admin 或 user")
	}
	c, code := requireLogin()
	if c == nil {
		return code
	}
	ctx := context.Background()
	target, err := resolveUser(ctx, c, fs.Arg(0))
	if err != nil {
		return fail("%v", err)
	}
	updated, err := c.UpdateUser(ctx, target.ID, nil, &role)
	if err != nil {
		return fail("修改角色失败: %v", err)
	}
	fmt.Printf("用户 %s 的角色已改为 %s\n", updated.Username, updated.Role)
	return 0
}

func runUserRemove(args []string) int {
	fs := newFlagSet("user rm")
	fs.Usage = func() { fmt.Print("用法: quad user rm <用户名|ID>\n") }
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	ref, code := singleArg(fs, "用法: quad user rm <用户名|ID>")
	if code != 0 {
		return code
	}
	c, code := requireLogin()
	if c == nil {
		return code
	}
	ctx := context.Background()
	target, err := resolveUser(ctx, c, ref)
	if err != nil {
		return fail("%v", err)
	}
	if err := c.DeleteUser(ctx, target.ID); err != nil {
		return fail("删除用户失败: %v", err)
	}
	fmt.Printf("已删除用户 %s (#%d)\n", target.Username, target.ID)
	return 0
}

func singleArg(fs *flag.FlagSet, usage string) (string, int) {
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		fmt.Fprintln(os.Stderr, usage)
		return "", 2
	}
	return strings.TrimSpace(fs.Arg(0)), 0
}

func resolveUser(ctx context.Context, c *client.Client, ref string) (store.User, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return store.User{}, fmt.Errorf("用户名或 ID 不能为空")
	}
	users, err := c.ListUsers(ctx)
	if err != nil {
		return store.User{}, fmt.Errorf("查询用户列表失败: %w", err)
	}
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		for _, u := range users {
			if u.ID == id {
				return u, nil
			}
		}
	}
	for _, u := range users {
		if strings.EqualFold(u.Username, ref) {
			return u, nil
		}
	}
	return store.User{}, fmt.Errorf("用户 %q 不存在", ref)
}
