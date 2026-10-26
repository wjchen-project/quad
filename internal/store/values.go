package store

import "strings"

// NormalizeQuadrant 把命令行的别名（q1-q4、中文方位）规范为存储值。
func NormalizeQuadrant(input string) (string, bool) {
	switch normalize(input) {
	case "urgent_important", "important_urgent", "q1", "1", "重要紧急", "紧急重要":
		return QuadrantUrgentImportant, true
	case "important_not_urgent", "q2", "2", "重要不紧急", "重要但不紧急":
		return QuadrantImportantNotUrgent, true
	case "urgent_not_important", "q3", "3", "紧急不重要", "紧急但不重要":
		return QuadrantUrgentNotImportant, true
	case "not_urgent_not_important", "neither", "q4", "4", "不重要不紧急", "不紧急不重要":
		return QuadrantNotUrgentNotImportant, true
	default:
		return "", false
	}
}

// NormalizeStatus 把命令行的别名规范为存储值。
func NormalizeStatus(input string) (string, bool) {
	switch normalize(input) {
	case "pending", "todo", "待办", "未开始":
		return StatusPending, true
	case "in_progress", "doing", "进行中":
		return StatusInProgress, true
	case "done", "completed", "完成", "已完成":
		return StatusDone, true
	case "cancelled", "canceled", "已取消":
		return StatusCancelled, true
	default:
		return "", false
	}
}

// NormalizeRole 把角色别名规范为存储值。
func NormalizeRole(input string) (string, bool) {
	switch normalize(input) {
	case "admin", "administrator", "管理员":
		return RoleAdmin, true
	case "user", "member", "普通用户":
		return RoleUser, true
	default:
		return "", false
	}
}

// QuadrantLabel 返回四象限的中文名，用于终端展示。
func QuadrantLabel(q string) string {
	switch q {
	case QuadrantUrgentImportant:
		return "重要紧急"
	case QuadrantImportantNotUrgent:
		return "重要不紧急"
	case QuadrantUrgentNotImportant:
		return "紧急不重要"
	case QuadrantNotUrgentNotImportant:
		return "不重要不紧急"
	default:
		return q
	}
}

// StatusLabel 返回状态的中文名，用于终端展示。
func StatusLabel(s string) string {
	switch s {
	case StatusPending:
		return "待办"
	case StatusInProgress:
		return "进行中"
	case StatusDone:
		return "已完成"
	case StatusCancelled:
		return "已取消"
	default:
		return s
	}
}

func normalize(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}
