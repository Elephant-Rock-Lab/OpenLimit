package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"openlimit/internal/store"
)

// Prompt CRUD handlers

type createPromptRequest struct {
	Name        string `json:"name"`
	Content     string `json:"content"`
	Description string `json:"description"`
}

func (h *Handler) createPrompt(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "prompt:create")
	if actor == nil {
		return
	}
	if h.db == nil {
		writeAdminError(w, r, http.StatusServiceUnavailable, "no_database", "database not configured")
		return
	}

	var req createPromptRequest
	if err := readJSON(r, &req); err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.Content == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "content is required")
		return
	}

	pt := &store.PromptTemplate{
		Name:        req.Name,
		Content:     req.Content,
		Description: req.Description,
	}
	if err := store.CreatePromptTemplate(r.Context(), h.db, pt); err != nil {
		if errors.Is(err, sql.ErrNoRows) || isDuplicateKey(err) {
			writeAdminError(w, r, http.StatusConflict, "duplicate", "prompt with this name already exists")
			return
		}
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to create prompt")
		return
	}

	writeAdminJSON(w, http.StatusCreated, pt)
}

func (h *Handler) listPrompts(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "prompt:list")
	if actor == nil {
		return
	}
	if h.db == nil {
		writeAdminJSON(w, http.StatusOK, []any{})
		return
	}

	prompts, err := store.ListPromptTemplates(r.Context(), h.db)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to list prompts")
		return
	}
	writeAdminJSON(w, http.StatusOK, prompts)
}

func (h *Handler) updatePrompt(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "prompt:update")
	if actor == nil {
		return
	}
	if h.db == nil {
		writeAdminError(w, r, http.StatusServiceUnavailable, "no_database", "database not configured")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "prompt id is required")
		return
	}

	var req createPromptRequest
	if err := readJSON(r, &req); err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.Content == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "content is required")
		return
	}

	pt := &store.PromptTemplate{
		ID:          id,
		Name:        req.Name,
		Content:     req.Content,
		Description: req.Description,
	}
	if err := store.UpdatePromptTemplate(r.Context(), h.db, pt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAdminError(w, r, http.StatusNotFound, "not_found", "prompt not found")
			return
		}
		if isDuplicateKey(err) {
			writeAdminError(w, r, http.StatusConflict, "duplicate", "prompt with this name already exists")
			return
		}
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to update prompt")
		return
	}

	writeAdminJSON(w, http.StatusOK, pt)
}

func (h *Handler) deletePrompt(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "prompt:delete")
	if actor == nil {
		return
	}
	if h.db == nil {
		writeAdminError(w, r, http.StatusServiceUnavailable, "no_database", "database not configured")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "prompt id is required")
		return
	}

	deleted, err := store.DeletePromptTemplate(r.Context(), h.db, id)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to delete prompt")
		return
	}
	if !deleted {
		writeAdminError(w, r, http.StatusNotFound, "not_found", "prompt not found")
		return
	}

	writeAdminJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// isDuplicateKey checks if the error is a unique constraint violation.
func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "duplicate key") || strings.Contains(s, "23505") || strings.Contains(s, "unique")
}
