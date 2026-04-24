package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/tools/kb"
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
	limit := body.Limit
	if body.TopK > 0 {
		limit = body.TopK
	}
	items, err := h.Deps.Knowledge.Search(r.Context(), chi.URLParam(r, "kb_id"), body.Query, limit, body.Filters)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, KBChunkListResponse{Items: items})
}

// CreateSource handles POST /v1/kbs/{kb_id}/sources.
// @Tags Knowledge
// @Summary Create knowledge source
// @Accept application/json
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param body body kb.CreateSourceInput true "Source payload"
// @Success 201 {object} kb.KnowledgeSource
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/sources [post]
func (h *KnowledgeHandler) CreateSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body kb.CreateSourceInput
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	source, err := h.Deps.Knowledge.CreateSource(chi.URLParam(r, "kb_id"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, source)
}

// ListSources handles GET /v1/kbs/{kb_id}/sources.
// @Tags Knowledge
// @Summary List knowledge sources
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Success 200 {object} KnowledgeSourceListResponse
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/sources [get]
func (h *KnowledgeHandler) ListSources(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	items, err := h.Deps.Knowledge.ListSources(chi.URLParam(r, "kb_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, KnowledgeSourceListResponse{Items: items})
}

// GetSource handles GET /v1/kbs/{kb_id}/sources/{source_id}.
// @Tags Knowledge
// @Summary Get knowledge source
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param source_id path string true "Source ID"
// @Success 200 {object} kb.KnowledgeSource
// @Failure 404 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/sources/{source_id} [get]
func (h *KnowledgeHandler) GetSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	source, ok := h.Deps.Knowledge.GetSource(chi.URLParam(r, "kb_id"), chi.URLParam(r, "source_id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "knowledge source not found"})
		return
	}
	writeJSON(w, http.StatusOK, source)
}

// UpdateSource handles PATCH /v1/kbs/{kb_id}/sources/{source_id}.
// @Tags Knowledge
// @Summary Update knowledge source
// @Accept application/json
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param source_id path string true "Source ID"
// @Param body body kb.UpdateSourceInput true "Source patch"
// @Success 200 {object} kb.KnowledgeSource
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/sources/{source_id} [patch]
func (h *KnowledgeHandler) UpdateSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body kb.UpdateSourceInput
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	source, err := h.Deps.Knowledge.UpdateSource(chi.URLParam(r, "kb_id"), chi.URLParam(r, "source_id"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, source)
}

// DeleteSource handles DELETE /v1/kbs/{kb_id}/sources/{source_id}.
// @Tags Knowledge
// @Summary Delete knowledge source
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param source_id path string true "Source ID"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/sources/{source_id} [delete]
func (h *KnowledgeHandler) DeleteSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	if err := h.Deps.Knowledge.DeleteSource(r.Context(), chi.URLParam(r, "kb_id"), chi.URLParam(r, "source_id")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, StatusResponse{Status: "deleted"})
}

// SyncSource handles POST /v1/kbs/{kb_id}/sources/{source_id}/sync.
// @Tags Knowledge
// @Summary Sync knowledge source
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param source_id path string true "Source ID"
// @Success 200 {object} kb.KnowledgeIndexJob
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/sources/{source_id}/sync [post]
func (h *KnowledgeHandler) SyncSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	job, err := h.Deps.Knowledge.SyncSource(r.Context(), chi.URLParam(r, "kb_id"), chi.URLParam(r, "source_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// ListIndexJobs handles GET /v1/kbs/{kb_id}/index-jobs.
// @Tags Knowledge
// @Summary List KB index jobs
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Success 200 {object} KnowledgeIndexJobListResponse
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/index-jobs [get]
func (h *KnowledgeHandler) ListIndexJobs(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	items, err := h.Deps.Knowledge.ListIndexJobs(chi.URLParam(r, "kb_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, KnowledgeIndexJobListResponse{Items: items})
}

// GetIndexJob handles GET /v1/kbs/{kb_id}/index-jobs/{job_id}.
// @Tags Knowledge
// @Summary Get KB index job
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param job_id path string true "Index job ID"
// @Success 200 {object} kb.KnowledgeIndexJob
// @Failure 404 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/index-jobs/{job_id} [get]
func (h *KnowledgeHandler) GetIndexJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	job, ok := h.Deps.Knowledge.GetIndexJob(chi.URLParam(r, "kb_id"), chi.URLParam(r, "job_id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "index job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// Retrieve handles POST /v1/kbs/{kb_id}/retrieve.
// @Tags Knowledge
// @Summary Retrieve KB chunks with citations
// @Accept application/json
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param body body kb.RetrievalQuery true "Retrieval payload"
// @Success 200 {object} RetrievalResultListResponse
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/retrieve [post]
func (h *KnowledgeHandler) Retrieve(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body kb.RetrievalQuery
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	body.KBID = chi.URLParam(r, "kb_id")
	items, err := h.Deps.Knowledge.Retrieve(r.Context(), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, RetrievalResultListResponse{Items: items})
}

// Answer handles POST /v1/kbs/{kb_id}/answer.
// @Tags Knowledge
// @Summary Answer from KB with citations
// @Accept application/json
// @Produce application/json
// @Param kb_id path string true "Knowledge base ID"
// @Param body body kb.AnswerInput true "Answer payload"
// @Success 200 {object} KBAnswerResponse
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/kbs/{kb_id}/answer [post]
func (h *KnowledgeHandler) Answer(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body kb.AnswerInput
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	body.KBID = chi.URLParam(r, "kb_id")
	answer, err := h.Deps.Knowledge.Answer(r.Context(), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, KBAnswerResponse{Answer: answer})
}

// ListRAGEvals handles GET /v1/rag/evals.
// @Tags RAG
// @Summary List RAG eval cases
// @Produce application/json
// @Success 200 {object} RAGEvalCaseListResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/rag/evals [get]
func (h *KnowledgeHandler) ListRAGEvals(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	writeJSON(w, http.StatusOK, RAGEvalCaseListResponse{Items: h.Deps.Knowledge.ListRAGEvals()})
}

// CreateRAGEval handles POST /v1/rag/evals.
// @Tags RAG
// @Summary Create RAG eval case
// @Accept application/json
// @Produce application/json
// @Param body body kb.RAGEvalCase true "Eval case"
// @Success 201 {object} kb.RAGEvalCase
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/rag/evals [post]
func (h *KnowledgeHandler) CreateRAGEval(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body kb.RAGEvalCase
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := h.Deps.Knowledge.CreateRAGEval(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// RunRAGEval handles POST /v1/rag/evals/run.
// @Tags RAG
// @Summary Run RAG eval
// @Accept application/json
// @Produce application/json
// @Param body body RAGEvalRunRequest false "Run selection"
// @Success 200 {object} kb.RAGEvalRun
// @Failure 400 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/rag/evals/run [post]
func (h *KnowledgeHandler) RunRAGEval(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	var body RAGEvalRunRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	run, err := h.Deps.Knowledge.RunRAGEval(r.Context(), body.CaseIDs)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// GetRAGEvalRun handles GET /v1/rag/evals/runs/{run_id}.
// @Tags RAG
// @Summary Get RAG eval run
// @Produce application/json
// @Param run_id path string true "Eval run ID"
// @Success 200 {object} kb.RAGEvalRun
// @Failure 404 {object} LegacyErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/rag/evals/runs/{run_id} [get]
func (h *KnowledgeHandler) GetRAGEvalRun(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	run, ok := h.Deps.Knowledge.GetRAGEvalRun(chi.URLParam(r, "run_id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "rag eval run not found"})
		return
	}
	writeJSON(w, http.StatusOK, run)
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
