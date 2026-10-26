package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"todo_helper/internal/client"
	"todo_helper/internal/store"
)

const todoUsage = `用法: todo_helper todo <子命令> [参数]

子命令:
  add      添加待办（必须指定象限）
  list     列出待办
  show     查看待办详情
  update   修改待办
  done     标记为已完成
  start    标记为进行中
  rm       删除待办

象限取值: q1 重要紧急 / q2 重要不紧急 / q3 紧急不重要 / q4 不重要不紧急
`

const quadrantHelp = "象限: q1 重要紧急 / q2 重要不紧急 / q3 紧急不重要 / q4 不重要不紧急"

func runTodo(args []string) int {
	if len(args) == 0 {
		fmt.Print(todoUsage)
		return 2
	}
	command, rest := args[0], args[1:]
	switch command {
	case "add":
		return runTodoAdd(rest)
	case "list", "ls":
		return runTodoList(rest)
	case "show":
		return runTodoShow(rest)
	case "update", "edit":
		return runTodoUpdate(rest)
	case "done":
		return runTodoSetStatus(rest, store.StatusDone, "已完成", "done")
	case "start":
		return runTodoSetStatus(rest, store.StatusInProgress, "进行中", "start")
	case "rm", "remove", "delete":
		return runTodoRemove(rest)
	case "help", "-h", "--help":
		fmt.Print(todoUsage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "错误: 未知 todo 子命令 %q\n\n", command)
		fmt.Print(todoUsage)
		return 2
	}
}

func runTodoAdd(args []string) int {
	fs := newFlagSet("todo add")
	title := fs.String("title", "", "待办标题")
	fs.StringVar(title, "t", "", "同 --title")
	notes := fs.String("notes", "", "备注")
	fs.StringVar(notes, "n", "", "同 --notes")
	quadrantRaw := fs.String("quadrant", "", quadrantHelp)
	fs.StringVar(quadrantRaw, "q", "", "同 --quadrant")
	statusRaw := fs.String("status", "", "初始状态: pending|in_progress|done|cancelled")
	fs.StringVar(statusRaw, "s", "", "同 --status")
	progress := fs.Int("progress", -1, "进度 0-100")
	fs.IntVar(progress, "p", -1, "同 --progress")
	fs.Usage = func() {
		printUsage(fs, `用法: todo_helper todo add --title <标题> --quadrant <象限> [参数]

参数:
`)
	}
	if ok, code := parseFlags(fs, args, false); !ok {
		return code
	}
	if strings.TrimSpace(*title) == "" {
		return fail("缺少 --title 参数")
	}
	if strings.TrimSpace(*quadrantRaw) == "" {
		return fail("缺少 --quadrant 参数：q1 重要紧急 / q2 重要不紧急 / q3 紧急不重要 / q4 不重要不紧急")
	}
	quadrant, ok := store.NormalizeQuadrant(*quadrantRaw)
	if !ok {
		return fail("--quadrant 取值无效：%s", quadrantHelp)
	}
	status := ""
	if strings.TrimSpace(*statusRaw) != "" {
		normalized, ok := store.NormalizeStatus(*statusRaw)
		if !ok {
			return fail("--status 取值无效: pending|in_progress|done|cancelled")
		}
		status = normalized
	}
	var progressPtr *int
	if *progress >= 0 {
		if *progress > 100 {
			return fail("--progress 必须在 0-100 之间")
		}
		value := *progress
		progressPtr = &value
	} else if *progress != -1 {
		return fail("--progress 必须在 0-100 之间")
	}

	c, code := requireLogin()
	if c == nil {
		return code
	}
	todo, err := c.CreateTodo(context.Background(), *title, *notes, quadrant, status, progressPtr)
	if err != nil {
		return fail("添加待办失败: %v", err)
	}
	fmt.Printf("已添加待办 #%d [%s/%s] %s\n",
		todo.ID, store.QuadrantLabel(todo.Quadrant), store.StatusLabel(todo.Status), todo.Title)
	return 0
}

func runTodoList(args []string) int {
	fs := newFlagSet("todo list")
	statusRaw := fs.String("status", "", "按状态过滤")
	fs.StringVar(statusRaw, "s", "", "同 --status")
	quadrantRaw := fs.String("quadrant", "", quadrantHelp)
	fs.StringVar(quadrantRaw, "q", "", "同 --quadrant")
	limit := fs.Int("limit", 0, "最多返回条数（默认 200）")
	offset := fs.Int("offset", 0, "跳过条数")
	fs.Usage = func() {
		printUsage(fs, `用法: todo_helper todo list [参数]

参数:
`)
	}
	if ok, code := parseFlags(fs, args, false); !ok {
		return code
	}
	query := client.TodoQuery{Limit: *limit, Offset: *offset}
	if strings.TrimSpace(*statusRaw) != "" {
		status, ok := store.NormalizeStatus(*statusRaw)
		if !ok {
			return fail("--status 取值无效: pending|in_progress|done|cancelled")
		}
		query.Status = status
	}
	if strings.TrimSpace(*quadrantRaw) != "" {
		quadrant, ok := store.NormalizeQuadrant(*quadrantRaw)
		if !ok {
			return fail("--quadrant 取值无效：%s", quadrantHelp)
		}
		query.Quadrant = quadrant
	}
	if *limit < 0 || *offset < 0 {
		return fail("--limit / --offset 不能为负数")
	}

	c, code := requireLogin()
	if c == nil {
		return code
	}
	todos, err := c.ListTodos(context.Background(), query)
	if err != nil {
		return fail("查询待办失败: %v", err)
	}
	printTodoTable(todos)
	return 0
}

