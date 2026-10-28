package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"quad/internal/client"
	"quad/internal/store"
)

const todoUsage = `用法: quad todo <子命令> [参数]

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
		printUsage(fs, `用法: quad todo add --title <标题> --quadrant <象限> [参数]

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
	statusRaw := fs.String("status", "", "按状态过滤（默认排除已完成，指定 done 可查看）")
	fs.StringVar(statusRaw, "s", "", "同 --status")
	quadrantRaw := fs.String("quadrant", "", quadrantHelp)
	fs.StringVar(quadrantRaw, "q", "", "同 --quadrant")
	titleRaw := fs.String("title", "", "按标题关键字模糊过滤")
	fs.StringVar(titleRaw, "t", "", "同 --title")
	fromRaw := fs.String("from", "", "创建时间起点，如 2026-10-01 或 \"2026-10-01 08:30\"")
	toRaw := fs.String("to", "", "创建时间终点，纯日期包含当天")
	limit := fs.Int("limit", 0, "最多返回条数（默认 200）")
	offset := fs.Int("offset", 0, "跳过条数")
	fs.Usage = func() {
		printUsage(fs, `用法: quad todo list [参数]

默认排除已完成（done）的待办；需要查看时指定 --status done。参数:
`)
	}
	if ok, code := parseFlags(fs, args, false); !ok {
		return code
	}
	query := client.TodoQuery{Limit: *limit, Offset: *offset}
	statusSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "status" || f.Name == "s" {
			statusSet = true
		}
	})
	if strings.TrimSpace(*statusRaw) != "" {
		status, ok := store.NormalizeStatus(*statusRaw)
		if !ok {
			return fail("--status 取值无效: pending|in_progress|done|cancelled")
		}
		query.Status = status
	}
	// 未显式指定状态时默认排除已完成，保持列表聚焦在未完成事项上。
	if !statusSet {
		query.ExcludeDone = true
	}
	if strings.TrimSpace(*quadrantRaw) != "" {
		quadrant, ok := store.NormalizeQuadrant(*quadrantRaw)
		if !ok {
			return fail("--quadrant 取值无效：%s", quadrantHelp)
		}
		query.Quadrant = quadrant
	}
	if keyword := strings.TrimSpace(*titleRaw); keyword != "" {
		query.Title = keyword
	}
	if raw := strings.TrimSpace(*fromRaw); raw != "" {
		from, err := parseTimeBound(raw, false)
		if err != nil {
			return fail("--from %v", err)
		}
		query.From = &from
	}
	if raw := strings.TrimSpace(*toRaw); raw != "" {
		to, err := parseTimeBound(raw, true)
		if err != nil {
			return fail("--to %v", err)
		}
		query.To = &to
	}
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		return fail("--from 不能晚于 --to")
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

// parseTimeBound 解析 --from / --to 的时间参数，带时区时以参数为准，
// 否则按本地时区解析；纯日期作为终点时覆盖到当天最后一毫秒。
func parseTimeBound(raw string, endOfDay bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		if ts, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return ts, nil
		}
	}
	ts, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("时间格式无效: %q（支持 2026-10-01、2026-10-01 08:30 或 RFC3339）", raw)
	}
	if endOfDay {
		ts = ts.AddDate(0, 0, 1).Add(-time.Millisecond)
	}
	return ts, nil
}

func runTodoShow(args []string) int {
	fs := newFlagSet("todo show")
	fs.Usage = func() { fmt.Print("用法: quad todo show <ID>\n") }
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	id, code := parseIDArg(fs, "用法: quad todo show <ID>")
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
		printUsage(fs, `用法: quad todo update <ID> [参数]

只更新显式传入的字段。参数:
`)
	}
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	id, code := parseIDArg(fs, "用法: quad todo update <ID> [参数]")
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
	fs.Usage = func() { fmt.Printf("用法: quad todo %s <ID>\n", command) }
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	id, code := parseIDArg(fs, fmt.Sprintf("用法: quad todo %s <ID>", command))
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
	fs.Usage = func() { fmt.Print("用法: quad todo rm <ID>\n") }
	if ok, code := parseFlags(fs, args, true); !ok {
		return code
	}
	id, code := parseIDArg(fs, "用法: quad todo rm <ID>")
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
