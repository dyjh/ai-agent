package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// KnowledgeHandler serves knowledge base APIs.
type KnowledgeHandler struct {
	Base
}

// NewKnowledgeHandler creates a KB handler.
func NewKnowledgeHandler(deps Dependencies) *KnowledgeHandler {
	return &KnowledgeHandler{Base{Deps: deps}}
}

// CreateKB handles POST /v1/kbs.
func (h *KnowledgeHandler) CreateKB(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item := h.Deps.Knowledge.CreateKB(body.Name, body.Description)
	writeJSON(w, http.StatusCreated, item)
}

// ListKBs handles GET /v1/kbs.
func (h *KnowledgeHandler) ListKBs(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Deps.Knowledge.ListKBs()})
}

// Health handles GET /v1/kbs/health.
func (h *KnowledgeHandler) Health(w http.ResponseWriter, r *http.Request) {
	if !h.Deps.Config.KB.Enabled || h.Deps.Knowledge == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":  false,
			"provider": h.Deps.Config.KB.Provider,
			"status":   "disabled",
		})
		return
	}
	writeJSON(w, http.StatusOK, h.Deps.Knowledge.Health(r.Context()))
}

// Upload handles POST /v1/kbs/{kb_id}/documents/upload.
func (h *KnowledgeHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := h.Deps.Knowledge.UploadDocument(r.Context(), chi.URLParam(r, "kb_id"), body.Filename, body.Content)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"items": items})
}

// Search handles POST /v1/kbs/{kb_id}/search.
func (h *KnowledgeHandler) Search(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := h.Deps.Knowledge.Search(r.Context(), chi.URLParam(r, "kb_id"), body.Query, body.Limit, nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *KnowledgeHandler) requireEnabled(w http.ResponseWriter) bool {
	if h.Deps.Config.KB.Enabled && h.Deps.Knowledge != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "feature_disabled", "knowledge base is disabled", map[string]any{
		"enabled":  false,
		"provider": h.Deps.Config.KB.Provider,
	})
	return false
}
