package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"quad/internal/auth"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func mustUser(t *testing.T, st *Store, username, role string) User {
	t.Helper()
	hash, err := auth.HashPassword("password-" + username)
	if err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateUser(context.Background(), username, hash, role)
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return user
}

func TestCreateUserAndLookup(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	alice := mustUser(t, st, "Alice", RoleUser)
	if alice.ID == 0 || alice.Role != RoleUser {
		t.Fatalf("unexpected user: %+v", alice)
	}
	got, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("用户名查询应大小写不敏感: %v", err)
	}
	if got.ID != alice.ID {
		t.Fatalf("ID = %d, want %d", got.ID, alice.ID)
	}
	if _, err := st.GetUserByUsername(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	hash, err := auth.HashPassword("another-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, "ALICE", hash, RoleUser); !errors.Is(err, ErrConflict) {
		t.Fatalf("重复用户名 err = %v, want ErrConflict", err)
	}
	if n, err := st.CountUsers(ctx); err != nil || n != 1 {
		t.Fatalf("CountUsers = %d, %v", n, err)
	}
	if n, err := st.CountAdmins(ctx); err != nil || n != 0 {
		t.Fatalf("CountAdmins = %d, %v", n, err)
	}
}

func TestUserPasswordRoleAndDelete(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	bob := mustUser(t, st, "bob", RoleUser)
	if err := st.SetUserRole(ctx, bob.ID, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	updated, err := st.GetUserByID(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Role != RoleAdmin {
		t.Fatalf("role = %s, want admin", updated.Role)
	}
	if err := st.SetUserRole(ctx, bob.ID, "root"); err == nil {
		t.Fatal("非法角色应报错")
	}
	hash, err := auth.HashPassword("new-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, bob.ID, hash); err != nil {
		t.Fatal(err)
	}
	_, stored, err := st.GetUserAuthByUsername(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := auth.VerifyPassword(stored, "new-password-1"); err != nil || !ok {
		t.Fatalf("新密码校验失败: ok=%v err=%v", ok, err)
	}
	if err := st.DeleteUser(ctx, bob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUserByID(ctx, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后查询 err = %v", err)
	}
	if err := st.DeleteUser(ctx, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除 err = %v, want ErrNotFound", err)
	}
}

func TestTodoCRUDFiltersAndIsolation(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	alice := mustUser(t, st, "alice", RoleUser)
	bob := mustUser(t, st, "bob", RoleUser)

	urgent, err := st.CreateTodo(ctx, Todo{
		UserID:   alice.ID,
		Title:    "写周报",
		Notes:    "周五前",
		Quadrant: QuadrantUrgentImportant,
		Status:   StatusPending,
		Progress: 0,
	})
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if urgent.CreatedAt.IsZero() || urgent.CompletedAt != nil {
		t.Fatalf("unexpected todo: %+v", urgent)
	}
	doneTime := time.Now().UTC().Add(-time.Hour)
	second, err := st.CreateTodo(ctx, Todo{
		UserID:      alice.ID,
		Title:       "买牛奶",
		Quadrant:    QuadrantUrgentNotImportant,
		Status:      StatusDone,
		Progress:    100,
		CompletedAt: &doneTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.CompletedAt == nil {
		t.Fatal("完成时间应被保存")
	}

	all, err := st.ListTodos(ctx, alice.ID, TodoFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}
	// 默认按创建时间倒序。
	if all[0].ID != second.ID {
		t.Fatalf("排序异常: %+v", all)
	}
	done, err := st.ListTodos(ctx, alice.ID, TodoFilter{Status: StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0].ID != second.ID {
		t.Fatalf("status 过滤失败: %+v", done)
	}
	byQuadrant, err := st.ListTodos(ctx, alice.ID, TodoFilter{Quadrant: QuadrantUrgentImportant})
	if err != nil {
		t.Fatal(err)
	}
	if len(byQuadrant) != 1 || byQuadrant[0].ID != urgent.ID {
		t.Fatalf("quadrant 过滤失败: %+v", byQuadrant)
	}

	urgent.Status = StatusInProgress
	urgent.Progress = 40
	if err := st.UpdateTodo(ctx, urgent); err != nil {
		t.Fatal(err)
	}
	reloaded, err := st.GetTodo(ctx, alice.ID, urgent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != StatusInProgress || reloaded.Progress != 40 {
		t.Fatalf("更新未生效: %+v", reloaded)
	}

	if _, err := st.GetTodo(ctx, bob.ID, urgent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨用户读取 err = %v, want ErrNotFound", err)
	}
	if err := st.UpdateTodo(ctx, Todo{ID: urgent.ID, UserID: bob.ID, Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨用户更新 err = %v, want ErrNotFound", err)
	}
	if err := st.DeleteTodo(ctx, bob.ID, urgent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨用户删除 err = %v, want ErrNotFound", err)
	}
	if err := st.DeleteTodo(ctx, alice.ID, urgent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTodo(ctx, alice.ID, urgent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 err = %v", err)
	}
}

func TestTodoListTitleTimeAndDoneFilters(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	alice := mustUser(t, st, "alice", RoleUser)

	mk := func(title, status string, createdAt time.Time) Todo {
		t.Helper()
		todo, err := st.CreateTodo(ctx, Todo{
			UserID:   alice.ID,
			Title:    title,
			Quadrant: QuadrantUrgentImportant,
			Status:   status,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `UPDATE todos SET created_at = ? WHERE id = ?`,
			createdAt.UTC().UnixMilli(), todo.ID); err != nil {
			t.Fatal(err)
		}
		todo.CreatedAt = createdAt.UTC()
		return todo
	}
	early := mk("写周报", StatusPending, time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	mk("买牛奶", StatusDone, time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC))
	late := mk("写文档 review", StatusPending, time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC))

	ids := func(todos []Todo) []int64 {
		out := make([]int64, 0, len(todos))
		for _, todo := range todos {
			out = append(out, todo.ID)
		}
		return out
	}

	byTitle, err := st.ListTodos(ctx, alice.ID, TodoFilter{Title: "写"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byTitle) != 2 || byTitle[0].ID != late.ID || byTitle[1].ID != early.ID {
		t.Fatalf("title 过滤失败: %+v", byTitle)
	}
	// 标题匹配不区分 ASCII 大小写。
	byCase, err := st.ListTodos(ctx, alice.ID, TodoFilter{Title: "REVIEW"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byCase) != 1 || byCase[0].ID != late.ID {
		t.Fatalf("title 大小写过滤失败: %+v", byCase)
	}

	from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	inRange, err := st.ListTodos(ctx, alice.ID, TodoFilter{CreatedFrom: &from, CreatedTo: &to})
	if err != nil {
		t.Fatal(err)
	}
	if len(inRange) != 1 || inRange[0].Status != StatusDone {
		t.Fatalf("时间区间过滤失败: %+v", inRange)
	}
	// 边界值应包含在内。
	boundary := early.CreatedAt
	atStart, err := st.ListTodos(ctx, alice.ID, TodoFilter{CreatedFrom: &boundary})
	if err != nil {
		t.Fatal(err)
	}
	if len(atStart) != 3 {
		t.Fatalf("起始边界应包含, got %v", ids(atStart))
	}

	notDone, err := st.ListTodos(ctx, alice.ID, TodoFilter{ExcludeDone: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(notDone) != 2 {
		t.Fatalf("排除已完成失败: %v", ids(notDone))
	}
	for _, todo := range notDone {
		if todo.Status == StatusDone {
			t.Fatalf("结果不应包含已完成待办: %+v", todo)
		}
	}
}

func TestDeleteUserCascadesTodos(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	carol := mustUser(t, st, "carol", RoleUser)
	todo, err := st.CreateTodo(ctx, Todo{
		UserID:   carol.ID,
		Title:    "临时任务",
		Quadrant: QuadrantNotUrgentNotImportant,
		Status:   StatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(ctx, carol.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTodo(ctx, carol.ID, todo.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("用户删除后待办应级联删除, err = %v", err)
	}
}

func TestRefreshTokenLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "dave", RoleUser)
	expires := time.Now().UTC().Add(24 * time.Hour)
	if err := st.CreateRefreshToken(ctx, user.ID, "hash-1", expires); err != nil {
		t.Fatal(err)
	}
	rt, err := st.GetRefreshToken(ctx, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.UserID != user.ID || rt.RevokedAt != nil {
		t.Fatalf("unexpected token: %+v", rt)
	}
	if err := st.RevokeRefreshToken(ctx, "hash-1"); err != nil {
		t.Fatal(err)
	}
	rt, err = st.GetRefreshToken(ctx, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.RevokedAt == nil {
		t.Fatal("撤销时间应被记录")
	}
	if err := st.RevokeRefreshToken(ctx, "hash-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复撤销 err = %v, want ErrNotFound", err)
	}
	if _, err := st.GetRefreshToken(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	if err := st.CreateRefreshToken(ctx, user.ID, "hash-2", expires); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeUserRefreshTokens(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	rt, err = st.GetRefreshToken(ctx, "hash-2")
	if err != nil {
		t.Fatal(err)
	}
	if rt.RevokedAt == nil {
		t.Fatal("批量撤销未生效")
	}
	if err := st.DeleteExpiredRefreshTokens(ctx, time.Now().UTC().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRefreshToken(ctx, "hash-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("清理后 err = %v", err)
	}
}

func TestMeta(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.GetMeta(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := st.SetMeta(ctx, "k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMeta(ctx, "k", "v2"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetMeta(ctx, "k")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v2" {
		t.Fatalf("value = %q, want v2", got)
	}
}
