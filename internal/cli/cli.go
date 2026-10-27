// Package cli 实现 quad 的全部子命令。
//
// 作者: wjchen
package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// Version 由构建时可通过 -ldflags 覆盖。
var Version = "0.1.0"

const rootUsage = `quad - 终端待办工具（客户端与服务端同一个二进制）

用法:
  quad <命令> [参数]

命令:
  serve        启动 HTTP API 服务端
  login        登录并将 token / refresh_token 保存到用户目录
  logout       退出登录并清除本地登录态
  whoami       显示当前登录用户
  todo         待办管理: add | list | show | update | done | start | rm
  user         用户管理（需管理员）: list | add | passwd | role | rm
  version      显示版本
  help         显示本帮助

典型流程:
  quad serve                      # 终端 1：启动服务端（首次会打印随机管理员密码）
  quad login -u sysadmin          # 终端 2：登录
  quad todo add -q q1 -t "写周报"  # 添加重要且紧急的待办
  quad todo list

运行 "quad <命令> -h" 查看子命令参数。
`

// Run 是 CLI 入口，返回进程退出码。
func Run(args []string) int {
	if len(args) == 0 {
		fmt.Print(rootUsage)
		return 2
	}
	command, rest := args[0], args[1:]
	switch command {
	case "serve":
		return runServe(rest)
	case "login":
		return runLogin(rest)
	case "logout":
		return runLogout(rest)
	case "whoami":
		return runWhoami(rest)
	case "todo":
		return runTodo(rest)
	case "user":
		return runUser(rest)
	case "version", "--version", "-v":
		fmt.Printf("quad v%s\n", Version)
		return 0
	case "help", "-h", "--help":
		fmt.Print(rootUsage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "错误: 未知命令 %q\n\n", command)
		fmt.Print(rootUsage)
		return 2
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func printUsage(fs *flag.FlagSet, header string) {
	fmt.Print(header)
	fs.SetOutput(os.Stdout)
	fs.PrintDefaults()
	fs.SetOutput(os.Stderr)
}

// parseFlags 返回是否继续执行以及退出码。allowPositional 为 false 时拒绝多余位置参数。
// 解析前会把位置参数移到末尾，使 "todo update 3 -p 50" 这类写法也能正确解析。
func parseFlags(fs *flag.FlagSet, args []string, allowPositional bool) (bool, int) {
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, 0
		}
		return false, 2
	}
	if !allowPositional && fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "错误: 多余的参数: %v\n", fs.Args())
		fs.Usage()
		return false, 2
	}
	return true, 0
}

// reorderArgs 把位置参数收集到末尾，避免 flag 包在遇到第一个位置参数时停止解析。
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	flags := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue
		}
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		if boolFlag, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && boolFlag.IsBoolFlag() {
			continue
		}
		// 非布尔开关的取值也属于这个开关，不能被当成位置参数。
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positional...)
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "错误: "+format+"\n", args...)
	return 1
}
