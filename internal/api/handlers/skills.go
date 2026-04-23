package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/core"
	"local-agent/internal/ids"
	"local-agent/internal/tools/skills"
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

// Get handles GET /v1/skills/{id}.
func (h *SkillsHandler) Get(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Skills.Resolve(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"skill":    item.Registration,
		"manifest": item.Manifest,
	})
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

// Test handles POST /v1/skills/{id}/test.
func (h *SkillsHandler) Test(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Args map[string]any `json:"args"`
	}
	_ = decodeJSON(r, &body)

	item, err := h.Deps.Skills.Resolve(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	if body.Args == nil {
		body.Args = map[string]any{}
	}
	if err := skills.ValidateInput(item.Manifest.InputSchema, body.Args); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"skill":     item.Registration,
		"validated": true,
	})
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

// Run handles POST /v1/skills/{id}/run.
func (h *SkillsHandler) Run(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Args map[string]any `json:"args"`
	}
	_ = decodeJSON(r, &body)
	if body.Args == nil {
		body.Args = map[string]any{}
	}

	proposal := core.ToolProposal{
		ID:        ids.New("tool"),
		Tool:      "skill.run",
		Input:     map[string]any{"skill_id": chi.URLParam(r, "id"), "args": body.Args},
		Purpose:   "通过 Skill API 执行本地技能",
		CreatedAt: time.Now().UTC(),
	}

	outcome, err := h.Deps.Router.Propose(r.Context(), "", "", proposal)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if outcome.Approval != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"approval":  outcome.Approval,
			"decision":  outcome.Decision,
			"inference": outcome.Inference,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"decision":  outcome.Decision,
		"inference": outcome.Inference,
		"result":    outcome.Result,
	})
}
