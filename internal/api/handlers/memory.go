package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/core"
	"local-agent/internal/ids"
	memstore "local-agent/internal/tools/memory"
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

// ListItems handles GET /v1/memory/items.
// @Tags Memory
// @Summary List memory items
// @Produce application/json
// @Param scope query string false "Memory scope"
// @Param type query string false "Memory type"
// @Param project_key query string false "Project key"
// @Param status query string false "Item status"
// @Param tag query string false "Tag"
// @Param query query string false "Text query"
// @Param include_archived query boolean false "Include archived/rejected/expired items"
// @Param limit query int false "Maximum items"
// @Success 200 {object} MemoryItemListResponse
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/memory/items [get]
func (h *MemoryHandler) ListItems(w http.ResponseWriter, r *http.Request) {
	filter := memstore.MemoryItemFilter{
		Scope:           memstore.MemoryScope(r.URL.Query().Get("scope")),
		Type:            memstore.MemoryType(r.URL.Query().Get("type")),
		ProjectKey:      r.URL.Query().Get("project_key"),
		Status:          memstore.MemoryItemStatus(r.URL.Query().Get("status")),
		Tag:             r.URL.Query().Get("tag"),
		Query:           r.URL.Query().Get("query"),
		IncludeArchived: strings.EqualFold(r.URL.Query().Get("include_archived"), "true"),
	}
	if limit := parsePositiveInt(r.URL.Query().Get("limit")); limit > 0 {
		filter.Limit = limit
	}
	items, err := h.Deps.Memory.ListItems(filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, MemoryItemListResponse{Items: items})
}

// CreateItem handles POST /v1/memory/items.
// @Tags Memory
// @Summary Create memory item through ToolRouter
// @Accept application/json
// @Produce application/json
// @Param body body MemoryItemCreateRequest true "Memory item payload"
// @Success 200 {object} ToolRouteResponse
// @Success 202 {object} ToolRouteResponse
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/memory/items [post]
func (h *MemoryHandler) CreateItem(w http.ResponseWriter, r *http.Request) {
	var body memstore.MemoryItemCreateInput
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if scan := memstore.ScanSensitiveMemory(body.Text); scan.Sensitive {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "sensitive memory content is not allowed"})
		return
	}
	h.routeMemoryProposal(w, r, "memory.item_create", map[string]any{
		"scope":             body.Scope,
		"type":              body.Type,
		"project_key":       body.ProjectKey,
		"text":              body.Text,
		"source":            body.Source,
		"source_message_id": body.SourceMessageID,
		"confidence":        body.Confidence,
		"importance":        body.Importance,
		"tags":              body.Tags,
		"expires_at":        body.ExpiresAt,
		"decay_policy":      body.DecayPolicy,
		"metadata":          body.Metadata,
		"path":              body.Path,
	}, "新增长期记忆 item")
}

// GetItem handles GET /v1/memory/items/{id}.
// @Tags Memory
// @Summary Get memory item
// @Produce application/json
// @Param id path string true "Memory item ID"
// @Success 200 {object} memstore.MemoryItem
// @Failure 404 {object} LegacyErrorResponse
// @Router /v1/memory/items/{id} [get]
func (h *MemoryHandler) GetItem(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Memory.GetItem(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// UpdateItem handles PATCH /v1/memory/items/{id}.
// @Tags Memory
// @Summary Update memory item through ToolRouter
// @Accept application/json
// @Produce application/json
// @Param id path string true "Memory item ID"
// @Param body body MemoryItemUpdateRequest true "Memory item update"
// @Success 200 {object} ToolRouteResponse
// @Success 202 {object} ToolRouteResponse
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/memory/items/{id} [patch]
func (h *MemoryHandler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if text, ok := body["text"].(string); ok {
		if scan := memstore.ScanSensitiveMemory(text); scan.Sensitive {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "sensitive memory content is not allowed"})
			return
		}
	}
	h.routeMemoryProposal(w, r, "memory.item_update", map[string]any{
		"id":     chi.URLParam(r, "id"),
		"fields": body,
	}, "更新长期记忆 item")
}

// DeleteItem handles DELETE /v1/memory/items/{id}.
// @Tags Memory
// @Summary Archive or force-delete memory item through ToolRouter
// @Produce application/json
// @Param id path string true "Memory item ID"
// @Param force query boolean false "Physically remove item block"
// @Success 200 {object} ToolRouteResponse
// @Success 202 {object} ToolRouteResponse
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/memory/items/{id} [delete]
func (h *MemoryHandler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	h.routeMemoryProposal(w, r, "memory.item_delete", map[string]any{
		"id":    chi.URLParam(r, "id"),
		"force": strings.EqualFold(r.URL.Query().Get("force"), "true"),
	}, "删除或归档长期记忆 item")
}

// ArchiveItem handles POST /v1/memory/items/{id}/archive.
// @Tags Memory
// @Summary Archive memory item through ToolRouter
// @Produce application/json
// @Param id path string true "Memory item ID"
// @Success 200 {object} ToolRouteResponse
// @Success 202 {object} ToolRouteResponse
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/memory/items/{id}/archive [post]
func (h *MemoryHandler) ArchiveItem(w http.ResponseWriter, r *http.Request) {
	h.routeMemoryProposal(w, r, "memory.item_archive", map[string]any{"id": chi.URLParam(r, "id")}, "归档长期记忆 item")
}

