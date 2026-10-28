// Package client 封装 CLI 对 HTTP API 的调用与本地登录态持久化。
//
// 作者: wjchen
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"quad/internal/config"
	"quad/internal/store"
)

// ErrLoginRequired 表示本地没有可用登录态，或刷新令牌已失效。
var ErrLoginRequired = errors.New("未登录或登录已过期，请先执行登录命令")

// APIError 是服务端返回的结构化错误。
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("服务端返回 HTTP %d", e.StatusCode)
}

// Session 是保存在用户目录下的登录态。
type Session struct {
	Server       string    `json:"server"`
	Username     string    `json:"username"`
	Role         string    `json:"role"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Client 是带自动刷新能力的 API 客户端。
type Client struct {
	http        *http.Client
	session     Session
	sessionPath string
}

type tokenResponse struct {
	AccessToken      string     `json:"access_token"`
	ExpiresIn        int        `json:"expires_in"`
	RefreshToken     string     `json:"refresh_token"`
	RefreshExpiresIn int        `json:"refresh_expires_in"`
	User             store.User `json:"user"`
}

// DefaultServer 与服务端默认监听地址一致。
const DefaultServer = "http://" + config.DefaultAddr

// New 读取本地登录态；server 非空时覆盖已保存的服务器地址。
func New(server string) (*Client, error) {
	path, err := config.SessionPath()
	if err != nil {
		return nil, err
	}
	c := &Client{
		http:        &http.Client{Timeout: 30 * time.Second},
		sessionPath: path,
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &c.session); err != nil {
			// 登录态损坏时按未登录处理，避免用户被卡住；下次登录会覆盖该文件。
			c.session = Session{}
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("client: 读取登录态文件失败: %w", err)
	}
	if strings.TrimSpace(server) != "" {
		c.session.Server = normalizeServer(server)
	}
	if c.session.Server == "" {
		c.session.Server = DefaultServer
	}
	return c, nil
}

// Server 返回当前服务器地址。
func (c *Client) Server() string {
	return c.session.Server
}

// Username 返回本地记录的登录用户名。
func (c *Client) Username() string {
	return c.session.Username
}

// SessionPath 返回登录态文件路径。
func (c *Client) SessionPath() string {
	return c.sessionPath
}

// LoggedIn 表示本地存在访问令牌或刷新令牌。
func (c *Client) LoggedIn() bool {
	return c.session.AccessToken != "" || c.session.RefreshToken != ""
}

// Login 登录并保存令牌。
func (c *Client) Login(ctx context.Context, username, password string) (store.User, error) {
	var resp tokenResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, &resp, false)
	if err != nil {
		return store.User{}, err
	}
	c.session.AccessToken = resp.AccessToken
	c.session.RefreshToken = resp.RefreshToken
	c.session.Username = resp.User.Username
	c.session.Role = resp.User.Role
	if err := c.save(); err != nil {
		return store.User{}, err
	}
	return resp.User, nil
}

// Logout 尽力撤销服务端刷新令牌，并清除本地登录态。
func (c *Client) Logout(ctx context.Context) error {
	var apiErr error
	if c.session.RefreshToken != "" {
		apiErr = c.do(ctx, http.MethodPost, "/api/v1/auth/logout", map[string]string{
			"refresh_token": c.session.RefreshToken,
		}, nil, true)
	}
	if err := c.clear(); err != nil {
		return err
	}
	return apiErr
}

// Me 返回当前登录用户（服务端实时数据）。
func (c *Client) Me(ctx context.Context) (store.User, error) {
	var user store.User
	err := c.do(ctx, http.MethodGet, "/api/v1/me", nil, &user, true)
	return user, err
}

// ListUsers 列出全部用户，仅管理员可用。
func (c *Client) ListUsers(ctx context.Context) ([]store.User, error) {
	var resp struct {
		Users []store.User `json:"users"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/users", nil, &resp, true)
	return resp.Users, err
}

// CreateUser 创建用户，仅管理员可用。
func (c *Client) CreateUser(ctx context.Context, username, password, role string) (store.User, error) {
	body := map[string]string{"username": username, "password": password}
	if role != "" {
		body["role"] = role
	}
	var user store.User
	err := c.do(ctx, http.MethodPost, "/api/v1/users", body, &user, true)
	return user, err
}

// UpdateUser 修改用户密码或角色，仅管理员可用。
func (c *Client) UpdateUser(ctx context.Context, id int64, password, role *string) (store.User, error) {
	body := map[string]any{}
	if password != nil {
		body["password"] = *password
	}
	if role != nil {
		body["role"] = *role
	}
	var user store.User
	err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", id), body, &user, true)
	return user, err
}

// DeleteUser 删除用户，仅管理员可用。
func (c *Client) DeleteUser(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", id), nil, nil, true)
}

// TodoQuery 是待办列表过滤条件。
type TodoQuery struct {
	Status      string
	Quadrant    string
	Title       string
	From        *time.Time
	To          *time.Time
	ExcludeDone bool
	Limit       int
	Offset      int
}

