package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"quad/internal/store"
)

type todoCreateRequest struct {
	Title    string `json:"title"`
	Notes    string `json:"notes"`
	Quadrant string `json:"quadrant"`
	Status   string `json:"status"`
	Progress *int   `json:"progress"`
}

type todoPatchRequest struct {
	Title    *string `json:"title"`
	Notes    *string `json:"notes"`
	Quadrant *string `json:"quadrant"`
	Status   *string `json:"status"`
	Progress *int    `json:"progress"`
}

func (s *Server) handleListTodos(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.TodoFilter{Status: "", Quadrant: ""}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		status, ok := store.NormalizeStatus(raw)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "status 取值无效")
			return
		}
		filter.Status = status
	}
	if raw := strings.TrimSpace(q.Get("quadrant")); raw != "" {
		quadrant, ok := store.NormalizeQuadrant(raw)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "quadrant 取值无效")
			return
		}
		filter.Quadrant = quadrant
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit 必须是正整数")
			return
		}
		filter.Limit = n
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "offset 必须是非负整数")
			return
		}
		filter.Offset = n
	}
	todos, err := s.store.ListTodos(r.Context(), userFrom(r.Context()).ID, filter)
	if err != nil {
		s.log.Error("查询待办失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"todos": todos})
}

func (s *Server) handleCreateTodo(w http.ResponseWriter, r *http.Request) {
	var req todoCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	title, err := validateTitle(req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := validateNotes(req.Notes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	quadrant, ok := store.NormalizeQuadrant(req.Quadrant)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "quadrant 必须是 urgent_important / important_not_urgent / urgent_not_important / not_urgent_not_important 之一")
		return
	}
	status := store.StatusPending
	if strings.TrimSpace(req.Status) != "" {
		if status, ok = store.NormalizeStatus(req.Status); !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "status 取值无效")
			return
		}
	}
	progress := 0
	if req.Progress != nil {
		progress = *req.Progress
	}
	if err := validateProgress(progress); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	todo := store.Todo{
		UserID:   userFrom(r.Context()).ID,
		Title:    title,
		Notes:    req.Notes,
		Quadrant: quadrant,
		Status:   status,
		Progress: progress,
	}
	if status == store.StatusDone {
		todo.Progress = 100
		now := s.now().UTC()
		todo.CompletedAt = &now
	}
	created, err := s.store.CreateTodo(r.Context(), todo)
	if err != nil {
		s.log.Error("创建待办失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetTodo(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	todo, err := s.store.GetTodo(r.Context(), userFrom(r.Context()).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "待办不存在")
		return
	}
	if err != nil {
		s.log.Error("查询待办失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func (s *Server) handleUpdateTodo(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	user := userFrom(r.Context())
	todo, err := s.store.GetTodo(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "待办不存在")
		return
	}
	if err != nil {
		s.log.Error("查询待办失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	var req todoPatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Title == nil && req.Notes == nil && req.Quadrant == nil && req.Status == nil && req.Progress == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "没有需要更新的字段")
		return
	}
	if req.Title != nil {
		title, err := validateTitle(*req.Title)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		todo.Title = title
	}
	if req.Notes != nil {
		if err := validateNotes(*req.Notes); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		todo.Notes = *req.Notes
	}
	if req.Quadrant != nil {
		quadrant, ok := store.NormalizeQuadrant(*req.Quadrant)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "quadrant 取值无效")
			return
		}
		todo.Quadrant = quadrant
	}
	if req.Progress != nil {
		if err := validateProgress(*req.Progress); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		todo.Progress = *req.Progress
	}
	if req.Status != nil {
		status, ok := store.NormalizeStatus(*req.Status)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "status 取值无效")
			return
		}
		todo.Status = status
		switch status {
		case store.StatusDone:
			todo.Progress = 100
			if todo.CompletedAt == nil {
				now := s.now().UTC()
				todo.CompletedAt = &now
			}
		default:
			todo.CompletedAt = nil
		}
	}
	if err := s.store.UpdateTodo(r.Context(), todo); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "待办不存在")
			return
		}
		s.log.Error("更新待办失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	updated, err := s.store.GetTodo(r.Context(), user.ID, id)
	if err != nil {
		s.log.Error("读取更新后的待办失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteTodo(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.store.DeleteTodo(r.Context(), userFrom(r.Context()).ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "待办不存在")
			return
		}
		s.log.Error("删除待办失败", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
