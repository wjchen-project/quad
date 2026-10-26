package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"todo_helper/internal/auth"
	"todo_helper/internal/store"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	AccessToken      string     `json:"access_token"`
	TokenType        string     `json:"token_type"`
	ExpiresIn        int        `json:"expires_in"`
	RefreshToken     string     `json:"refresh_token"`
	RefreshExpiresIn int        `json:"refresh_expires_in"`
	User             store.User `json:"user"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "用户名和密码不能为空")
		return
	}
	user, hash, err := s.store.GetUserAuthByUsername(r.Context(), req.Username)
	if errors.Is(err, store.ErrNotFound) {
		// 用户不存在时也执行一次哈希校验，避免通过响应时间枚举用户名。
		_, _ = auth.VerifyPassword(s.dummyHash, req.Password)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	}
	if err != nil {
		s.log.Error("登录查询失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	ok, err := auth.VerifyPassword(hash, req.Password)
	if err != nil {
		s.log.Error("密码哈希校验失败", "user", user.Username, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	}
	// 顺手清理过期 / 已撤销的刷新令牌，失败不影响登录。
	_ = s.store.DeleteExpiredRefreshTokens(r.Context(), s.now())
	resp, err := s.issueTokens(r, user)
	if err != nil {
		s.log.Error("签发令牌失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "refresh_token 不能为空")
		return
	}
	hash := auth.HashRefreshToken(req.RefreshToken)
	rt, err := s.store.GetRefreshToken(r.Context(), hash)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token", "刷新令牌无效，请重新登录")
		return
	}
	if err != nil {
		s.log.Error("查询刷新令牌失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	if rt.RevokedAt != nil {
		// 已撤销的令牌被再次使用，视为可能的重放：撤销该用户全部刷新令牌。
		_ = s.store.RevokeUserRefreshTokens(r.Context(), rt.UserID)
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token", "刷新令牌已失效，请重新登录")
		return
	}
	if !s.now().Before(rt.ExpiresAt) {
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token", "刷新令牌已过期，请重新登录")
		return
	}
	user, err := s.store.GetUserByID(r.Context(), rt.UserID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token", "用户不存在，请重新登录")
		return
	}
	if err != nil {
		s.log.Error("查询刷新令牌用户失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	// 轮换：旧令牌立即失效，每次刷新都返回新的 access/refresh 对。
	if err := s.store.RevokeRefreshToken(r.Context(), hash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid_refresh_token", "刷新令牌已失效，请重新登录")
			return
		}
		s.log.Error("撤销刷新令牌失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	resp, err := s.issueTokens(r, user)
	if err != nil {
		s.log.Error("签发令牌失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if token := strings.TrimSpace(req.RefreshToken); token != "" {
		hash := auth.HashRefreshToken(token)
		if rt, err := s.store.GetRefreshToken(r.Context(), hash); err == nil && rt.UserID == userFrom(r.Context()).ID {
			_ = s.store.RevokeRefreshToken(r.Context(), hash)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r.Context()))
}

func (s *Server) issueTokens(r *http.Request, user store.User) (tokenResponse, error) {
	now := s.now().UTC()
	accessToken, err := s.signer.Sign(auth.Claims{
		Subject:   user.ID,
		Username:  user.Username,
		Role:      user.Role,
		ExpiresAt: now.Add(s.accessTTL),
	})
	if err != nil {
		return tokenResponse{}, err
	}
	refreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		return tokenResponse{}, err
	}
	if err := s.store.CreateRefreshToken(r.Context(), user.ID, auth.HashRefreshToken(refreshToken), now.Add(s.refreshTTL)); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{
		AccessToken:      accessToken,
		TokenType:        "Bearer",
		ExpiresIn:        int(s.accessTTL.Seconds()),
		RefreshToken:     refreshToken,
		RefreshExpiresIn: int(s.refreshTTL.Seconds()),
		User:             user,
	}, nil
}
