package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/core"
	"local-agent/internal/ids"
	"local-agent/internal/tools/ops"
)

// OpsHandler serves operations APIs.
type OpsHandler struct {
	Base
}

// NewOpsHandler creates an ops handler.
func NewOpsHandler(deps Dependencies) *OpsHandler {
	return &OpsHandler{Base{Deps: deps}}
}

// ListHosts handles GET /v1/ops/hosts.
// @Tags Ops
// @Summary List operations host profiles
// @Produce application/json
// @Success 200 {object} OpsHostListResponse
// @Router /v1/ops/hosts [get]
func (h *OpsHandler) ListHosts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, OpsHostListResponse{Items: h.Deps.Ops.ListHosts()})
}

// CreateHost handles POST /v1/ops/hosts.
// @Tags Ops
// @Summary Create operations host profile
// @Accept application/json
// @Produce application/json
// @Param body body ops.HostProfileInput true "Host profile payload"
// @Success 201 {object} ops.HostProfile
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/ops/hosts [post]
func (h *OpsHandler) CreateHost(w http.ResponseWriter, r *http.Request) {
	var body ops.HostProfileInput
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := h.Deps.Ops.CreateHost(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// GetHost handles GET /v1/ops/hosts/{host_id}.
// @Tags Ops
// @Summary Get operations host profile
// @Produce application/json
// @Param host_id path string true "Host profile ID"
// @Success 200 {object} ops.HostProfile
// @Failure 404 {object} LegacyErrorResponse
// @Router /v1/ops/hosts/{host_id} [get]
func (h *OpsHandler) GetHost(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Ops.GetHost(chi.URLParam(r, "host_id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// UpdateHost handles PATCH /v1/ops/hosts/{host_id}.
// @Tags Ops
// @Summary Update operations host profile
// @Accept application/json
// @Produce application/json
// @Param host_id path string true "Host profile ID"
// @Param body body ops.HostProfileInput true "Host profile payload"
// @Success 200 {object} ops.HostProfile
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/ops/hosts/{host_id} [patch]
func (h *OpsHandler) UpdateHost(w http.ResponseWriter, r *http.Request) {
	var body ops.HostProfileInput
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := h.Deps.Ops.UpdateHost(chi.URLParam(r, "host_id"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// DeleteHost handles DELETE /v1/ops/hosts/{host_id}.
// @Tags Ops
// @Summary Delete operations host profile
// @Produce application/json
// @Param host_id path string true "Host profile ID"
// @Success 200 {object} StatusResponse
// @Failure 404 {object} LegacyErrorResponse
// @Router /v1/ops/hosts/{host_id} [delete]
func (h *OpsHandler) DeleteHost(w http.ResponseWriter, r *http.Request) {
	if err := h.Deps.Ops.DeleteHost(chi.URLParam(r, "host_id")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, StatusResponse{Status: "ok"})
}

// TestHost handles POST /v1/ops/hosts/{host_id}/test.
// @Tags Ops
// @Summary Test operations host profile connectivity
// @Produce application/json
// @Param host_id path string true "Host profile ID"
// @Success 200 {object} ops.HostTestResult
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/ops/hosts/{host_id}/test [post]
func (h *OpsHandler) TestHost(w http.ResponseWriter, r *http.Request) {
	result, err := h.Deps.Ops.TestHost(r.Context(), chi.URLParam(r, "host_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ListRunbooks handles GET /v1/ops/runbooks.
// @Tags Ops
// @Summary List operations runbooks
// @Produce application/json
// @Success 200 {object} OpsRunbookListResponse
// @Router /v1/ops/runbooks [get]
func (h *OpsHandler) ListRunbooks(w http.ResponseWriter, r *http.Request) {
	items, err := h.Deps.Ops.ListRunbooks()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, OpsRunbookListResponse{Items: items})
}

// ReadRunbook handles GET /v1/ops/runbooks/{id}.
// @Tags Ops
// @Summary Read operations runbook
// @Produce application/json
// @Param id path string true "Runbook ID"
// @Success 200 {object} ops.Runbook
// @Failure 404 {object} LegacyErrorResponse
// @Router /v1/ops/runbooks/{id} [get]
func (h *OpsHandler) ReadRunbook(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Ops.ReadRunbook(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// PlanRunbook handles POST /v1/ops/runbooks/{id}/plan.
// @Tags Ops
// @Summary Plan operations runbook without executing steps
// @Accept application/json
// @Produce application/json
// @Param id path string true "Runbook ID"
// @Param body body OpsRunbookPlanRequest false "Runbook plan payload"
// @Success 200 {object} ops.RunbookPlan
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/ops/runbooks/{id}/plan [post]
func (h *OpsHandler) PlanRunbook(w http.ResponseWriter, r *http.Request) {
	var body OpsRunbookPlanRequest
	_ = decodeJSON(r, &body)
	plan, err := h.Deps.Ops.PlanRunbook(chi.URLParam(r, "id"), body.HostID, true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// ExecuteRunbook handles POST /v1/ops/runbooks/{id}/execute.
// @Tags Ops
// @Summary Execute operations runbook through ToolRouter
// @Accept application/json
// @Produce application/json
// @Param id path string true "Runbook ID"
// @Param body body ops.RunbookExecuteRequest false "Runbook execute payload"
// @Success 200 {object} ToolRouteResponse
// @Success 202 {object} ToolRouteResponse
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/ops/runbooks/{id}/execute [post]
func (h *OpsHandler) ExecuteRunbook(w http.ResponseWriter, r *http.Request) {
	var body ops.RunbookExecuteRequest
	_ = decodeJSON(r, &body)
	input := map[string]any{
		"runbook_id": chi.URLParam(r, "id"),
		"host_id":    body.HostID,
		"dry_run":    body.DryRun,
		"max_steps":  body.MaxSteps,
	}
	proposal := core.ToolProposal{
		ID:        ids.New("tool"),
		Tool:      "runbook.execute",
		Input:     input,
		Purpose:   "执行运维 runbook",
		CreatedAt: time.Now().UTC(),
	}
	outcome, err := h.Deps.Router.Propose(r.Context(), "", "", proposal)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	status := http.StatusOK
	if outcome.Approval != nil {
		status = http.StatusAccepted
	}
	writeJSON(w, status, ToolRouteResponse{
		Approval:  outcome.Approval,
		Decision:  &outcome.Decision,
		Inference: &outcome.Inference,
		Result:    outcome.Result,
	})
}
