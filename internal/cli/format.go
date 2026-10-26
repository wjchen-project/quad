package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"todo_helper/internal/store"
)

// displayWidth 估算终端显示宽度，把常见 CJK 字符按 2 列计算。
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if isWideRune(r) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE10 && r <= 0xFE19,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1FAFF:
		return true
	default:
		return false
	}
}

func padRight(s string, width int) string {
	if d := displayWidth(s); d < width {
		return s + strings.Repeat(" ", width-d)
	}
	return s
}

func truncateDisplay(s string, max int) string {
	if displayWidth(s) <= max {
		return s
	}
	var b strings.Builder
	width := 0
	for _, r := range s {
		w := 1
		if isWideRune(r) {
			w = 2
		}
		if width+w > max-1 {
			break
		}
		b.WriteRune(r)
		width += w
	}
	return b.String() + "…"
}

func printTable(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = displayWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			if w := displayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	printRow := func(cells []string) {
		var b strings.Builder
		for i, cell := range cells {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(padRight(cell, widths[i]))
		}
		fmt.Println(strings.TrimRight(b.String(), " "))
	}
	printRow(headers)
	sep := make([]string, len(headers))
	for i, w := range widths {
		sep[i] = strings.Repeat("-", w)
	}
	printRow(sep)
	for _, row := range rows {
		printRow(row)
	}
}

func printTodoTable(todos []store.Todo) {
	if len(todos) == 0 {
		fmt.Println("（没有待办）")
		return
	}
	rows := make([][]string, 0, len(todos))
	for _, t := range todos {
		rows = append(rows, []string{
			strconv.FormatInt(t.ID, 10),
			store.QuadrantLabel(t.Quadrant),
			store.StatusLabel(t.Status),
			fmt.Sprintf("%d%%", t.Progress),
			formatTime(t.CreatedAt),
			truncateDisplay(t.Title, 40),
		})
	}
	printTable([]string{"ID", "象限", "状态", "进度", "创建时间", "标题"}, rows)
}

func printUserTable(users []store.User) {
	if len(users) == 0 {
		fmt.Println("（没有用户）")
		return
	}
	rows := make([][]string, 0, len(users))
	for _, u := range users {
		rows = append(rows, []string{
			strconv.FormatInt(u.ID, 10),
			u.Username,
			u.Role,
			formatTime(u.CreatedAt),
		})
	}
	printTable([]string{"ID", "用户名", "角色", "创建时间"}, rows)
}

func printTodoDetail(t store.Todo) {
	completed := "-"
	if t.CompletedAt != nil {
		completed = formatTime(*t.CompletedAt)
	}
	fmt.Printf("ID:       %d\n", t.ID)
	fmt.Printf("标题:     %s\n", t.Title)
	fmt.Printf("象限:     %s (%s)\n", store.QuadrantLabel(t.Quadrant), t.Quadrant)
	fmt.Printf("状态:     %s (%s)\n", store.StatusLabel(t.Status), t.Status)
	fmt.Printf("进度:     %d%%\n", t.Progress)
	fmt.Printf("创建时间: %s\n", formatTime(t.CreatedAt))
	fmt.Printf("更新时间: %s\n", formatTime(t.UpdatedAt))
	fmt.Printf("完成时间: %s\n", completed)
	if strings.TrimSpace(t.Notes) == "" {
		fmt.Println("备注:     -")
		return
	}
	fmt.Println("备注:")
	for _, line := range strings.Split(t.Notes, "\n") {
		fmt.Printf("  %s\n", line)
	}
}

func formatTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}
