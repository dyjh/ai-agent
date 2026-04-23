package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// SkillsHandler serves skill APIs.
type SkillsHandler struct {
	Base
}

// NewSkillsHandler creates a skills handler.
func NewSkillsHandler(deps Dependencies) *SkillsHandler {
	return &SkillsHandler{Base{Deps: deps}}
}

// Upload handles POST /v1/skills/upload.
func (h *SkillsHandler) Upload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path        string `json:"path"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := h.Deps.Skills.Upload(body.Path, body.Name, body.Description)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// List handles GET /v1/skills.
func (h *SkillsHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Deps.Skills.List()})
}

// Enable handles POST /v1/skills/{id}/enable.
func (h *SkillsHandler) Enable(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Skills.SetEnabled(chi.URLParam(r, "id"), true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Disable handles POST /v1/skills/{id}/disable.
func (h *SkillsHandler) Disable(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Skills.SetEnabled(chi.URLParam(r, "id"), false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}
