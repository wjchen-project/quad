// Package store 封装 SQLite 数据访问，包含用户、待办与刷新令牌。
//
// 作者: wjchen
package store

import (
	"time"
)

// 用户角色。
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// 待办的紧急 / 重要四象限。
const (
	QuadrantUrgentImportant       = "urgent_important"         // 重要且紧急
	QuadrantImportantNotUrgent    = "important_not_urgent"     // 重要不紧急
	QuadrantUrgentNotImportant    = "urgent_not_important"     // 紧急不重要
	QuadrantNotUrgentNotImportant = "not_urgent_not_important" // 不重要不紧急
)

// 待办状态。
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusCancelled  = "cancelled"
)

// User 是接口返回的用户信息，不含密码哈希。
type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Todo 是待办记录。
type Todo struct {
	ID          int64      `json:"id"`
	UserID      int64      `json:"user_id"`
	Title       string     `json:"title"`
	Notes       string     `json:"notes"`
	Quadrant    string     `json:"quadrant"`
	Status      string     `json:"status"`
	Progress    int        `json:"progress"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

// RefreshToken 是刷新令牌的入库形态，TokenHash 为明文令牌的 SHA-256。
type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// TodoFilter 控制待办列表的过滤与分页。
type TodoFilter struct {
	Status   string
	Quadrant string
	Limit    int
	Offset   int
}