// ListTodos 返回当前用户的待办。
func (c *Client) ListTodos(ctx context.Context, q TodoQuery) ([]store.Todo, error) {
	values := url.Values{}
	if q.Status != "" {
		values.Set("status", q.Status)
	}
	if q.Quadrant != "" {
		values.Set("quadrant", q.Quadrant)
	}
	if q.Title != "" {
		values.Set("title", q.Title)
	}
	if q.From != nil {
		values.Set("from", q.From.UTC().Format(time.RFC3339Nano))
	}
	if q.To != nil {
		values.Set("to", q.To.UTC().Format(time.RFC3339Nano))
	}
	if q.ExcludeDone {
		values.Set("exclude_done", "true")
	}
	if q.Limit > 0 {
		values.Set("limit", fmt.Sprint(q.Limit))
	}
	if q.Offset > 0 {
		values.Set("offset", fmt.Sprint(q.Offset))
	}
	path := "/api/v1/todos"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var resp struct {
		Todos []store.Todo `json:"todos"`
	}
	err := c.do(ctx, http.MethodGet, path, nil, &resp, true)
	return resp.Todos, err
}

// CreateTodo 创建待办。
func (c *Client) CreateTodo(ctx context.Context, title, notes, quadrant, status string, progress *int) (store.Todo, error) {
	body := map[string]any{
		"title":    title,
		"notes":    notes,
		"quadrant": quadrant,
	}
	if status != "" {
		body["status"] = status
	}
	if progress != nil {
		body["progress"] = *progress
	}
	var todo store.Todo
	err := c.do(ctx, http.MethodPost, "/api/v1/todos", body, &todo, true)
	return todo, err
}

// GetTodo 按 ID 获取待办。
func (c *Client) GetTodo(ctx context.Context, id int64) (store.Todo, error) {
	var todo store.Todo
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/todos/%d", id), nil, &todo, true)
	return todo, err
}

// TodoPatch 是待办的部分更新字段，nil 表示不修改。
type TodoPatch struct {
	Title    *string `json:"title,omitempty"`
	Notes    *string `json:"notes,omitempty"`
	Quadrant *string `json:"quadrant,omitempty"`
	Status   *string `json:"status,omitempty"`
	Progress *int    `json:"progress,omitempty"`
}

// UpdateTodo 部分更新待办。
func (c *Client) UpdateTodo(ctx context.Context, id int64, patch TodoPatch) (store.Todo, error) {
	var todo store.Todo
	err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/todos/%d", id), patch, &todo, true)
	return todo, err
}

// DeleteTodo 删除待办。
func (c *Client) DeleteTodo(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/todos/%d", id), nil, nil, true)
}

// ---- 内部实现 ----

func (c *Client) do(ctx context.Context, method, path string, in, out any, allowRefresh bool) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.session.Server+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.session.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.session.AccessToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 %s 失败: %w", c.session.Server, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode == http.StatusUnauthorized && allowRefresh && c.session.RefreshToken != "" {
		if err := c.refresh(ctx); err == nil {
			return c.do(ctx, method, path, in, out, false)
		} else if isStatus(err, http.StatusUnauthorized) {
			_ = c.clear()
			return ErrLoginRequired
		} else {
			return err
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("client: 解析响应失败: %w", err)
			}
		}
		return nil
	}
	return toAPIError(resp.StatusCode, data)
}

func (c *Client) refresh(ctx context.Context) error {
	var resp tokenResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": c.session.RefreshToken,
	}, &resp, false)
	if err != nil {
		return err
	}
	c.session.AccessToken = resp.AccessToken
	c.session.RefreshToken = resp.RefreshToken
	c.session.Username = resp.User.Username
	c.session.Role = resp.User.Role
	return c.save()
}

func (c *Client) save() error {
	c.session.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(c.sessionPath), 0o700); err != nil {
		return fmt.Errorf("client: 创建配置目录失败: %w", err)
	}
	data, err := json.MarshalIndent(c.session, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(c.sessionPath), ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("client: 写入登录态失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("client: 设置登录态权限失败: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("client: 写入登录态失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("client: 关闭登录态文件失败: %w", err)
	}
	if err := os.Rename(tmpName, c.sessionPath); err != nil {
		return fmt.Errorf("client: 保存登录态失败: %w", err)
	}
	return nil
}

func (c *Client) clear() error {
	err := os.Remove(c.sessionPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("client: 清除登录态失败: %w", err)
	}
	c.session.AccessToken = ""
	c.session.RefreshToken = ""
	c.session.Username = ""
	c.session.Role = ""
	return nil
}

func normalizeServer(server string) string {
	server = strings.TrimSpace(server)
	server = strings.TrimRight(server, "/")
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	return server
}

func toAPIError(status int, data []byte) error {
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	apiErr := &APIError{StatusCode: status}
	if json.Unmarshal(data, &payload) == nil {
		apiErr.Code = payload.Error.Code
		apiErr.Message = payload.Error.Message
	}
	return apiErr
}

func isStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}