func runTodoShow(args []string) int {
	fs := newFlagSet("todo show")
	fs.Usage = func() { fmt.Print("用法: todo_helper todo show <ID>\n") }
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	id, code := parseIDArg(fs, "用法: todo_helper todo show <ID>")
	if code != 0 {
		return code
	}
	c, code := requireLogin()
	if c == nil {
		return code
	}
	todo, err := c.GetTodo(context.Background(), id)
	if err != nil {
		return fail("查询待办失败: %v", err)
	}
	printTodoDetail(todo)
	return 0
}

func runTodoUpdate(args []string) int {
	fs := newFlagSet("todo update")
	title := fs.String("title", "", "新标题")
	fs.StringVar(title, "t", "", "同 --title")
	notes := fs.String("notes", "", "新备注（传空字符串可清空）")
	fs.StringVar(notes, "n", "", "同 --notes")
	quadrantRaw := fs.String("quadrant", "", quadrantHelp)
	fs.StringVar(quadrantRaw, "q", "", "同 --quadrant")
	statusRaw := fs.String("status", "", "新状态: pending|in_progress|done|cancelled")
	fs.StringVar(statusRaw, "s", "", "同 --status")
	progress := fs.Int("progress", -1, "新进度 0-100")
	fs.IntVar(progress, "p", -1, "同 --progress")
	fs.Usage = func() {
		printUsage(fs, `用法: todo_helper todo update <ID> [参数]

只更新显式传入的字段。参数:
`)
	}
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	id, code := parseIDArg(fs, "用法: todo_helper todo update <ID> [参数]")
	if code != 0 {
		return code
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	patch := client.TodoPatch{}
	if set["title"] || set["t"] {
		value := strings.TrimSpace(*title)
		if value == "" {
			return fail("--title 不能为空")
		}
		patch.Title = &value
	}
	if set["notes"] || set["n"] {
		patch.Notes = notes
	}
	if set["quadrant"] || set["q"] {
		quadrant, ok := store.NormalizeQuadrant(*quadrantRaw)
		if !ok {
			return fail("--quadrant 取值无效：%s", quadrantHelp)
		}
		patch.Quadrant = &quadrant
	}
	if set["status"] || set["s"] {
		status, ok := store.NormalizeStatus(*statusRaw)
		if !ok {
			return fail("--status 取值无效: pending|in_progress|done|cancelled")
		}
		patch.Status = &status
	}
	if set["progress"] || set["p"] {
		if *progress < 0 || *progress > 100 {
			return fail("--progress 必须在 0-100 之间")
		}
		value := *progress
		patch.Progress = &value
	}
	if patch.Title == nil && patch.Notes == nil && patch.Quadrant == nil && patch.Status == nil && patch.Progress == nil {
		return fail("没有指定要更新的字段，使用 -h 查看可用参数")
	}

	c, code := requireLogin()
	if c == nil {
		return code
	}
	todo, err := c.UpdateTodo(context.Background(), id, patch)
	if err != nil {
		return fail("更新待办失败: %v", err)
	}
	fmt.Printf("已更新待办 #%d [%s/%s %d%%] %s\n",
		todo.ID, store.QuadrantLabel(todo.Quadrant), store.StatusLabel(todo.Status), todo.Progress, todo.Title)
	return 0
}

func runTodoSetStatus(args []string, status, label, command string) int {
	fs := newFlagSet("todo " + command)
	fs.Usage = func() { fmt.Printf("用法: todo_helper todo %s <ID>\n", command) }
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	id, code := parseIDArg(fs, fmt.Sprintf("用法: todo_helper todo %s <ID>", command))
	if code != 0 {
		return code
	}
	c, code := requireLogin()
	if c == nil {
		return code
	}
	todo, err := c.UpdateTodo(context.Background(), id, client.TodoPatch{Status: &status})
	if err != nil {
		return fail("更新待办失败: %v", err)
	}
	fmt.Printf("待办 #%d 已标记为%s\n", todo.ID, label)
	return 0
}

func runTodoRemove(args []string) int {
	fs := newFlagSet("todo rm")
	fs.Usage = func() { fmt.Print("用法: todo_helper todo rm <ID>\n") }
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	id, code := parseIDArg(fs, "用法: todo_helper todo rm <ID>")
	if code != 0 {
		return code
	}
	c, code := requireLogin()
	if c == nil {
		return code
	}
	if err := c.DeleteTodo(context.Background(), id); err != nil {
		return fail("删除待办失败: %v", err)
	}
	fmt.Printf("已删除待办 #%d\n", id)
	return 0
}

// parseIDArg 读取唯一的位置参数作为 ID。返回非 0 表示失败。
func parseIDArg(fs *flag.FlagSet, usage string) (int64, int) {
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, usage)
		return 0, 2
	}
	raw := strings.TrimSpace(fs.Arg(0))
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fail("无效的 ID: %q", raw)
	}
	return id, 0
}
