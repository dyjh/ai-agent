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
func (h *MemoryHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	items, err := h.Deps.Memory.ListFiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetFile handles GET /v1/memory/files/{path}.
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
func (h *MemoryHandler) Search(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := h.Deps.Memory.Search(body.Query, body.Limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// CreatePatch handles POST /v1/memory/patches.
func (h *MemoryHandler) CreatePatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path        string            `json:"path"`
		Summary     string            `json:"summary"`
		Body        string            `json:"body"`
		Frontmatter map[string]string `json:"frontmatter"`
	}
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
func (h *MemoryHandler) Reindex(w http.ResponseWriter, r *http.Request) {
	if err := h.Deps.Memory.Reindex(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
