// Package api 实现 HTTP 接口：认证、用户管理（管理员）与待办管理。
//
// 作者: wjchen
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"todo_helper/internal/auth"
	"todo_helper/internal/bootstrap"
	"todo_helper/internal/store"
)

// Options 是构建 Server 所需的依赖。
type Options struct {
	Store      *store.Store
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Logger     *slog.Logger
	// Now 仅供测试替换时间源。
	Now func() time.Time
}

// Server 持有路由与鉴权依赖。
type Server struct {
	store      *store.Store
	signer     *auth.Signer
	accessTTL  time.Duration
	refreshTTL time.Duration
	log        *slog.Logger
	now        func() time.Time
	dummyHash  string
	handler    http.Handler
}

type ctxKey int

const userKey ctxKey = iota

var errUnauthenticated = errors.New("未认证")

// New 注册路由并确保 JWT 密钥存在。
func New(ctx context.Context, opts Options) (*Server, error) {
	if opts.Store == nil {
		return nil, errors.New("api: store 不能为空")
	}
	if opts.AccessTTL <= 0 || opts.RefreshTTL <= 0 {
		return nil, errors.New("api: 令牌有效期必须大于 0")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	secret, err := bootstrap.EnsureJWTSecret(ctx, opts.Store)
	if err != nil {
		return nil, err
	}
	signer, err := auth.NewSigner(secret)
	if err != nil {
		return nil, err
	}
	dummy, err := auth.HashPassword("timing-dummy-password")
	if err != nil {
		return nil, err
	}
	s := &Server{
		store:      opts.Store,
		signer:     signer,
		accessTTL:  opts.AccessTTL,
		refreshTTL: opts.RefreshTTL,
		log:        logger,
		now:        now,
		dummyHash:  dummy,
	}
	s.handler = s.recoverer(s.logRequests(s.routes()))
	return s, nil
}

// Handler 返回完整的 HTTP 处理器。
func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", s.handleRefresh)
	mux.Handle("POST /api/v1/auth/logout", s.authenticated(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /api/v1/me", s.authenticated(http.HandlerFunc(s.handleMe)))

	mux.Handle("GET /api/v1/users", s.adminOnly(http.HandlerFunc(s.handleListUsers)))
	mux.Handle("POST /api/v1/users", s.adminOnly(http.HandlerFunc(s.handleCreateUser)))
	mux.Handle("PATCH /api/v1/users/{id}", s.adminOnly(http.HandlerFunc(s.handleUpdateUser)))
	mux.Handle("DELETE /api/v1/users/{id}", s.adminOnly(http.HandlerFunc(s.handleDeleteUser)))

	mux.Handle("GET /api/v1/todos", s.authenticated(http.HandlerFunc(s.handleListTodos)))
	mux.Handle("POST /api/v1/todos", s.authenticated(http.HandlerFunc(s.handleCreateTodo)))
	mux.Handle("GET /api/v1/todos/{id}", s.authenticated(http.HandlerFunc(s.handleGetTodo)))
	mux.Handle("PATCH /api/v1/todos/{id}", s.authenticated(http.HandlerFunc(s.handleUpdateTodo)))
	mux.Handle("DELETE /api/v1/todos/{id}", s.authenticated(http.HandlerFunc(s.handleDeleteTodo)))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticate(r)
		if err != nil {
			if errors.Is(err, errUnauthenticated) {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "访问令牌无效或已过期，请重新登录")
				return
			}
			s.log.Error("认证查询失败", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) adminOnly(next http.Handler) http.Handler {
	return s.authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r.Context()).Role != store.RoleAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (s *Server) authenticate(r *http.Request) (store.User, error) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return store.User{}, errUnauthenticated
	}
	claims, err := s.signer.Verify(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
	if err != nil {
		return store.User{}, errUnauthenticated
	}
	user, err := s.store.GetUserByID(r.Context(), claims.Subject)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, errUnauthenticated
	}
	if err != nil {
		return store.User{}, err
	}
	return user, nil
}

func userFrom(ctx context.Context) store.User {
	user, _ := ctx.Value(userKey).(store.User)
	return user
}

// ---- 请求 / 响应辅助 ----

const maxBodyBytes = 1 << 20

type errorPayload struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var payload errorPayload
	payload.Error.Code = code
	payload.Error.Message = message
	writeJSON(w, status, payload)
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("请求体不是合法 JSON: %w", err)
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("无效的 ID")
	}
	return id, nil
}

// ---- 中间件 ----

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).Round(time.Millisecond).String(),
			"remote", r.RemoteAddr,
		)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("请求处理 panic", "error", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