// RestoreItem handles POST /v1/memory/items/{id}/restore.
// @Tags Memory
// @Summary Restore memory item through ToolRouter
// @Produce application/json
// @Param id path string true "Memory item ID"
// @Success 200 {object} ToolRouteResponse
// @Success 202 {object} ToolRouteResponse
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/memory/items/{id}/restore [post]
func (h *MemoryHandler) RestoreItem(w http.ResponseWriter, r *http.Request) {
	h.routeMemoryProposal(w, r, "memory.item_restore", map[string]any{"id": chi.URLParam(r, "id")}, "恢复长期记忆 item")
}

// ExtractReview handles POST /v1/memory/review/extract.
// @Tags Memory
// @Summary Extract memory candidates into review queue
// @Accept application/json
// @Produce application/json
// @Param body body MemoryExtractReviewRequest true "Extraction payload"
// @Success 201 {object} MemoryReviewListResponse
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/memory/review/extract [post]
func (h *MemoryHandler) ExtractReview(w http.ResponseWriter, r *http.Request) {
	var body MemoryExtractReviewRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := h.Deps.Memory.ExtractAndReview(memstore.MemoryExtractInput{
		ConversationID: body.ConversationID,
		MessageID:      body.MessageID,
		Text:           body.Text,
		ProjectKey:     body.ProjectKey,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, MemoryReviewListResponse{Items: items})
}

// ListReview handles GET /v1/memory/review.
// @Tags Memory
// @Summary List memory review queue
// @Produce application/json
// @Param status query string false "Review status"
// @Param limit query int false "Maximum items"
// @Success 200 {object} MemoryReviewListResponse
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/memory/review [get]
func (h *MemoryHandler) ListReview(w http.ResponseWriter, r *http.Request) {
	items, err := h.Deps.Memory.ListReviews(memstore.MemoryReviewStatus(r.URL.Query().Get("status")), parsePositiveInt(r.URL.Query().Get("limit")))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, MemoryReviewListResponse{Items: items})
}

// GetReview handles GET /v1/memory/review/{review_id}.
// @Tags Memory
// @Summary Get memory review item
// @Produce application/json
// @Param review_id path string true "Review ID"
// @Success 200 {object} memstore.MemoryReviewItem
// @Failure 404 {object} LegacyErrorResponse
// @Router /v1/memory/review/{review_id} [get]
func (h *MemoryHandler) GetReview(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Memory.GetReview(chi.URLParam(r, "review_id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// ApproveReview handles POST /v1/memory/review/{review_id}/approve.
// @Tags Memory
// @Summary Approve memory review item and apply it
// @Accept application/json
// @Produce application/json
// @Param review_id path string true "Review ID"
// @Param body body MemoryReviewDecisionRequest false "Decision note"
// @Success 200 {object} memstore.MemoryReviewItem
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/memory/review/{review_id}/approve [post]
func (h *MemoryHandler) ApproveReview(w http.ResponseWriter, r *http.Request) {
	var body MemoryReviewDecisionRequest
	_ = decodeJSON(r, &body)
	item, err := h.Deps.Memory.ApproveReview(r.Context(), chi.URLParam(r, "review_id"), body.Note)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// RejectReview handles POST /v1/memory/review/{review_id}/reject.
// @Tags Memory
// @Summary Reject memory review item
// @Accept application/json
// @Produce application/json
// @Param review_id path string true "Review ID"
// @Param body body MemoryReviewDecisionRequest false "Decision note"
// @Success 200 {object} memstore.MemoryReviewItem
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/memory/review/{review_id}/reject [post]
func (h *MemoryHandler) RejectReview(w http.ResponseWriter, r *http.Request) {
	var body MemoryReviewDecisionRequest
	_ = decodeJSON(r, &body)
	item, err := h.Deps.Memory.RejectReview(chi.URLParam(r, "review_id"), body.Note)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
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

func (h *MemoryHandler) routeMemoryProposal(w http.ResponseWriter, r *http.Request, tool string, input map[string]any, purpose string) {
	if h.Deps.Router == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tool router is not configured"})
		return
	}
	proposal := core.ToolProposal{
		ID:        ids.New("tool"),
		Tool:      tool,
		Input:     input,
		Purpose:   purpose,
		CreatedAt: time.Now().UTC(),
	}
	outcome, err := h.Deps.Router.Propose(r.Context(), "", "", proposal)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	response := ToolRouteResponse{
		Decision:  &outcome.Decision,
		Inference: &outcome.Inference,
		Result:    outcome.Result,
		Approval:  outcome.Approval,
	}
	if outcome.Approval != nil {
		writeJSON(w, http.StatusAccepted, response)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func parsePositiveInt(value string) int {
	if value == "" {
		return 0
	}
	var out int
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		out = out*10 + int(r-'0')
	}
	return out
}
