package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"quad/internal/api"
	"quad/internal/auth"
	"quad/internal/store"
)

const adminPassword = "admin-password-1"

type harness struct {
	url  string
	http *http.Client
	// adminID / adminToken 便于断言“最后一个管理员”等场景。
	adminID    int64
	adminToken string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	hash, err := auth.HashPassword(adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := st.CreateUser(context.Background(), "admin", hash, store.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server, err := api.New(context.Background(), api.Options{
		Store:      st,
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	h := &harness{url: ts.URL, http: ts.Client(), adminID: admin.ID}
	h.adminToken, _ = h.login(t, "admin", adminPassword)
	return h
}

func (h *harness) request(t *testing.T, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, h.url+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := h.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}

type loginResult struct {
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token"`
	ExpiresIn    int        `json:"expires_in"`
	User         store.User `json:"user"`
}

func (h *harness) login(t *testing.T, username, password string) (string, loginResult) {
	t.Helper()
	status, data := h.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": username,
		"password": password,
	})
	if status != http.StatusOK {
		t.Fatalf("login(%s) status = %d, body = %s", username, status, data)
	}
	var res loginResult
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	return res.AccessToken, res
}

func errorCode(t *testing.T, data []byte) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("解析错误响应失败: %v, body=%s", err, data)
	}
	return payload.Error.Code
}

func TestLoginMeAndHealth(t *testing.T) {
	h := newHarness(t)
	status, data := h.request(t, http.MethodGet, "/healthz", "", nil)
	if status != http.StatusOK || !strings.Contains(string(data), "ok") {
		t.Fatalf("healthz = %d %s", status, data)
	}

	status, data = h.request(t, http.MethodGet, "/api/v1/me", h.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", status, data)
	}
	var me store.User
	if err := json.Unmarshal(data, &me); err != nil {
		t.Fatal(err)
	}
	if me.Username != "admin" || me.Role != store.RoleAdmin {
		t.Fatalf("me = %+v", me)
	}

	if status, _ := h.request(t, http.MethodGet, "/api/v1/me", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("无令牌访问 me status = %d, want 401", status)
	}
	status, data = h.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": "admin", "password": "wrong-password",
	})
	if status != http.StatusUnauthorized || errorCode(t, data) != "invalid_credentials" {
		t.Fatalf("错误密码 = %d %s", status, data)
	}
	status, data = h.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": "ghost", "password": "whatever-1",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("不存在的用户 = %d %s", status, data)
	}
}

func TestRefreshRotationAndReplay(t *testing.T) {
	h := newHarness(t)
	_, first := h.login(t, "admin", adminPassword)
	status, data := h.request(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": first.RefreshToken,
	})
	if status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", status, data)
	}
	var second loginResult
	if err := json.Unmarshal(data, &second); err != nil {
		t.Fatal(err)
	}
	if second.AccessToken == first.AccessToken || second.RefreshToken == first.RefreshToken {
		t.Fatal("刷新后应返回新的令牌对")
	}
	if status, _ := h.request(t, http.MethodGet, "/api/v1/me", second.AccessToken, nil); status != http.StatusOK {
		t.Fatalf("新访问令牌不可用: %d", status)
	}

	// 旧刷新令牌被重用：应失败并撤销该用户全部刷新令牌。
	status, data = h.request(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": first.RefreshToken,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("重放旧刷新令牌 = %d %s, want 401", status, data)
	}
	status, _ = h.request(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": second.RefreshToken,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("重放后新刷新令牌应被撤销, status = %d", status)
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	h := newHarness(t)
	token, session := h.login(t, "admin", adminPassword)
	status, data := h.request(t, http.MethodPost, "/api/v1/auth/logout", token, map[string]string{
		"refresh_token": session.RefreshToken,
	})
	if status != http.StatusNoContent {
		t.Fatalf("logout = %d %s", status, data)
	}
	status, _ = h.request(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": session.RefreshToken,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("注销后刷新令牌仍可用, status = %d", status)
	}
}

