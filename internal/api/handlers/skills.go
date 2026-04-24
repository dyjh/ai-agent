package handlers

import (
	"io"
	"net/http"
	"os"
	"strings"
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

// Upload registers a local skill directory or manifest path.
// @Tags Skills
// @Summary Upload local skill path
// @Accept application/json
// @Produce application/json
// @Param body body skills.UploadInput true "Local skill upload request"
// @Success 201 {object} core.SkillRegistration
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /v1/skills/upload [post]
func (h *SkillsHandler) Upload(w http.ResponseWriter, r *http.Request) {
	var body skills.UploadInput
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	item, err := h.Deps.Skills.Upload(body.Path, body.Name, body.Description)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// UploadZip installs a skill package from a zip archive.
// @Tags Skills
// @Summary Upload skill zip package
// @Accept multipart/form-data
// @Produce application/json
// @Param file formData file true "Skill zip archive"
// @Param force formData boolean false "Overwrite the same installed version"
// @Success 201 {object} map[string]any
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /v1/skills/upload-zip [post]
func (h *SkillsHandler) UploadZip(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "multipart field file is required", nil)
		return
	}
	defer file.Close()

	tempFile, err := os.CreateTemp("", "skill-upload-*.zip")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, file); err != nil {
		tempFile.Close()
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	if err := tempFile.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	force := strings.EqualFold(r.FormValue("force"), "true")
	item, err := h.Deps.Skills.InstallZip(tempPath, force)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), map[string]any{"filename": header.Filename})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"skill":   item.Registration,
		"package": item.Package,
	})
}

// List handles GET /v1/skills.
func (h *SkillsHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Deps.Skills.List()})
}

// Get returns the registered skill and manifest.
// @Tags Skills
// @Summary Get skill detail
// @Produce application/json
// @Param id path string true "Skill ID"
// @Success 200 {object} map[string]any
// @Failure 404 {object} ErrorResponse
// @Router /v1/skills/{id} [get]
func (h *SkillsHandler) Get(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Skills.Resolve(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"skill":    item.Registration,
		"manifest": item.Manifest,
		"package":  item.Package,
	})
}

// Manifest returns the normalized manifest for a registered skill.
// @Tags Skills
// @Summary Get skill manifest
// @Produce application/json
// @Param id path string true "Skill ID"
// @Success 200 {object} map[string]any
// @Failure 404 {object} ErrorResponse
// @Router /v1/skills/{id}/manifest [get]
func (h *SkillsHandler) Manifest(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Skills.Resolve(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"skill":    item.Registration,
		"manifest": item.Manifest,
	})
}

// Package returns package/install metadata for a registered skill.
// @Tags Skills
// @Summary Get skill package metadata
// @Produce application/json
// @Param id path string true "Skill ID"
// @Success 200 {object} core.SkillPackageInfo
// @Failure 404 {object} ErrorResponse
// @Router /v1/skills/{id}/package [get]
func (h *SkillsHandler) Package(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Skills.Package(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Enable handles POST /v1/skills/{id}/enable.
func (h *SkillsHandler) Enable(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Skills.SetEnabled(chi.URLParam(r, "id"), true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Test validates a skill request without executing it.
func (h *SkillsHandler) Test(w http.ResponseWriter, r *http.Request) {
	h.Validate(w, r)
}

// Disable handles POST /v1/skills/{id}/disable.
func (h *SkillsHandler) Disable(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Skills.SetEnabled(chi.URLParam(r, "id"), false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Validate checks the registered manifest, permissions, and optional runtime args.
// @Tags Skills
// @Summary Validate a registered skill
// @Accept application/json
// @Produce application/json
// @Param id path string true "Skill ID"
// @Param body body skills.SkillRunInput false "Validation request"
// @Success 200 {object} map[string]any
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /v1/skills/{id}/validate [post]
func (h *SkillsHandler) Validate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Args map[string]any `json:"args"`
	}
	_ = decodeJSON(r, &body)
	if body.Args == nil {
		body.Args = map[string]any{}
	}

	item, err := h.Deps.Skills.Resolve(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	validation, err := h.Deps.Skills.Validate(item.Registration.ID, body.Args, int64(h.Deps.Config.Shell.MaxOutputChars))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"skill":      item.Registration,
		"package":    item.Package,
		"validation": validation,
	})
}

// Remove uninstalls a registered skill.
// @Tags Skills
// @Summary Remove a registered skill
// @Produce application/json
// @Param id path string true "Skill ID"
// @Success 200 {object} skills.RemovalResult
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /v1/skills/{id} [delete]
func (h *SkillsHandler) Remove(w http.ResponseWriter, r *http.Request) {
	result, err := h.Deps.Skills.Remove(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Run executes a registered skill through the approval chain.
// @Tags Skills
// @Summary Run skill through ToolRouter
// @Accept application/json
// @Produce application/json
// @Param id path string true "Skill ID"
// @Param body body skills.SkillRunInput false "Skill run request"
// @Success 200 {object} map[string]any
// @Success 202 {object} map[string]any
// @Failure 400 {object} ErrorResponse
// @Router /v1/skills/{id}/run [post]
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
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
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
