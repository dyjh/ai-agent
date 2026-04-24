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
// @Tags Knowledge
// @Summary Create knowledge base
// @Accept application/json
// @Produce application/json
// @Param body body CreateKnowledgeBaseRequest true "Knowledge base payload"
// @Success 201 {object} KnowledgeBaseItem
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs [post]
func (h *KnowledgeHandler) CreateKB(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body CreateKnowledgeBaseRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item := h.Deps.Knowledge.CreateKB(body.Name, body.Description)
	writeJSON(w, http.StatusCreated, item)
}

// ListKBs handles GET /v1/kbs.
// @Tags Knowledge
// @Summary List knowledge bases
// @Produce application/json
// @Success 200 {object} KnowledgeBaseListResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs [get]
func (h *KnowledgeHandler) ListKBs(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	writeJSON(w, http.StatusOK, KnowledgeBaseListResponse{Items: h.Deps.Knowledge.ListKBs()})
}

// Health handles GET /v1/kbs/health.
// @Tags Knowledge
// @Summary Get knowledge base health
// @Produce application/json
// @Success 200 {object} KnowledgeBaseHealthResponse
// @Router /v1/kbs/health [get]
func (h *KnowledgeHandler) Health(w http.ResponseWriter, r *http.Request) {
	if !h.Deps.Config.KB.Enabled || h.Deps.Knowledge == nil {
		writeJSON(w, http.StatusOK, KnowledgeBaseHealthResponse{
			Enabled:  false,
			Provider: h.Deps.Config.KB.Provider,
			Status:   "disabled",
		})
		return
	}
	health := h.Deps.Knowledge.Health(r.Context())
	writeJSON(w, http.StatusOK, KnowledgeBaseHealthResponse{
		VectorBackend:  health.VectorBackend,
		FallbackReason: health.FallbackReason,
		Qdrant:         health.Qdrant,
		Collections:    health.Collections,
		Error:          health.Error,
	})
}

// Upload handles POST /v1/kbs/{kb_id}/documents/upload.
// @Tags Knowledge
// @Summary Upload KB document
// @Accept application/json
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param body body KnowledgeBaseDocumentUploadRequest true "Document payload"
// @Success 201 {object} KBChunkListResponse
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/documents/upload [post]
func (h *KnowledgeHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body KnowledgeBaseDocumentUploadRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := h.Deps.Knowledge.UploadDocument(r.Context(), chi.URLParam(r, "kb_id"), body.Filename, body.Content)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, KBChunkListResponse{Items: items})
}

// Search handles POST /v1/kbs/{kb_id}/search.
// @Tags Knowledge
// @Summary Search knowledge base
// @Accept application/json
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param body body SearchRequest true "Search payload"
// @Success 200 {object} KBChunkListResponse
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/search [post]
func (h *KnowledgeHandler) Search(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body SearchRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := h.Deps.Knowledge.Search(r.Context(), chi.URLParam(r, "kb_id"), body.Query, body.Limit, nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, KBChunkListResponse{Items: items})
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