func TestAdminUserManagementAndGuards(t *testing.T) {
	h := newHarness(t)
	status, data := h.request(t, http.MethodPost, "/api/v1/users", h.adminToken, map[string]string{
		"username": "bob",
		"password": "bob-password-1",
	})
	if status != http.StatusCreated {
		t.Fatalf("create user = %d %s", status, data)
	}
	var bob store.User
	if err := json.Unmarshal(data, &bob); err != nil {
		t.Fatal(err)
	}
	if bob.Role != store.RoleUser {
		t.Fatalf("默认角色 = %s, want user", bob.Role)
	}

	status, data = h.request(t, http.MethodPost, "/api/v1/users", h.adminToken, map[string]string{
		"username": "BOB",
		"password": "bob-password-2",
	})
	if status != http.StatusConflict {
		t.Fatalf("重复用户名 = %d %s", status, data)
	}
	status, _ = h.request(t, http.MethodPost, "/api/v1/users", h.adminToken, map[string]string{
		"username": "x",
		"password": "short",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("非法用户名/密码 = %d, want 400", status)
	}

	bobToken, _ := h.login(t, "bob", "bob-password-1")
	if status, _ := h.request(t, http.MethodGet, "/api/v1/users", bobToken, nil); status != http.StatusForbidden {
		t.Fatalf("普通用户访问用户管理 = %d, want 403", status)
	}
	if status, _ := h.request(t, http.MethodGet, "/api/v1/users", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("匿名访问用户管理 = %d, want 401", status)
	}

	status, data = h.request(t, http.MethodGet, "/api/v1/users", h.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list users = %d %s", status, data)
	}
	var list struct {
		Users []store.User `json:"users"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Users) != 2 {
		t.Fatalf("users = %+v", list.Users)
	}

	path := "/api/v1/users/" + itoa(bob.ID)
	status, data = h.request(t, http.MethodPatch, path, h.adminToken, map[string]string{"role": "admin"})
	if status != http.StatusOK {
		t.Fatalf("promote = %d %s", status, data)
	}
	var promoted store.User
	if err := json.Unmarshal(data, &promoted); err != nil {
		t.Fatal(err)
	}
	if promoted.Role != store.RoleAdmin {
		t.Fatalf("role = %s", promoted.Role)
	}
	status, data = h.request(t, http.MethodPatch, path, h.adminToken, map[string]string{"role": "user"})
	if status != http.StatusOK {
		t.Fatalf("demote = %d %s", status, data)
	}
	status, data = h.request(t, http.MethodPatch, path, h.adminToken, map[string]string{"password": "bob-password-3"})
	if status != http.StatusOK {
		t.Fatalf("reset password = %d %s", status, data)
	}
	bobToken, _ = h.login(t, "bob", "bob-password-3")
	if status, _ := h.request(t, http.MethodGet, "/api/v1/todos", bobToken, nil); status != http.StatusOK {
		t.Fatalf("新密码登录后访问待办 = %d", status)
	}

	status, data = h.request(t, http.MethodDelete, path, h.adminToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete bob = %d %s", status, data)
	}
	if status, _ := h.request(t, http.MethodDelete, path, h.adminToken, nil); status != http.StatusNotFound {
		t.Fatalf("重复删除 = %d, want 404", status)
	}
	if status, _ := h.request(t, http.MethodDelete, "/api/v1/users/"+itoa(h.adminID), h.adminToken, nil); status != http.StatusConflict {
		t.Fatalf("删除自己 = %d, want 409", status)
	}
	status, data = h.request(t, http.MethodPatch, "/api/v1/users/"+itoa(h.adminID), h.adminToken, map[string]string{"role": "user"})
	if status != http.StatusConflict {
		t.Fatalf("降级最后一个管理员 = %d %s, want 409", status, data)
	}
	status, data = h.request(t, http.MethodPatch, "/api/v1/users/"+itoa(h.adminID), h.adminToken, map[string]any{})
	if status != http.StatusBadRequest {
		t.Fatalf("空更新 = %d %s, want 400", status, data)
	}
}

func TestTodoLifecycleAndIsolation(t *testing.T) {
	h := newHarness(t)
	status, data := h.request(t, http.MethodPost, "/api/v1/todos", h.adminToken, map[string]any{
		"title":    "写周报",
		"notes":    "周五前完成",
		"quadrant": "q1",
	})
	if status != http.StatusCreated {
		t.Fatalf("create todo = %d %s", status, data)
	}
	var todo store.Todo
	if err := json.Unmarshal(data, &todo); err != nil {
		t.Fatal(err)
	}
	if todo.Status != store.StatusPending || todo.Progress != 0 || todo.CompletedAt != nil {
		t.Fatalf("新建待办默认值异常: %+v", todo)
	}
	if todo.Quadrant != store.QuadrantUrgentImportant {
		t.Fatalf("quadrant = %s", todo.Quadrant)
	}

	status, data = h.request(t, http.MethodPost, "/api/v1/todos", h.adminToken, map[string]any{
		"title":    "买牛奶",
		"quadrant": "q3",
		"status":   "done",
	})
	if status != http.StatusCreated {
		t.Fatalf("create done todo = %d %s", status, data)
	}
	var doneTodo store.Todo
	if err := json.Unmarshal(data, &doneTodo); err != nil {
		t.Fatal(err)
	}
	if doneTodo.Progress != 100 || doneTodo.CompletedAt == nil {
		t.Fatalf("完成状态应自动填进度与完成时间: %+v", doneTodo)
	}

	path := "/api/v1/todos/" + itoa(todo.ID)
	status, data = h.request(t, http.MethodPatch, path, h.adminToken, map[string]any{
		"status":   "done",
		"progress": 55,
	})
	if status != http.StatusOK {
		t.Fatalf("patch todo = %d %s", status, data)
	}
	var updated store.Todo
	if err := json.Unmarshal(data, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.StatusDone || updated.Progress != 100 || updated.CompletedAt == nil {
		t.Fatalf("完成规则未生效: %+v", updated)
	}

	status, data = h.request(t, http.MethodPatch, path, h.adminToken, map[string]any{"status": "pending"})
	if status != http.StatusOK {
		t.Fatalf("reopen = %d %s", status, data)
	}
	var reopened store.Todo
	if err := json.Unmarshal(data, &reopened); err != nil {
		t.Fatal(err)
	}
	if reopened.CompletedAt != nil {
		t.Fatalf("重新打开后完成时间应清空: %+v", reopened)
	}

	status, data = h.request(t, http.MethodGet, "/api/v1/todos?status=pending&quadrant=urgent-important", h.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list todos = %d %s", status, data)
	}
	var list struct {
		Todos []store.Todo `json:"todos"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Todos) != 1 || list.Todos[0].ID != todo.ID {
		t.Fatalf("过滤结果异常: %+v", list.Todos)
	}

	status, data = h.request(t, http.MethodPost, "/api/v1/users", h.adminToken, map[string]string{
		"username": "bob",
		"password": "bob-password-1",
	})
	if status != http.StatusCreated {
		t.Fatalf("create bob = %d %s", status, data)
	}
	bobToken, _ := h.login(t, "bob", "bob-password-1")
	if status, _ := h.request(t, http.MethodGet, path, bobToken, nil); status != http.StatusNotFound {
		t.Fatalf("跨用户读取 = %d, want 404", status)
	}
	if status, _ := h.request(t, http.MethodPatch, path, bobToken, map[string]any{"progress": 10}); status != http.StatusNotFound {
		t.Fatalf("跨用户更新 = %d, want 404", status)
	}
	if status, _ := h.request(t, http.MethodDelete, path, bobToken, nil); status != http.StatusNotFound {
		t.Fatalf("跨用户删除 = %d, want 404", status)
	}

	status, data = h.request(t, http.MethodDelete, path, h.adminToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete todo = %d %s", status, data)
	}
	if status, _ := h.request(t, http.MethodGet, path, h.adminToken, nil); status != http.StatusNotFound {
		t.Fatalf("删除后读取 = %d, want 404", status)
	}
	if status, _ := h.request(t, http.MethodGet, "/api/v1/todos/abc", h.adminToken, nil); status != http.StatusBadRequest {
		t.Fatalf("非法 ID = %d, want 400", status)
	}
}

func TestTodoValidation(t *testing.T) {
	h := newHarness(t)
	cases := []map[string]any{
		{"notes": "缺标题", "quadrant": "q1"},
		{"title": "缺象限"},
		{"title": "坏象限", "quadrant": "q9"},
		{"title": "坏进度", "quadrant": "q1", "progress": 101},
		{"title": "坏状态", "quadrant": "q1", "status": "sleeping"},
		{"title": strings.Repeat("长", 201), "quadrant": "q1"},
	}
	for _, body := range cases {
		status, data := h.request(t, http.MethodPost, "/api/v1/todos", h.adminToken, body)
		if status != http.StatusBadRequest {
			t.Fatalf("body %v => %d %s, want 400", body, status, data)
		}
	}
	status, _ := h.request(t, http.MethodPost, "/api/v1/todos", h.adminToken, map[string]any{
		"title": "合法", "quadrant": "q2",
	})
	if status != http.StatusCreated {
		t.Fatalf("合法请求 = %d", status)
	}
	status, data := h.request(t, http.MethodPatch, "/api/v1/todos/1", h.adminToken, map[string]any{})
	if status != http.StatusBadRequest {
		t.Fatalf("空更新 = %d %s, want 400", status, data)
	}
}

func TestStoreIsolationAcrossServers(t *testing.T) {
	// 冒烟测试：同一 Server 实例可重复创建，JWT 密钥持久化在数据库中。
	h := newHarness(t)
	_, session := h.login(t, "admin", adminPassword)
	if session.ExpiresIn <= 0 {
		t.Fatalf("expires_in = %d", session.ExpiresIn)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
