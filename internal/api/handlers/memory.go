package handlers

import (
	"net/http"
	"strings"

	"local-agent/internal/core"
)

// MemoryHandler serves Markdown memory APIs.
type MemoryHandler struct {
	Base
}

// NewMemoryHandler creates a memory handler.
func NewMemoryHandler(deps Dependencies) *MemoryHandler {
	return &MemoryHandler{Base{Deps: deps}}
}

// ListFiles handles GET /v1/memory/files.
// @Tags Memory
// @Summary List memory files
// @Produce application/json
// @Success 200 {object} MemoryFilePathListResponse
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/memory/files [get]
func (h *MemoryHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	items, err := h.Deps.Memory.ListFiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, MemoryFilePathListResponse{Items: items})
}

// GetFile handles GET /v1/memory/files/{path}.
// @Tags Memory
// @Summary Read memory file
// @Produce application/json
// @Param path path string true "Memory file path"
// @Success 200 {object} core.MemoryFile
// @Failure 404 {object} LegacyErrorResponse
// @Router /v1/memory/files/{path} [get]
func (h *MemoryHandler) GetFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/memory/files/")
	item, err := h.Deps.Memory.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Search handles POST /v1/memory/search.
// @Tags Memory
// @Summary Search markdown memory
// @Accept application/json
// @Produce application/json
// @Param body body SearchRequest true "Search payload"
// @Success 200 {object} MemoryFileListResponse
// @Failure 400 {object} LegacyErrorResponse
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/memory/search [post]
func (h *MemoryHandler) Search(w http.ResponseWriter, r *http.Request) {
	var body SearchRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := h.Deps.Memory.Search(body.Query, body.Limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, MemoryFileListResponse{Items: items})
}

// CreatePatch handles POST /v1/memory/patches.
// @Tags Memory
// @Summary Create memory patch
// @Accept application/json
// @Produce application/json
// @Param body body CreateMemoryPatchRequest true "Patch payload"
// @Success 201 {object} core.MemoryPatch
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/memory/patches [post]
func (h *MemoryHandler) CreatePatch(w http.ResponseWriter, r *http.Request) {
	var body CreateMemoryPatchRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := h.Deps.Memory.CreatePatch(core.MemoryPatch{
		Path:        body.Path,
		Summary:     body.Summary,
		Body:        body.Body,
		Frontmatter: body.Frontmatter,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// Reindex handles POST /v1/memory/reindex.
// @Tags Memory
// @Summary Reindex markdown memory
// @Accept application/json
// @Produce application/json
// @Success 200 {object} StatusResponse
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/memory/reindex [post]
func (h *MemoryHandler) Reindex(w http.ResponseWriter, r *http.Request) {
	if err := h.Deps.Memory.Reindex(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, StatusResponse{Status: "ok"})
}
