package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/evals"
)

// EvalsHandler serves generic eval and replay APIs.
type EvalsHandler struct {
	Base
}

// NewEvalsHandler creates an eval handler.
func NewEvalsHandler(deps Dependencies) *EvalsHandler {
	return &EvalsHandler{Base{Deps: deps}}
}

// List handles GET /v1/evals.
// @Tags Eval
// @Summary List eval cases
// @Produce application/json
// @Param category query string false "Eval category"
// @Param tag query string false "Tag filter"
// @Success 200 {object} evals.EvalCaseListResponse
// @Failure 500 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/evals [get]
func (h *EvalsHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	items, err := h.Deps.Evals.ListCases(evals.EvalRunRequest{
		Category: evals.EvalCategory(r.URL.Query().Get("category")),
		Tag:      r.URL.Query().Get("tag"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "eval_list_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, evals.EvalCaseListResponse{Items: items})
}

// Create handles POST /v1/evals.
// @Tags Eval
// @Summary Create eval case
// @Accept application/json
// @Produce application/json
// @Param body body evals.EvalCase true "Eval case"
// @Success 201 {object} evals.EvalCase
// @Failure 400 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/evals [post]
func (h *EvalsHandler) Create(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	var body evals.EvalCase
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), nil)
		return
	}
	item, err := h.Deps.Evals.CreateCase(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "eval_create_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// Get handles GET /v1/evals/{case_id}.
// @Tags Eval
// @Summary Get eval case
// @Produce application/json
// @Param case_id path string true "Eval case ID"
// @Success 200 {object} evals.EvalCase
// @Failure 404 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/evals/{case_id} [get]
func (h *EvalsHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	item, err := h.Deps.Evals.GetCase(chi.URLParam(r, "case_id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "eval_not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Update handles PATCH /v1/evals/{case_id}.
// @Tags Eval
// @Summary Update eval case
// @Accept application/json
// @Produce application/json
// @Param case_id path string true "Eval case ID"
// @Param body body evals.EvalCase true "Eval case"
// @Success 200 {object} evals.EvalCase
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/evals/{case_id} [patch]
func (h *EvalsHandler) Update(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	var body evals.EvalCase
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), nil)
		return
	}
	item, err := h.Deps.Evals.UpdateCase(chi.URLParam(r, "case_id"), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "eval_update_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Delete handles DELETE /v1/evals/{case_id}.
// @Tags Eval
// @Summary Delete eval case
// @Produce application/json
// @Param case_id path string true "Eval case ID"
// @Success 200 {object} StatusResponse
// @Failure 404 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/evals/{case_id} [delete]
func (h *EvalsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	if err := h.Deps.Evals.DeleteCase(chi.URLParam(r, "case_id")); err != nil {
		writeError(w, http.StatusNotFound, "eval_delete_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, StatusResponse{Status: "deleted"})
}

// Run handles POST /v1/evals/run.
// @Tags Eval
// @Summary Run eval cases in safe mode
// @Accept application/json
// @Produce application/json
// @Param body body evals.EvalRunRequest false "Eval run selection"
// @Success 200 {object} evals.EvalRun
// @Failure 400 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/evals/run [post]
func (h *EvalsHandler) Run(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	var body evals.EvalRunRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), nil)
			return
		}
	}
	run, err := h.Deps.Evals.Run(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "eval_run_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// ListRuns handles GET /v1/evals/runs.
// @Tags Eval
// @Summary List eval runs
// @Produce application/json
// @Param limit query int false "Max runs to return"
// @Success 200 {object} evals.EvalRunListResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/evals/runs [get]
func (h *EvalsHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Deps.Evals.ListRuns(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "eval_runs_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, evals.EvalRunListResponse{Items: items})
}

// GetRun handles GET /v1/evals/runs/{run_id}.
// @Tags Eval
// @Summary Get eval run
// @Produce application/json
// @Param run_id path string true "Eval run ID or latest"
// @Success 200 {object} evals.EvalRun
// @Failure 404 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/evals/runs/{run_id} [get]
func (h *EvalsHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	run, err := h.Deps.Evals.GetRun(chi.URLParam(r, "run_id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "eval_run_not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// GetReport handles GET /v1/evals/runs/{run_id}/report.
// @Tags Eval
// @Summary Get eval report
// @Produce application/json
// @Param run_id path string true "Eval run ID or latest"
// @Param format query string false "json or markdown"
// @Success 200 {object} evals.EvalReport
// @Failure 404 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/evals/runs/{run_id}/report [get]
func (h *EvalsHandler) GetReport(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	if r.URL.Query().Get("format") == "markdown" {
		body, err := h.Deps.Evals.GetReportMarkdown(chi.URLParam(r, "run_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "eval_report_not_found", err.Error(), nil)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(body))
		return
	}
	report, err := h.Deps.Evals.GetReport(chi.URLParam(r, "run_id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "eval_report_not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ReplayRun handles POST /v1/runs/{run_id}/replay.
// @Tags Replay
// @Summary Replay workflow run in safe mode
// @Accept application/json
// @Produce application/json
// @Param run_id path string true "Workflow run ID"
// @Param body body evals.ReplayOptions false "Replay options"
// @Success 200 {object} evals.ReplayResult
// @Failure 400 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/runs/{run_id}/replay [post]
func (h *EvalsHandler) ReplayRun(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	var body evals.ReplayOptions
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), nil)
			return
		}
	}
	result, err := h.Deps.Evals.ReplayRun(r.Context(), chi.URLParam(r, "run_id"), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "replay_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// GetReplay handles GET /v1/replays/{replay_id}.
// @Tags Replay
// @Summary Get replay result
// @Produce application/json
// @Param replay_id path string true "Replay ID"
// @Success 200 {object} evals.ReplayResult
// @Failure 404 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/replays/{replay_id} [get]
func (h *EvalsHandler) GetReplay(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Evals == nil {
		writeError(w, http.StatusServiceUnavailable, "eval_unavailable", "eval manager is not configured", nil)
		return
	}
	result, err := h.Deps.Evals.GetReplay(chi.URLParam(r, "replay_id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "replay_not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
