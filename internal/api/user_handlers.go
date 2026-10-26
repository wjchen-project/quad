package api

import (
	"errors"
	"net/http"

	"todo_helper/internal/auth"
	"todo_helper/internal/store"
)

type userCreateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type userPatchRequest struct {
	Password *string `json:"password"`
	Role     *string `json:"role"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.log.Error("查询用户列表失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req userCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	username, err := validateUsername(req.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	role := store.RoleUser
	if req.Role != "" {
		normalized, ok := store.NormalizeRole(req.Role)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "角色只能是 admin 或 user")
			return
		}
		role = normalized
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.log.Error("密码哈希失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	user, err := s.store.CreateUser(r.Context(), username, hash, role)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "用户名已存在")
		return
	}
	if err != nil {
		s.log.Error("创建用户失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	target, err := s.store.GetUserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	if err != nil {
		s.log.Error("查询用户失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	var req userPatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Password == nil && req.Role == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "至少提供 password 或 role 之一")
		return
	}
	// 先校验完所有字段再写库，避免 role 已生效而 password 校验失败造成部分更新。
	var newRole *string
	if req.Role != nil {
		role, ok := store.NormalizeRole(*req.Role)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "角色只能是 admin 或 user")
			return
		}
		if target.Role == store.RoleAdmin && role != store.RoleAdmin {
			admins, err := s.store.CountAdmins(r.Context())
			if err != nil {
				s.log.Error("统计管理员失败", "error", err)
				writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
				return
			}
			if admins <= 1 {
				writeError(w, http.StatusConflict, "conflict", "必须保留至少一个管理员")
				return
			}
		}
		newRole = &role
	}
	var newHash string
	if req.Password != nil {
		if err := validatePassword(*req.Password); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			s.log.Error("密码哈希失败", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
		newHash = hash
	}
	if newRole != nil {
		if err := s.store.SetUserRole(r.Context(), target.ID, *newRole); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "用户不存在")
				return
			}
			s.log.Error("修改用户角色失败", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
	}
	if req.Password != nil {
		if err := s.store.SetUserPassword(r.Context(), target.ID, newHash); err != nil {
			s.log.Error("修改用户密码失败", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
		// 改密后旧刷新令牌全部失效，避免旧会话继续续期。
		if err := s.store.RevokeUserRefreshTokens(r.Context(), target.ID); err != nil {
			s.log.Error("撤销用户刷新令牌失败", "error", err)
		}
	}
	updated, err := s.store.GetUserByID(r.Context(), target.ID)
	if err != nil {
		s.log.Error("读取更新后的用户失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	current := userFrom(r.Context())
	if id == current.ID {
		writeError(w, http.StatusConflict, "conflict", "不能删除当前登录账号")
		return
	}
	target, err := s.store.GetUserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	if err != nil {
		s.log.Error("查询用户失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	if target.Role == store.RoleAdmin {
		admins, err := s.store.CountAdmins(r.Context())
		if err != nil {
			s.log.Error("统计管理员失败", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
		if admins <= 1 {
			writeError(w, http.StatusConflict, "conflict", "必须保留至少一个管理员")
			return
		}
	}
	if err := s.store.DeleteUser(r.Context(), target.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "用户不存在")
			return
		}
		s.log.Error("删除用户失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
