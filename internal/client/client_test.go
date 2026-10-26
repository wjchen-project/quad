package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"todo_helper/internal/config"
)

type fakeAPI struct {
	mu           sync.Mutex
	access       string
	refresh      string
	refreshFails bool
	logoutHit    bool
}

func (f *fakeAPI) issue(access, refresh string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.access = access
	f.refresh = refresh
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/auth/login":
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Username != "alice" || req.Password != "secret-123" {
			writeAPIError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
			return
		}
		f.issue("access-1", "refresh-1")
		f.writeTokens(w)
	case "/api/v1/auth/refresh":
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		ok := !f.refreshFails && req.RefreshToken == f.refresh
		f.mu.Unlock()
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "invalid_refresh_token", "刷新令牌已失效，请重新登录")
			return
		}
		f.issue("access-2", "refresh-2")
		f.writeTokens(w)
	case "/api/v1/auth/logout":
		f.mu.Lock()
		f.logoutHit = true
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case "/api/v1/todos":
		f.mu.Lock()
		access := f.access
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+access {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "访问令牌无效或已过期")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"todos":[]}`))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAPI) writeTokens(w http.ResponseWriter) {
	f.mu.Lock()
	access, refresh := f.access, f.refresh
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"expires_in":    60,
		"user":          map[string]any{"id": 1, "username": "alice", "role": "admin"},
	})
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func newClientHarness(t *testing.T) (*Client, *fakeAPI, *httptest.Server) {
	t.Helper()
	t.Setenv("TODO_HELPER_HOME", t.TempDir())
	fake := &fakeAPI{}
	ts := httptest.NewServer(fake)
	t.Cleanup(ts.Close)
	c, err := New(ts.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, fake, ts
}

func TestLoginPersistsSession(t *testing.T) {
	c, _, _ := newClientHarness(t)
	user, err := c.Login(context.Background(), "alice", "secret-123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if user.Username != "alice" || user.Role != "admin" {
		t.Fatalf("user = %+v", user)
	}
	if !c.LoggedIn() {
		t.Fatal("登录后应处于已登录状态")
	}
	data, err := os.ReadFile(c.SessionPath())
	if err != nil {
		t.Fatalf("登录态文件未写入: %v", err)
	}
	var saved Session
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken != "refresh-1" || saved.AccessToken != "access-1" {
		t.Fatalf("saved session = %+v", saved)
	}
	if saved.Server == "" || saved.Username != "alice" {
		t.Fatalf("saved session = %+v", saved)
	}

	// 新进程应能加载登录态。
	reloaded, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Username() != "alice" || !reloaded.LoggedIn() {
		t.Fatalf("reloaded = %+v", reloaded.session)
	}
}

func TestLoginFailureDoesNotPersist(t *testing.T) {
	c, _, _ := newClientHarness(t)
	_, err := c.Login(context.Background(), "alice", "wrong-password")
	if err == nil {
		t.Fatal("错误密码应登录失败")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v", err)
	}
	if c.LoggedIn() {
		t.Fatal("登录失败不应写入登录态")
	}
	if _, statErr := os.Stat(c.SessionPath()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("登录态文件不应存在, err = %v", statErr)
	}
}

func TestAutoRefreshOnUnauthorized(t *testing.T) {
	c, fake, _ := newClientHarness(t)
	if _, err := c.Login(context.Background(), "alice", "secret-123"); err != nil {
		t.Fatal(err)
	}
	// 让当前 access token 失效，强制走刷新流程。
	fake.issue("invalid-access", "refresh-1")
	if _, err := c.ListTodos(context.Background(), TodoQuery{}); err != nil {
		t.Fatalf("ListTodos 应自动刷新后成功: %v", err)
	}
	if c.session.AccessToken != "access-2" || c.session.RefreshToken != "refresh-2" {
		t.Fatalf("session 未更新: %+v", c.session)
	}
	data, err := os.ReadFile(c.SessionPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved Session
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken != "refresh-2" {
		t.Fatalf("刷新后的令牌未落盘: %+v", saved)
	}
}

func TestRefreshFailureClearsSession(t *testing.T) {
	c, fake, _ := newClientHarness(t)
	if _, err := c.Login(context.Background(), "alice", "secret-123"); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.refreshFails = true
	fake.access = "invalid-access"
	fake.mu.Unlock()
	if _, err := c.ListTodos(context.Background(), TodoQuery{}); !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired", err)
	}
	if c.LoggedIn() {
		t.Fatal("刷新失败后应清除登录态")
	}
	if _, err := os.Stat(c.SessionPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("登录态文件应被删除, err = %v", err)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	c, fake, _ := newClientHarness(t)
	if _, err := c.Login(context.Background(), "alice", "secret-123"); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !fake.logoutHit {
		t.Fatal("应调用服务端注销接口")
	}
	if c.LoggedIn() {
		t.Fatal("注销后应清除登录态")
	}
	if _, err := os.Stat(c.SessionPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("注销后登录态文件应被删除, err = %v", err)
	}
}

func TestCorruptSessionIsTreatedAsLoggedOut(t *testing.T) {
	t.Setenv("TODO_HELPER_HOME", t.TempDir())
	path, err := config.SessionPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(&fakeAPI{})
	defer ts.Close()
	c, err := New(ts.URL)
	if err != nil {
		t.Fatalf("损坏的登录态不应阻塞客户端初始化: %v", err)
	}
	if c.LoggedIn() {
		t.Fatal("损坏的登录态不应被视为已登录")
	}
}

func TestNormalizeServer(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8787":      "http://127.0.0.1:8787",
		"http://example.com/": "http://example.com",
		" https://a.b/c/ ":    "https://a.b/c",
	}
	for input, want := range cases {
		if got := normalizeServer(input); got != want {
			t.Fatalf("normalizeServer(%q) = %q, want %q", input, got, want)
		}
	}
}
