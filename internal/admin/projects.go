package admin

import (
	"net/http"

	"openlimit/internal/audit"
	"openlimit/internal/requestid"
	"openlimit/internal/store"
)

type createProjectRequest struct {
	Name string `json:"name"`
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "project:create")
	if actor == nil {
		return
	}

	var req createProjectRequest
	if err := readJSON(r, &req); err != nil || req.Name == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}

	project, err := store.CreateProject(r.Context(), h.db, req.Name)
	if err != nil {
		writeAdminError(w, r, http.StatusConflict, "conflict", "project name already exists")
		return
	}

	writeAdminJSON(w, http.StatusCreated, project)

	h.audit.Record(audit.Event{
		EventType: audit.EventProjectCreate,
		Actor:     actorFromRequest(r),
		Action:    "create",
		Resource:  "project:" + project.ID,
		Outcome:   "success",
		RequestID: requestid.FromContext(r.Context()),
		Metadata:  map[string]any{"name": project.Name},
	})
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "project:list")
	if actor == nil {
		return
	}

	projects, err := store.ListProjects(r.Context(), h.db)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to list projects")
		return
	}
	if projects == nil {
		projects = []store.Project{}
	}
	writeAdminJSON(w, http.StatusOK, projects)
}

func (h *Handler) deleteProject(w http.ResponseWriter, r *http.Request) {
	actor := h.requireRole(w, r, "project:delete")
	if actor == nil {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeAdminError(w, r, http.StatusBadRequest, "invalid_request", "project id is required")
		return
	}

	deleted, err := store.DeleteProject(r.Context(), h.db, id)
	if err != nil {
		writeAdminError(w, r, http.StatusInternalServerError, "internal_error", "failed to delete project")
		return
	}
	if !deleted {
		writeAdminError(w, r, http.StatusNotFound, "not_found", "project not found")
		return
	}

	h.audit.Record(audit.Event{
		EventType: audit.EventProjectDelete,
		Actor:     actorFromRequest(r),
		Action:    "delete",
		Resource:  "project:" + id,
		Outcome:   "success",
		RequestID: requestid.FromContext(r.Context()),
	})

	w.WriteHeader(http.StatusNoContent)
}
